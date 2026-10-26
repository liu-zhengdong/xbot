package account

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	passIter = 1000
	os.Exit(m.Run())
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type env struct {
	svc    *Service
	srv    *httptest.Server
	cli    Remote
	outbox string
	clk    *clock
}

// newEnv runs the service behind its own HTTP handler, the way it is hosted.
func newEnv(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	st, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	clk := &clock{t: time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)}
	e := &env{outbox: filepath.Join(dir, "outbox"), clk: clk}
	srv := httptest.NewServer(nil)
	t.Cleanup(srv.Close)
	e.svc = New(st, OutboxMailer{Dir: e.outbox}, Config{BaseURL: srv.URL, Now: clk.now})
	srv.Config.Handler = e.svc.Handler()
	e.srv, e.cli = srv, NewRemote(srv.URL)
	return e
}

var (
	linkRe = regexp.MustCompile(`https?://\S+` + regexp.QuoteMeta(LinkPath) + `\?t=([0-9a-f]+)`)
	codeRe = regexp.MustCompile(`验证码：(\d{6})`)
)

// mails returns the outbox files in order.
func (e *env) mails(t *testing.T) []string {
	t.Helper()
	ents, _ := os.ReadDir(e.outbox)
	var names []string
	for _, x := range ents {
		names = append(names, x.Name())
	}
	sort.Strings(names)
	var out []string
	for _, n := range names {
		raw, err := os.ReadFile(filepath.Join(e.outbox, n))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, string(raw))
	}
	return out
}

func lastLink(t *testing.T, mail string) (string, string) {
	t.Helper()
	l, c := linkRe.FindStringSubmatch(mail), codeRe.FindStringSubmatch(mail)
	if l == nil || c == nil {
		t.Fatalf("no link or code in %q", mail)
	}
	return l[1], c[1]
}

func (e *env) openLink(t *testing.T, tok string) int {
	t.Helper()
	res, err := http.PostForm(e.srv.URL+LinkPath, url.Values{"t": {tok}})
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return res.StatusCode
}

