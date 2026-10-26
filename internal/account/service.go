package account

import (
	"context"
	"errors"
	"net/mail"
	"sort"
	"strings"
	"time"
)

var (
	// ErrInput is a malformed email or password.
	ErrInput = errors.New("account: bad input")
	// ErrCredentials is a wrong email or password; it never says which.
	ErrCredentials = errors.New("account: wrong email or password")
	// ErrUnverified means the password is right but the email is not confirmed yet.
	ErrUnverified = errors.New("account: email not verified")
	// ErrBusy is too many attempts in a short time.
	ErrBusy = errors.New("account: too many attempts")
	// ErrExpired is a link, code or challenge past its time or already used.
	ErrExpired = errors.New("account: expired")
	// ErrSession is a missing or ended session.
	ErrSession = errors.New("account: no session")
)

const (
	maxEmail      = 254
	minPass       = 8
	maxPass       = 128
	maxAccounts   = 10_000
	maxChallenges = 2_000
	maxPerEmail   = 5
	maxCodeTries  = 5
	maxFailures   = 10
	maxDevices    = 16
	maxDeviceBots = 64
	maxNameLen    = 64
	maxRegPerIP   = 5 // register calls per client IP per rolling hour
)

// Config is the service's knobs. Zero values take the defaults.
type Config struct {
	// BaseURL is the public address mail links point at, such as
	// https://account.example.com. A request may supply it with WithLinkBase
	// only when this is empty (the local stand-in).
	BaseURL    string
	LinkTTL    time.Duration // 15 minutes
	SessionTTL time.Duration // 30 days
	ResendGap  time.Duration // 30 seconds
	FailWindow time.Duration // 15 minutes
	OnlineFor  time.Duration // 3 minutes: a device reporting within this is online
	Now        func() time.Time
}

// Service is the account logic over a Store and a Mailer.
type Service struct {
	store    Store
	mail     Mailer
	cfg      Config
	regLimit *ipLimit
}

func New(store Store, mailer Mailer, cfg Config) *Service {
	if cfg.LinkTTL <= 0 {
		cfg.LinkTTL = 15 * time.Minute
	}
	if cfg.SessionTTL <= 0 {
		cfg.SessionTTL = 30 * 24 * time.Hour
	}
	if cfg.ResendGap <= 0 {
		cfg.ResendGap = 30 * time.Second
	}
	if cfg.FailWindow <= 0 {
		cfg.FailWindow = 15 * time.Minute
	}
	if cfg.OnlineFor <= 0 {
		cfg.OnlineFor = 3 * time.Minute
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	return &Service{store: store, mail: mailer, cfg: cfg, regLimit: newIPLimit(maxRegPerIP, time.Hour)}
}

type linkBaseKey struct{}

// WithLinkBase passes the address mail links use when the service has no BaseURL.
func WithLinkBase(ctx context.Context, base string) context.Context {
	return context.WithValue(ctx, linkBaseKey{}, strings.TrimRight(base, "/"))
}

func (s *Service) linkBase(ctx context.Context) string {
	if s.cfg.BaseURL != "" {
		return s.cfg.BaseURL
	}
	v, _ := ctx.Value(linkBaseKey{}).(string)
	return v
}

// Pending is what a client keeps while it waits for the email: the challenge
// id to poll with and the masked address to show.
type Pending struct {
	Challenge string `json:"challenge"`
	Email     string `json:"email"` // masked
}

// Signed is a finished sign-in.
type Signed struct {
	Session string `json:"session"`
	Email   string `json:"email"` // masked
}

// Me is what a session knows about its account.
type Me struct {
	Email    string `json:"email"` // masked
	Verified bool   `json:"verified"`
	// Key is a stable opaque id for the account (a hash of the address), so a
	// client can tell accounts apart without holding the address.
	Key string `json:"key"`
}

// DeviceView is one device of the account.
type DeviceView struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Online bool     `json:"online"`
	Bots   []string `json:"bots"`
}

// PollResult is a challenge's state. Session is set once, on the first poll after it finishes.
type PollResult struct {
	State   string `json:"state"` // pending | done | expired
	Session string `json:"session,omitempty"`
	Email   string `json:"email,omitempty"`
}

func normEmail(raw string) (string, error) {
	e := strings.ToLower(strings.TrimSpace(raw))
	if e == "" || len(e) > maxEmail || strings.ContainsAny(e, " \t\r\n<>") {
		return "", ErrInput
	}
	a, err := mail.ParseAddress(e)
	if err != nil || a.Address != e || !strings.Contains(e[strings.LastIndexByte(e, '@')+1:], ".") {
		return "", ErrInput
	}
	return e, nil
}

