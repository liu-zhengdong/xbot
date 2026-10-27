package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"xbot/internal/account"
	"xbot/internal/bot"
	"xbot/internal/fsutil"
	"xbot/internal/peer"
	"xbot/internal/remote"
)

// The product account (email + password) is the way in on desktop and phone.
// It is a separate identity from the install's owner token (the old login
// code, /api/login and /app/launch, all kept): an account that this install
// is bound to is handed the owner session when it signs in. Nothing here
// changes what /api/login means.

const accountCookie = "xbot_account"

// accountState is the per-server account wiring, set up on Register.
type accountState struct {
	client account.Client
	local  *account.Service // the stand-in, when no hosted service is configured
	mu     sync.Mutex
	report time.Time // last device report, to send at most once a minute
	pipeMu sync.Mutex
	pipe   *accountPipe
}

// accountBinding is which account this install belongs to, by the service's
// opaque key (no address is kept here), and this install's device id.
type accountBinding struct {
	Key      string    `json:"key,omitempty"`
	DeviceID string    `json:"device_id"`
	Bound    time.Time `json:"bound,omitzero"`
}

func (s *Server) bindingPath() string { return filepath.Join(s.Root, "account-binding.json") }

func (s *Server) readBinding() (accountBinding, error) {
	var b accountBinding
	raw, err := os.ReadFile(s.bindingPath())
	if errors.Is(err, os.ErrNotExist) {
		return b, nil
	}
	if err != nil {
		return b, err
	}
	return b, json.Unmarshal(raw, &b)
}

// bindIfFree binds the install to key when nothing is bound yet, and gives the
// install a device id. It returns the binding after the call.
func (s *Server) bindIfFree(key string, allowed bool) (accountBinding, error) {
	var out accountBinding
	err := fsutil.WithLock(s.bindingPath()+".lock", func() error {
		b, err := s.readBinding()
		if err != nil {
			return err
		}
		changed := false
		if b.DeviceID == "" {
			var buf [16]byte
			if _, err := rand.Read(buf[:]); err != nil {
				return err
			}
			id := hex.EncodeToString(buf[:])
			b.DeviceID, changed = id, true
		}
		if b.Key == "" && allowed && key != "" {
			b.Key, b.Bound, changed = key, time.Now().UTC(), true
		}
		out = b
		if !changed {
			return nil
		}
		raw, err := fsutil.Marshal(b)
		if err != nil {
			return err
		}
		return fsutil.WriteAtomic(s.bindingPath(), raw, 0o600)
	})
	return out, err
}