func TestRegisterSignsInAtOnce(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	const pass = "correct horse 1"
	r, err := e.cli.Register(ctx, "Zhengdong@Example.com", pass)
	if err != nil || r.Session == "" || r.Email != "zh***@example.com" {
		t.Fatalf("register %+v %v", r, err)
	}
	if n := len(e.mails(t)); n != 0 {
		t.Fatalf("register sent %d mails", n)
	}
	me, err := e.cli.Me(ctx, r.Session)
	if err != nil || !me.Verified || me.Email != "zh***@example.com" || me.Key == "" {
		t.Fatalf("me %+v %v", me, err)
	}
	signed, _, err := e.cli.Login(ctx, "ZHENGDONG@example.com", pass)
	if err != nil || signed.Session == "" {
		t.Fatalf("login %+v %v", signed, err)
	}
	if _, _, err := e.cli.Login(ctx, "zhengdong@example.com", "wrong pass 1"); err != ErrCredentials {
		t.Fatalf("wrong password %v", err)
	}
	if err := e.cli.Logout(ctx, signed.Session); err != nil {
		t.Fatal(err)
	}
	if err := e.cli.Logout(ctx, signed.Session); err != nil {
		t.Fatalf("second logout %v", err)
	}
	if _, err := e.cli.Me(ctx, signed.Session); err != ErrSession {
		t.Fatalf("after logout %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(filepath.Dir(e.outbox), "accounts.json"))
	if strings.Contains(string(raw), pass) || strings.Contains(string(raw), r.Session) || strings.Contains(string(raw), signed.Session) {
		t.Fatal("a secret is stored in the clear")
	}
}

// The first sign-up holds the address: signing up again with its password is
// a login, with any other password it fails like a wrong login.
func TestFirstSignUpHoldsTheAddress(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	first, err := e.cli.Register(ctx, "a@example.com", "first pass 12")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.cli.Register(ctx, "a@example.com", "other pass 12"); err != ErrCredentials {
		t.Fatalf("second sign-up with another password %v", err)
	}
	if _, _, err := e.cli.Login(ctx, "a@example.com", "other pass 12"); err != ErrCredentials {
		t.Fatalf("second password works: %v", err)
	}
	again, err := e.cli.Register(ctx, "A@example.com", "first pass 12")
	if err != nil || again.Session == "" || again.Session == first.Session {
		t.Fatalf("sign-up as login %+v %v", again, err)
	}
	if _, err := e.cli.Me(ctx, again.Session); err != nil {
		t.Fatal(err)
	}
}

// A short wrong password for a taken address is a failed login, not bad input.
func TestRegisterTakenShortPasswordIsALogin(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.cli.Register(ctx, "short@example.com", "password 1"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.cli.Register(ctx, "short@example.com", "wrong"); err != ErrCredentials {
		t.Fatalf("short wrong password %v", err)
	}
	var fails int
	e.svc.store.View(func(st *State) error { fails = st.Accounts["short@example.com"].Failures; return nil })
	if fails != 1 {
		t.Fatalf("failures %d", fails)
	}
	if _, err := e.cli.Register(ctx, "new@example.com", "short"); err != ErrInput {
		t.Fatalf("short password for a new address %v", err)
	}
}

// A Local caller that passes an IP is limited like HTTP; one without is not.
func TestLocalRegisterLimit(t *testing.T) {
	e := newEnv(t)
	l := Local{S: e.svc}
	ctx := WithClientIP(context.Background(), "192.0.2.1")
	for i := 0; i < 5; i++ {
		l.Register(ctx, "bad", "password 1")
	}
	if _, err := l.Register(ctx, "l@example.com", "password 1"); err != ErrBusy {
		t.Fatalf("sixth local %v", err)
	}
	if _, err := l.Register(context.Background(), "l@example.com", "password 1"); err != nil {
		t.Fatalf("no IP %v", err)
	}
}

// postRegister calls register over HTTP with the given headers set.
func (e *env) postRegister(t *testing.T, email string, hdr map[string]string) int {
	t.Helper()
	req, _ := http.NewRequest("POST", e.srv.URL+Prefix+"register", strings.NewReader(`{"email":"`+email+`","password":"password 1"}`))
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return res.StatusCode
}

func TestRegisterIsLimitedPerIP(t *testing.T) {
	e := newEnv(t)
	ip := map[string]string{"X-Real-IP": "203.0.113.7"}
	for i := 0; i < 5; i++ {
		if code := e.postRegister(t, "r"+string(rune('a'+i))+"@example.com", ip); code != 200 {
			t.Fatalf("register %d: %d", i, code)
		}
	}
	if code := e.postRegister(t, "rz@example.com", ip); code != http.StatusTooManyRequests {
		t.Fatalf("sixth register %d", code)
	}
	// A forged X-Forwarded-For does not make a new client.
	spoof := map[string]string{"X-Real-IP": "203.0.113.7", "X-Forwarded-For": "198.51.100.1"}
	if code := e.postRegister(t, "rz@example.com", spoof); code != http.StatusTooManyRequests {
		t.Fatalf("forwarded-for bypass %d", code)
	}
	if code := e.postRegister(t, "rz@example.com", map[string]string{"X-Real-IP": "203.0.113.8"}); code != 200 {
		t.Fatalf("another IP %d", code)
	}
	// Failures count too: bad input and wrong passwords use up the hour.
	other := map[string]string{"X-Real-IP": "203.0.113.9"}
	for i := 0; i < 5; i++ {
		if code := e.postRegister(t, "not an email", other); code != http.StatusBadRequest {
			t.Fatalf("bad input %d: %d", i, code)
		}
	}
	if code := e.postRegister(t, "ok@example.com", other); code != http.StatusTooManyRequests {
		t.Fatalf("after five bad tries %d", code)
	}
	// So do bodies that are not JSON at all.
	raw := map[string]string{"X-Real-IP": "203.0.113.10"}
	for i := 0; i < 5; i++ {
		req, _ := http.NewRequest("POST", e.srv.URL+Prefix+"register", strings.NewReader("{"))
		req.Header.Set("X-Real-IP", raw["X-Real-IP"])
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("malformed %d: %d", i, res.StatusCode)
		}
	}
	if code := e.postRegister(t, "json@example.com", raw); code != http.StatusTooManyRequests {
		t.Fatalf("after five malformed bodies %d", code)
	}
	// Without X-Real-IP the peer address is the client.
	for i := 0; i < 5; i++ {
		e.postRegister(t, "p"+string(rune('a'+i))+"@example.com", nil)
	}
	if code := e.postRegister(t, "pz@example.com", nil); code != http.StatusTooManyRequests {
		t.Fatalf("direct sixth %d", code)
	}
	// The window slides.
	e.clk.add(61 * time.Minute)
	if code := e.postRegister(t, "rz@example.com", ip); code != 200 {
		t.Fatalf("after the hour %d", code)
	}
}

// Mail login still works: link page, confirm, poll for the session.
func TestMagicLinkLogin(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.cli.Register(ctx, "m@example.com", "password 1"); err != nil {
		t.Fatal(err)
	}
	p, err := e.cli.Magic(ctx, "m@example.com")
	if err != nil || p.Challenge == "" || p.Email != "m***@example.com" {
		t.Fatalf("magic %+v %v", p, err)
	}
	mails := e.mails(t)
	if len(mails) != 1 || !strings.Contains(mails[0], "To: m@example.com") || !strings.Contains(mails[0], "登录 XBot") {
		t.Fatalf("mail %q", mails)
	}
	tok, _ := lastLink(t, mails[0])
	if r, _ := e.cli.Poll(ctx, p.Challenge); r.State != "pending" || r.Session != "" {
		t.Fatalf("poll before %+v", r)
	}
	// A mail scanner fetching the link does not use it up.
	res, err := http.Get(e.srv.URL + LinkPath + "?t=" + tok)
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("link page %v %v", res, err)
	}
	res.Body.Close()
	if r, _ := e.cli.Poll(ctx, p.Challenge); r.State != "pending" {
		t.Fatalf("GET used the link %+v", r)
	}
	if code := e.openLink(t, tok); code != 200 {
		t.Fatalf("confirm %d", code)
	}
	if code := e.openLink(t, tok); code != http.StatusGone {
		t.Fatalf("second confirm %d", code)
	}
	r, err := e.cli.Poll(ctx, p.Challenge)
	if err != nil || r.State != "done" || r.Session == "" {
		t.Fatalf("poll after %+v %v", r, err)
	}
	if r2, _ := e.cli.Poll(ctx, p.Challenge); r2.State != "expired" || r2.Session != "" {
		t.Fatalf("session handed out twice %+v", r2)
	}
	if me, err := e.cli.Me(ctx, r.Session); err != nil || !me.Verified {
		t.Fatalf("me %+v %v", me, err)
	}
	raw, _ := os.ReadFile(filepath.Join(filepath.Dir(e.outbox), "accounts.json"))
	if strings.Contains(string(raw), tok) || strings.Contains(string(raw), r.Session) {
		t.Fatal("a secret is stored in the clear")
	}
}

func TestCodeAndResendAndLimits(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.cli.Register(ctx, "b@example.com", "password 1")
	p, _ := e.cli.Magic(ctx, "b@example.com")
	tok1, code1 := lastLink(t, e.mails(t)[0])
	// Again within the gap: nothing new is sent, no error.
	if err := e.cli.Resend(ctx, p.Challenge); err != nil || len(e.mails(t)) != 1 {
		t.Fatalf("resend in gap %v %d", err, len(e.mails(t)))
	}
	e.clk.add(31 * time.Second)
	if err := e.cli.Resend(ctx, p.Challenge); err != nil || len(e.mails(t)) != 2 {
		t.Fatalf("resend %v", err)
	}
	if e.openLink(t, tok1) != http.StatusGone {
		t.Fatal("the replaced link still works")
	}
	_, code2 := lastLink(t, e.mails(t)[1])
	if code1 != code2 {
		if _, err := e.cli.Code(ctx, p.Challenge, code1); err != ErrCredentials {
			t.Fatalf("replaced code %v", err)
		}
	}
	for i := 0; i < 3; i++ {
		e.cli.Code(ctx, p.Challenge, "000000")
	}
	signed, err := e.cli.Code(ctx, p.Challenge, code2)
	if err != nil || signed.Session == "" {
		t.Fatalf("code %v", err)
	}
	if _, err := e.cli.Code(ctx, p.Challenge, code2); err != ErrExpired {
		t.Fatalf("code reused %v", err)
	}
	// Five wrong codes lock the challenge.
	p2, _ := e.cli.Magic(ctx, "b@example.com")
	_, good := lastLink(t, e.mails(t)[2])
	for i := 0; i < 5; i++ {
		e.cli.Code(ctx, p2.Challenge, "999999")
	}
	if _, err := e.cli.Code(ctx, p2.Challenge, good); err != ErrBusy {
		t.Fatalf("after five wrong codes %v", err)
	}
	// Links expire.
	p3, _ := e.cli.Magic(ctx, "b@example.com")
	tok3, _ := lastLink(t, e.mails(t)[3])
	e.clk.add(16 * time.Minute)
	if e.openLink(t, tok3) != http.StatusGone {
		t.Fatal("expired link works")
	}
	if r, _ := e.cli.Poll(ctx, p3.Challenge); r.Session != "" {
		t.Fatal("expired challenge signs in")
	}
}

// The same answer for known and unknown addresses; only real ones get mail.
func TestNoAccountEnumeration(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.cli.Register(ctx, "c@example.com", "password 1")
	known, err1 := e.cli.Magic(ctx, "c@example.com")
	unknown, err2 := e.cli.Magic(ctx, "nobody@example.com")
	if err1 != nil || err2 != nil || known.Challenge == "" || unknown.Challenge == "" {
		t.Fatalf("magic %v %v", err1, err2)
	}
	if n := len(e.mails(t)); n != 1 {
		t.Fatalf("mails %d", n)
	}
	// Signing up again with a taken address answers like a wrong login and
	// changes nothing.
	if _, err := e.cli.Register(ctx, "c@example.com", "new password 9"); err != ErrCredentials {
		t.Fatalf("taken address %v", err)
	}
	if _, _, err := e.cli.Login(ctx, "c@example.com", "new password 9"); err != ErrCredentials {
		t.Fatal("re-registering changed the password")
	}
	if _, _, err := e.cli.Login(ctx, "nobody@example.com", "password 1"); err != ErrCredentials {
		t.Fatalf("unknown login %v", err)
	}
	if _, err := e.cli.Register(ctx, "not an email", "password 1"); err != ErrInput {
		t.Fatalf("bad email %v", err)
	}
	if _, err := e.cli.Register(ctx, "d@example.com", "short"); err != ErrInput {
		t.Fatalf("short password %v", err)
	}
}

func TestPasswordGuessingIsThrottled(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.cli.Register(ctx, "g@example.com", "password 1")
	for i := 0; i < 10; i++ {
		if _, _, err := e.cli.Login(ctx, "g@example.com", "nope nope"); err != ErrCredentials {
			t.Fatalf("try %d %v", i, err)
		}
	}
	if _, _, err := e.cli.Login(ctx, "g@example.com", "password 1"); err != ErrBusy {
		t.Fatalf("eleventh %v", err)
	}
	e.clk.add(16 * time.Minute)
	if _, _, err := e.cli.Login(ctx, "g@example.com", "password 1"); err != nil {
		t.Fatalf("after the window %v", err)
	}
}

func TestDevicesBelongToTheAccount(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sign := func(addr string) string {
		r, _ := e.cli.Register(ctx, addr, "password 1")
		return r.Session
	}
	a, b := sign("own@example.com"), sign("other@example.com")
	if err := e.cli.Report(ctx, a, "dev1", "Mac Mini", []string{"考公助手", "写作"}); err != nil {
		t.Fatal(err)
	}
	if err := e.cli.Report(ctx, a, "dev1", "Mac Mini", []string{"考公助手", "写作", "XBot"}); err != nil {
		t.Fatal(err)
	}
	list, err := e.cli.Devices(ctx, a)
	if err != nil || len(list) != 1 || !list[0].Online || len(list[0].Bots) != 3 {
		t.Fatalf("devices %+v %v", list, err)
	}
	if other, _ := e.cli.Devices(ctx, b); len(other) != 0 {
		t.Fatalf("another account sees %+v", other)
	}
	e.clk.add(4 * time.Minute)
	if list, _ := e.cli.Devices(ctx, a); list[0].Online {
		t.Fatal("a silent device stays online")
	}
	if _, err := e.cli.Devices(ctx, "nope"); err != ErrSession {
		t.Fatalf("no session %v", err)
	}
}

func TestEnvFrom(t *testing.T) {
	get := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	if _, err := EnvFrom(get(map[string]string{"BASE_URL": "https://a.example"})); err == nil {
		t.Fatal("DATA_DIR missing")
	}
	e, err := EnvFrom(get(map[string]string{"DATA_DIR": "/d", "BASE_URL": "https://a.example"}))
	if err != nil || e.Addr != ":8790" || e.Outbox != filepath.Join("/d", "outbox") {
		t.Fatalf("%+v %v", e, err)
	}
	if _, err := EnvFrom(get(map[string]string{"DATA_DIR": "/d", "BASE_URL": "https://a.example", "MAIL_MODE": "smtp"})); err == nil {
		t.Fatal("unsupported mail mode accepted")
	}
}