func checkPass(p string) error {
	if len(p) < minPass || len(p) > maxPass {
		return ErrInput
	}
	return nil
}

// Register creates a verified account and signs it in at once; no mail is
// sent. A taken address is a login: the right password signs in, any other is
// ErrCredentials and counts as a failed login. Every call counts against the
// caller's IP (see WithClientIP), bad input included.
func (s *Service) Register(ctx context.Context, email, pass string) (Signed, error) {
	if !s.regLimit.allow(clientIPFrom(ctx), s.cfg.Now()) {
		return Signed{}, ErrBusy
	}
	return s.register(ctx, email, pass)
}

// register is Register after the IP has been counted.
func (s *Service) register(ctx context.Context, email, pass string) (Signed, error) {
	e, err := normEmail(email)
	if err != nil {
		return Signed{}, err
	}
	taken := false
	if err := s.store.View(func(st *State) error {
		_, taken = st.Accounts[e]
		return nil
	}); err != nil {
		return Signed{}, err
	}
	if !taken {
		if err := checkPass(pass); err != nil {
			return Signed{}, err
		}
		hash, err := hashPassword(pass)
		if err != nil {
			return Signed{}, err
		}
		var out Signed
		err = s.store.Update(func(st *State) error {
			s.prune(st)
			if _, ok := st.Accounts[e]; ok {
				taken = true // signed up by another call meanwhile
				return nil
			}
			if len(st.Accounts) >= maxAccounts {
				return ErrBusy
			}
			st.Accounts[e] = &Account{Email: e, PassHash: hash, Verified: true, Created: s.cfg.Now()}
			// Sign-ups still waiting from the mail flow are void now.
			for _, c := range st.Challenges {
				if c.Email == e && c.PassHash != "" && !c.Done {
					c.Done, c.Collected = true, true
				}
			}
			tok, err := s.newSession(st, e)
			out = Signed{Session: tok, Email: MaskEmail(e)}
			return err
		})
		if err != nil || !taken {
			return out, err
		}
	}
	signed, _, err := s.Login(ctx, e, pass)
	if errors.Is(err, ErrUnverified) {
		err = ErrCredentials
	}
	return signed, err
}

// Magic starts a login by mail. An address with no account sends nothing but
// answers the same.
func (s *Service) Magic(ctx context.Context, email string) (Pending, error) {
	e, err := normEmail(email)
	if err != nil {
		return Pending{}, err
	}
	var out Pending
	var m *Mail
	err = s.store.Update(func(st *State) error {
		s.prune(st)
		a, ok := st.Accounts[e]
		c, mm, err := s.newChallenge(ctx, st, e, "", true, !ok || !a.Verified)
		if err != nil {
			return err
		}
		out, m = Pending{Challenge: c.ID, Email: MaskEmail(e)}, mm
		return nil
	})
	if err != nil {
		return Pending{}, err
	}
	return out, s.send(ctx, m)
}

// Resend mails a fresh link and code for the challenge; the old ones stop
// working. Within ResendGap it changes nothing and still succeeds.
func (s *Service) Resend(ctx context.Context, id string) error {
	var m *Mail
	err := s.store.Update(func(st *State) error {
		s.prune(st)
		c, ok := st.Challenges[id]
		if !ok || c.Done {
			return ErrExpired
		}
		now := s.cfg.Now()
		if now.Sub(c.SentAt) < s.cfg.ResendGap {
			return nil
		}
		link, code, err := s.secrets(ctx, c)
		if err != nil {
			return err
		}
		c.SentAt, c.Expires, c.Attempts = now, now.Add(s.cfg.LinkTTL), 0
		if !c.Silent {
			mm := verifyMail(c.Email, link, code, c.PassHash == "" && st.Accounts[c.Email] != nil)
			m = &mm
		}
		return nil
	})
	if err != nil {
		return err
	}
	return s.send(ctx, m)
}

// ConfirmLink finishes the challenge whose mail link carried token.
func (s *Service) ConfirmLink(token string) error {
	if token == "" || len(token) > 128 {
		return ErrExpired
	}
	want := hashOf(token)
	return s.store.Update(func(st *State) error {
		s.prune(st)
		for _, c := range st.Challenges {
			if !c.Silent && !c.Done && sameHash(c.LinkHash, want) {
				return s.finish(st, c)
			}
		}
		return ErrExpired
	})
}

// LinkValid reports whether a mail link can still be confirmed, without using it.
func (s *Service) LinkValid(token string) bool {
	if token == "" || len(token) > 128 {
		return false
	}
	want, ok := hashOf(token), false
	_ = s.store.View(func(st *State) error {
		now := s.cfg.Now()
		for _, c := range st.Challenges {
			if !c.Silent && !c.Done && now.Before(c.Expires) && sameHash(c.LinkHash, want) {
				ok = true
			}
		}
		return nil
	})
	return ok
}