// ownerProof says the request may claim an unbound install: it already holds
// the owner session (desktop launch, or the old code), or it comes from this
// computer directly rather than through a tunnel or proxy.
func (s *Server) ownerProof(r *http.Request) bool {
	if s.authenticated(r) {
		return true
	}
	if forwarded(r) {
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// forwarded reports any sign that a proxy or tunnel relayed the request: every
// X-Forwarded-* header, Forwarded, Via, and the client-address headers that
// tunnels and proxies add. Present at all, even empty, it counts.
func forwarded(r *http.Request) bool {
	for name := range r.Header {
		if strings.HasPrefix(name, "X-Forwarded-") {
			return true
		}
		switch name {
		case "Forwarded", "Via", "X-Real-Ip", "Cf-Connecting-Ip", "Cf-Ray", "True-Client-Ip", "X-Client-Ip":
			return true
		}
	}
	return false
}

// accountClaim binds this install to the signed-in account: the real
// bindIfFree, so the same owner proof as sign-in applies, and a device
// report follows so the machine shows up in 我的设备 at once.
func (s *Server) accountClaim(w http.ResponseWriter, r *http.Request) {
	if !s.accountReady(w, r) {
		return
	}
	session := accountSession(r)
	if session == "" {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	me, err := s.acct.client.Me(r.Context(), session)
	if errors.Is(err, account.ErrSession) {
		s.forgetPipeSession(session)
		clearAccountCookie(w)
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if err != nil {
		writeAccountErr(w, err)
		return
	}
	b, err := s.bindIfFree(me.Key, me.Verified && s.ownerProof(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "account binding")
		return
	}
	if b.Key != "" && b.Key != me.Key {
		writeErr(w, http.StatusForbidden, "这台电脑已经绑在另一个账号上")
		return
	}
	if !me.Verified || !s.ownerProof(r) {
		writeErr(w, http.StatusForbidden, "要在这台电脑上登录，才能把它绑到账号")
		return
	}
	view := s.accountViewFor(r, me, b)
	s.setCookie(w, s.loginToken())
	if err := s.keepPipeSession(session); err != nil {
		writeErr(w, 500, "account session persistence")
		return
	}
	// The binding is on disk either way; reported tells the page whether the
	// account's device list has this computer yet.
	reported := s.reportDevice(r.Context(), session, b, true) == nil
	writeJSON(w, http.StatusOK, map[string]any{"account": view, "bound": true, "reported": reported})
}

func (s *Server) registerAccount(mux *http.ServeMux) {
	if s.acct.client == nil {
		if s.Account != nil {
			s.acct.client = s.Account
		} else if svc, err := account.OpenLocal(filepath.Join(s.Root, "account-api")); err == nil {
			s.acct.client, s.acct.local = account.Local{S: svc}, svc
		}
	}
	if s.acct.local != nil {
		// The stand-in's mail links open here; the rest of its API stays in process.
		h := s.acct.local.Handler()
		mux.Handle("GET "+account.LinkPath, h)
		mux.Handle("POST "+account.LinkPath, h)
	}
	mux.HandleFunc("POST /api/account/register", s.accountRegister)
	mux.HandleFunc("POST /api/account/login", s.accountLogin)
	mux.HandleFunc("POST /api/account/magic", s.accountMagic)
	mux.HandleFunc("POST /api/account/resend", s.accountResend)
	mux.HandleFunc("POST /api/account/code", s.accountCode)
	mux.HandleFunc("POST /api/account/poll", s.accountPoll)
	mux.HandleFunc("GET /api/account/me", s.accountMe)
	mux.HandleFunc("POST /api/account/logout", s.accountLogout)
	mux.HandleFunc("POST /api/account/claim", s.accountClaim)
	mux.HandleFunc("GET /api/account/devices", s.accountDevices)
	mux.HandleFunc("GET /account.js", s.static("static/account.js", "text/javascript; charset=utf-8"))
	mux.HandleFunc("GET /account.css", s.static("static/account.css", "text/css; charset=utf-8"))
}

// accountCtx carries the address mail links should use when the stand-in runs.
func accountCtx(r *http.Request) context.Context {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return account.WithLinkBase(r.Context(), scheme+"://"+r.Host)
}

func (s *Server) accountReady(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet && !allowOrigin(r) {
		writeErr(w, http.StatusForbidden, "cross-origin")
		return false
	}
	if s.acct.client == nil {
		writeErr(w, http.StatusServiceUnavailable, "unavailable")
		return false
	}
	return true
}

func writeAccountErr(w http.ResponseWriter, err error) {
	code, msg := account.ErrorCode(err)
	if code == http.StatusInternalServerError && strings.Contains(err.Error(), "unreachable") {
		code, msg = http.StatusBadGateway, "unreachable"
	}
	writeJSON(w, code, map[string]string{"error": msg})
}

func setAccountCookie(w http.ResponseWriter, session string) {
	http.SetCookie(w, &http.Cookie{Name: accountCookie, Value: session, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 86400 * 30})
}

func clearAccountCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: accountCookie, Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
}

func accountSession(r *http.Request) string {
	c, err := r.Cookie(accountCookie)
	if err != nil || len(c.Value) > 128 {
		return ""
	}
	return c.Value
}

type credsBody struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type challengeBody struct {
	Challenge string `json:"challenge"`
	Code      string `json:"code,omitempty"`
}

func (s *Server) accountRegister(w http.ResponseWriter, r *http.Request) {
	if !s.accountReady(w, r) {
		return
	}
	var in credsBody
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	// Sign-up sends no mail for now and signs in at once; mail verification
	// comes back once real mail is sent (ADR 0025).
	signed, err := s.acct.client.Register(accountCtx(r), in.Email, in.Password)
	if err != nil {
		writeAccountErr(w, err)
		return
	}
	s.signIn(w, r, signed.Session)
}

func (s *Server) accountMagic(w http.ResponseWriter, r *http.Request) {
	if !s.accountReady(w, r) {
		return
	}
	var in struct {
		Email string `json:"email"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	p, err := s.acct.client.Magic(accountCtx(r), in.Email)
	if err != nil {
		writeAccountErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) accountResend(w http.ResponseWriter, r *http.Request) {
	if !s.accountReady(w, r) {
		return
	}
	var in challengeBody
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	if err := s.acct.client.Resend(accountCtx(r), in.Challenge); err != nil {
		writeAccountErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) accountLogin(w http.ResponseWriter, r *http.Request) {
	if !s.accountReady(w, r) {
		return
	}
	var in credsBody
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	signed, pend, err := s.acct.client.Login(accountCtx(r), in.Email, in.Password)
	if errors.Is(err, account.ErrUnverified) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "unverified", "challenge": pend.Challenge, "email": pend.Email})
		return
	}
	if err != nil {
		writeAccountErr(w, err)
		return
	}
	s.signIn(w, r, signed.Session)
}

func (s *Server) accountCode(w http.ResponseWriter, r *http.Request) {
	if !s.accountReady(w, r) {
		return
	}
	var in challengeBody
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	signed, err := s.acct.client.Code(r.Context(), in.Challenge, in.Code)
	if err != nil {
		writeAccountErr(w, err)
		return
	}
	s.signIn(w, r, signed.Session)
}

func (s *Server) accountPoll(w http.ResponseWriter, r *http.Request) {
	if !s.accountReady(w, r) {
		return
	}
	var in challengeBody
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	res, err := s.acct.client.Poll(r.Context(), in.Challenge)
	if err != nil {
		writeAccountErr(w, err)
		return
	}
	if res.State != "done" {
		writeJSON(w, http.StatusOK, map[string]string{"state": res.State})
		return
	}
	s.signIn(w, r, res.Session)
}

// accountView is what the page learns about the signed-in account.
type accountView struct {
	Email string `json:"email"` // masked
	// Owner means this install belongs to the account: the page may open the
	// main interface. Otherwise the account only sees its devices.
	Owner bool `json:"owner"`
	// Hosted is false while the local stand-in runs.
	Hosted  bool   `json:"hosted"`
	Gateway string `json:"gateway"`
	// Official is the gateway being the official default one.
	Official  bool `json:"official"`
	GatewayOK bool `json:"gateway_ok"`
	// App is the desktop app's page.
	App bool `json:"app"`
	// Unbound means this install is not bound to any account yet; the page
	// may offer 把这台电脑绑到账号.
	Unbound bool `json:"unbound"`
}

// signIn keeps the account session in a cookie and, for the account this
// install belongs to, opens the owner session too. It never binds: an unbound
// install answers unbound, and the page asks 把这台电脑绑到账号？ before
// POST /api/account/claim binds it.
func (s *Server) signIn(w http.ResponseWriter, r *http.Request, session string) {
	me, err := s.acct.client.Me(r.Context(), session)
	if err != nil {
		writeAccountErr(w, err)
		return
	}
	setAccountCookie(w, session)
	b, err := s.bindIfFree(me.Key, false)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "account binding")
		return
	}
	view := s.accountViewFor(r, me, b)
	if view.Owner {
		if err := s.keepPipeSession(session); err != nil {
			writeErr(w, 500, "account session persistence")
			return
		}
		s.setCookie(w, s.loginToken())
		_ = s.reportDevice(r.Context(), session, b, true)
	}
	writeJSON(w, http.StatusOK, map[string]any{"state": "done", "account": view})
}

func (s *Server) accountViewFor(r *http.Request, me account.Me, b accountBinding) accountView {
	cfg, _ := remote.Load(s.Root)
	gw := cfg.GatewayURL()
	return accountView{
		Email: me.Email, Owner: me.Verified && b.Key != "" && b.Key == me.Key,
		Hosted: s.acct.client.Hosted(), Gateway: strings.TrimPrefix(strings.TrimPrefix(gw, "https://"), "http://"),
		Official: gw == remote.DefaultGateway, GatewayOK: gatewayUp(r.Context(), gw, false), App: s.App,
		Unbound: b.Key == "",
	}
}

// accountMe tells the page who is signed in. A valid account session that
// owns this install also renews the owner session.
func (s *Server) accountMe(w http.ResponseWriter, r *http.Request) {
	if !s.accountReady(w, r) {
		return
	}
	session := accountSession(r)
	if session == "" {
		writeJSON(w, http.StatusOK, map[string]any{"account": nil, "app": s.App})
		return
	}
	me, err := s.acct.client.Me(r.Context(), session)
	if errors.Is(err, account.ErrSession) {
		s.forgetPipeSession(session)
		clearAccountCookie(w)
		writeJSON(w, http.StatusOK, map[string]any{"account": nil, "app": s.App})
		return
	}
	if err != nil {
		writeAccountErr(w, err)
		return
	}
	b, err := s.readBinding()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "account binding")
		return
	}
	view := s.accountViewFor(r, me, b)
	if view.Owner {
		if err := s.keepPipeSession(session); err != nil {
			writeErr(w, 500, "account session persistence")
			return
		}
		s.setCookie(w, s.loginToken())
		_ = s.reportDevice(r.Context(), session, b, false)
	}
	writeJSON(w, http.StatusOK, map[string]any{"account": view, "app": s.App})
}

// accountLogout ends the account session here and at the service, and the
// owner session with it: the page goes back to the sign-in screen.
func (s *Server) accountLogout(w http.ResponseWriter, r *http.Request) {
	if !s.accountReady(w, r) {
		return
	}
	if session := accountSession(r); session != "" {
		_ = s.acct.client.Logout(r.Context(), session)
		s.forgetPipeSession(session)
	}
	clearAccountCookie(w)
	if !localBrowser(r) {
		s.clearCookie(w)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// reportDevice tells the service this computer and its bots, at most once a
// minute unless forced. Only bot names leave the machine.
// A failed report clears the throttle, so the next page request tries again.
func (s *Server) reportDevice(ctx context.Context, session string, b accountBinding, force bool) error {
	s.acct.mu.Lock()
	if !force && time.Since(s.acct.report) < time.Minute {
		s.acct.mu.Unlock()
		return nil
	}
	s.acct.report = time.Now()
	s.acct.mu.Unlock()
	if b.DeviceID == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	err := s.acct.client.Report(ctx, session, b.DeviceID, s.selfDeviceName(), s.ownerBotNames())
	if err != nil {
		s.acct.mu.Lock()
		s.acct.report = time.Time{}
		s.acct.mu.Unlock()
	}
	return err
}

// selfDeviceName is the name this computer goes by in 设备; before any
// pairing set one up, the host name.
func (s *Server) selfDeviceName() string {
	if self, err := remote.Self(s.Root, s.owner); err == nil && strings.TrimSpace(self.Name) != "" {
		return strings.TrimSuffix(self.Name, ".local")
	}
	if h, err := os.Hostname(); err == nil && strings.TrimSpace(h) != "" {
		return strings.TrimSuffix(h, ".local")
	}
	return "这台电脑"
}

func (s *Server) ownerBotNames() []string {
	cfgs, err := peer.OwnerBots(s.Root, s.owner)
	if err != nil {
		return nil
	}
	out := []string{}
	for _, cfg := range bot.Order(cfgs) {
		out = append(out, cfg.Name)
	}
	return out
}

type accountBot struct {
	Name      string `json:"name"`
	Hint      string `json:"hint,omitempty"`
	Unread    bool   `json:"unread,omitempty"`
	AvatarURL string `json:"avatar_url,omitempty"`
	AvatarBG  string `json:"avatar_bg,omitempty"`
	AvatarInk string `json:"avatar_ink,omitempty"`
	Initial   string `json:"initial,omitempty"`
}

type accountDevice struct {
	ID     string       `json:"id"`
	Name   string       `json:"name"`
	Online bool         `json:"online"`
	Self   bool         `json:"self"`
	Bots   []accountBot `json:"bots"`
}

// accountDevices lists the account's devices. This computer's row, when it
// belongs to the account, uses the local bot list so it is never stale.
func (s *Server) accountDevices(w http.ResponseWriter, r *http.Request) {
	if !s.accountReady(w, r) {
		return
	}
	session := accountSession(r)
	me, err := s.acct.client.Me(r.Context(), session)
	if err != nil {
		if errors.Is(err, account.ErrSession) {
			s.forgetPipeSession(session)
		}
		writeAccountErr(w, err)
		return
	}
	b, err := s.readBinding()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "account binding")
		return
	}
	owner := me.Verified && b.Key != "" && b.Key == me.Key
	if owner {
		_ = s.reportDevice(r.Context(), session, b, false)
	}
	list, err := s.acct.client.Devices(r.Context(), session)
	if err != nil {
		if errors.Is(err, account.ErrSession) {
			s.forgetPipeSession(session)
		}
		writeAccountErr(w, err)
		return
	}
	out := []accountDevice{}
	for _, d := range list {
		row := accountDevice{ID: d.ID, Name: d.Name, Online: d.Online, Bots: []accountBot{}}
		if owner && d.ID == b.DeviceID {
			row.Self, row.Online, row.Bots = true, true, s.localAccountBots()
		} else {
			for _, n := range d.Bots {
				row.Bots = append(row.Bots, accountBot{Name: n})
			}
		}
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": out})
}

func (s *Server) localAccountBots() []accountBot {
	cfgs, err := peer.OwnerBots(s.Root, s.owner)
	if err != nil {
		return []accountBot{}
	}
	cur, _ := s.readCursor()
	out := []accountBot{}
	for _, cfg := range bot.Order(cfgs) {
		b, err := bot.Load(s.Root, cfg.Name)
		if err != nil {
			continue
		}
		count, _ := b.Count()
		url, bg, ink, initial := paintBot(b)
		out = append(out, accountBot{Name: cfg.Name, Hint: clip(cfg.Description, 40), Unread: count > cur[cfg.Name], AvatarURL: url, AvatarBG: bg, AvatarInk: ink, Initial: initial})
	}
	return out
}