// ConfirmCode finishes a challenge with the code from the mail and signs in.
func (s *Service) ConfirmCode(id, code string) (Signed, error) {
	code = strings.TrimSpace(code)
	var out Signed
	wrong := false
	err := s.store.Update(func(st *State) error {
		s.prune(st)
		c, ok := st.Challenges[id]
		if !ok || c.Done {
			return ErrExpired
		}
		if c.Attempts >= maxCodeTries {
			return ErrBusy
		}
		c.Attempts++
		if c.Silent || len(code) != 6 || !sameHash(c.CodeHash, hashOf(code)) {
			wrong = true // saved, so the attempt counts
			return nil
		}
		if err := s.finish(st, c); err != nil {
			return err
		}
		c.Collected = true
		tok, err := s.newSession(st, c.Email)
		out = Signed{Session: tok, Email: MaskEmail(c.Email)}
		return err
	})
	if err == nil && wrong {
		return Signed{}, ErrCredentials
	}
	return out, err
}

// Poll reports a challenge; the first poll after it finishes carries the session.
func (s *Service) Poll(id string) (PollResult, error) {
	var out PollResult
	err := s.store.Update(func(st *State) error {
		s.prune(st)
		c, ok := st.Challenges[id]
		switch {
		case !ok:
			out.State = "expired"
		case !c.Done:
			out.State = "pending"
		case c.Collected:
			out.State = "expired"
		default:
			tok, err := s.newSession(st, c.Email)
			if err != nil {
				return err
			}
			c.Collected = true
			out = PollResult{State: "done", Session: tok, Email: MaskEmail(c.Email)}
		}
		return nil
	})
	return out, err
}

// Login signs in with a password. A sign-up still waiting for its email
// answers ErrUnverified with that sign-up's challenge.
func (s *Service) Login(ctx context.Context, email, pass string) (Signed, Pending, error) {
	e, err := normEmail(email)
	if err != nil || len(pass) > maxPass {
		return Signed{}, Pending{}, ErrCredentials
	}
	var signed Signed
	var pend Pending
	failed := false
	err = s.store.Update(func(st *State) error {
		s.prune(st)
		now := s.cfg.Now()
		a := st.Accounts[e]
		if a != nil && a.Failures >= maxFailures && now.Sub(a.FailedAt) < s.cfg.FailWindow {
			return ErrBusy
		}
		if a != nil && a.Verified && checkPassword(a.PassHash, pass) {
			a.Failures = 0
			tok, err := s.newSession(st, e)
			signed = Signed{Session: tok, Email: MaskEmail(e)}
			return err
		}
		for _, c := range st.Challenges {
			if c.Email == e && !c.Done && c.PassHash != "" && checkPassword(c.PassHash, pass) {
				pend = Pending{Challenge: c.ID, Email: MaskEmail(e)}
				return ErrUnverified
			}
		}
		failed = true
		if a != nil {
			if now.Sub(a.FailedAt) >= s.cfg.FailWindow {
				a.Failures = 0
			}
			a.Failures++
			a.FailedAt = now
		}
		return nil
	})
	if err == nil && failed {
		err = ErrCredentials
	}
	return signed, pend, err
}

// Session reads a session.
func (s *Service) Session(token string) (Me, error) {
	var out Me
	err := s.store.View(func(st *State) error {
		e, ok := s.sessionEmail(st, token)
		if !ok {
			return ErrSession
		}
		a := st.Accounts[e]
		out = Me{Email: MaskEmail(e), Verified: a != nil && a.Verified, Key: hashOf("account:" + e)[:24]}
		return nil
	})
	return out, err
}

// Logout ends a session. Ending it twice is fine.
func (s *Service) Logout(token string) error {
	return s.store.Update(func(st *State) error {
		delete(st.Sessions, hashOf(token))
		return nil
	})
}

// ReportDevice records this device for the session's account.
func (s *Service) ReportDevice(token, id, name string, bots []string) error {
	id, name = clip(id), clip(name)
	if id == "" || name == "" {
		return ErrInput
	}
	clean := make([]string, 0, len(bots))
	for _, b := range bots {
		if b = clip(b); b != "" && len(clean) < maxDeviceBots {
			clean = append(clean, b)
		}
	}
	return s.store.Update(func(st *State) error {
		e, ok := s.sessionEmail(st, token)
		if !ok {
			return ErrSession
		}
		key := e + "/" + id
		if _, ok := st.Devices[key]; !ok {
			n := 0
			for _, d := range st.Devices {
				if d.Email == e {
					n++
				}
			}
			if n >= maxDevices {
				return ErrBusy
			}
		}
		st.Devices[key] = &Device{ID: id, Email: e, Name: name, Bots: clean, LastSeen: s.cfg.Now()}
		return nil
	})
}

// Devices lists the session account's devices, online ones first.
func (s *Service) Devices(token string) ([]DeviceView, error) {
	out := []DeviceView{}
	err := s.store.View(func(st *State) error {
		e, ok := s.sessionEmail(st, token)
		if !ok {
			return ErrSession
		}
		now := s.cfg.Now()
		for _, d := range st.Devices {
			if d.Email == e {
				out = append(out, DeviceView{ID: d.ID, Name: d.Name, Online: now.Sub(d.LastSeen) < s.cfg.OnlineFor, Bots: append([]string{}, d.Bots...)})
			}
		}
		return nil
	})
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Online != out[j].Online {
			return out[i].Online
		}
		return out[i].Name < out[j].Name
	})
	return out, err
}

func clip(s string) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > maxNameLen {
		s = string(r[:maxNameLen])
	}
	return s
}

func (s *Service) sessionEmail(st *State, token string) (string, bool) {
	if token == "" || len(token) > 128 {
		return "", false
	}
	sess, ok := st.Sessions[hashOf(token)]
	if !ok || !s.cfg.Now().Before(sess.Expires) {
		return "", false
	}
	return sess.Email, true
}

func (s *Service) newSession(st *State, email string) (string, error) {
	tok, err := randomToken()
	if err != nil {
		return "", err
	}
	st.Sessions[hashOf(tok)] = &Session{Email: email, Expires: s.cfg.Now().Add(s.cfg.SessionTTL)}
	return tok, nil
}

// finish confirms the mailbox: a sign-up becomes a verified account with the
// password it was started with.
func (s *Service) finish(st *State, c *Challenge) error {
	if !s.cfg.Now().Before(c.Expires) {
		return ErrExpired
	}
	c.Done = true
	if c.PassHash != "" {
		st.Accounts[c.Email] = &Account{Email: c.Email, PassHash: c.PassHash, Verified: true, Created: s.cfg.Now()}
		// Other sign-ups for this address are void now.
		for _, o := range st.Challenges {
			if o != c && o.Email == c.Email && o.PassHash != "" && !o.Done {
				o.Done, o.Collected = true, true
			}
		}
		return nil
	}
	if a := st.Accounts[c.Email]; a == nil || !a.Verified {
		return ErrExpired
	}
	return nil
}

func (s *Service) newChallenge(ctx context.Context, st *State, email, pass string, login, silent bool) (*Challenge, *Mail, error) {
	if len(st.Challenges) >= maxChallenges {
		return nil, nil, ErrBusy
	}
	n := 0
	for _, c := range st.Challenges {
		if c.Email == email && !c.Done {
			n++
		}
	}
	if n >= maxPerEmail {
		return nil, nil, ErrBusy
	}
	id, err := randomToken()
	if err != nil {
		return nil, nil, err
	}
	now := s.cfg.Now()
	c := &Challenge{ID: id, Email: email, SentAt: now, Expires: now.Add(s.cfg.LinkTTL), Silent: silent, PassHash: pass}
	link, code, err := s.secrets(ctx, c)
	if err != nil {
		return nil, nil, err
	}
	st.Challenges[id] = c
	if silent {
		return c, nil, nil
	}
	m := verifyMail(email, link, code, login)
	return c, &m, nil
}

// secrets sets a fresh link token and code on c and returns their plain values.
func (s *Service) secrets(ctx context.Context, c *Challenge) (string, string, error) {
	tok, err := randomToken()
	if err != nil {
		return "", "", err
	}
	code, err := randomCode()
	if err != nil {
		return "", "", err
	}
	c.LinkHash, c.CodeHash = hashOf(tok), hashOf(code)
	return s.linkBase(ctx) + LinkPath + "?t=" + tok, code, nil
}

func (s *Service) send(ctx context.Context, m *Mail) error {
	if m == nil || s.mail == nil {
		return nil
	}
	return s.mail.Send(ctx, *m)
}

// prune drops expired sessions and challenges.
func (s *Service) prune(st *State) {
	now := s.cfg.Now()
	for k, v := range st.Sessions {
		if !now.Before(v.Expires) {
			delete(st.Sessions, k)
		}
	}
	for k, c := range st.Challenges {
		if now.After(c.Expires.Add(s.cfg.LinkTTL)) || (c.Collected && now.After(c.Expires)) {
			delete(st.Challenges, k)
		}
	}
}
