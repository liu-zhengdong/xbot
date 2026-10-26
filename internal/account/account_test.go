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

func TestSignUpByMailLinkThenPasswordLogin(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	const pass = "correct horse 1"
	p, err := e.cli.Register(ctx, "Zhengdong@Example.com", pass)
	if err != nil || p.Challenge == "" || p.Email != "zh***@example.com" {
		t.Fatalf("register %+v %v", p, err)
	}
	mails := e.mails(t)
	if len(mails) != 1 || strings.Contains(mails[0], pass) || !strings.Contains(mails[0], "To: zhengdong@example.com") {
		t.Fatalf("mail %q", mails)
	}
	tok, _ := lastLink(t, mails[0])
	// Not confirmed yet: the password does not sign in, it points back to the wait.
	_, pend, err := e.cli.Login(ctx, "zhengdong@example.com", pass)
	if err != ErrUnverified || pend.Challenge != p.Challenge {
		t.Fatalf("unverified login %+v %v", pend, err)
	}
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
	if strings.Contains(string(raw), pass) || strings.Contains(string(raw), tok) || strings.Contains(string(raw), signed.Session) {
		t.Fatal("a secret is stored in the clear")
	}
}

// Whoever controls the mailbox sets the password: a stranger's earlier sign-up for
// the same address does not survive the owner's confirmation.
func TestSignUpPasswordIsTheConfirmedOne(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.cli.Register(ctx, "a@example.com", "stranger pass"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.cli.Register(ctx, "a@example.com", "owner pass 12"); err != nil {
		t.Fatal(err)
	}
	mails := e.mails(t)
	tok, _ := lastLink(t, mails[1])
	if e.openLink(t, tok) != 200 {
		t.Fatal("confirm")
	}
	if _, _, err := e.cli.Login(ctx, "a@example.com", "stranger pass"); err != ErrCredentials {
		t.Fatalf("stranger password works: %v", err)
	}
	if _, _, err := e.cli.Login(ctx, "a@example.com", "owner pass 12"); err != nil {
		t.Fatal(err)
	}
	// The stranger's link is void now.
	tok0, _ := lastLink(t, mails[0])
	if e.openLink(t, tok0) != http.StatusGone {
		t.Fatal("old sign-up link still works")
	}
}

func TestCodeAndResendAndLimits(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p, _ := e.cli.Register(ctx, "b@example.com", "password 1")
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
	p, _ := e.cli.Register(ctx, "c@example.com", "password 1")
	tok, _ := lastLink(t, e.mails(t)[0])
	e.openLink(t, tok)
	e.cli.Poll(ctx, p.Challenge)
	known, err1 := e.cli.Magic(ctx, "c@example.com")
	unknown, err2 := e.cli.Magic(ctx, "nobody@example.com")
	if err1 != nil || err2 != nil || known.Challenge == "" || unknown.Challenge == "" {
		t.Fatalf("magic %v %v", err1, err2)
	}
	if n := len(e.mails(t)); n != 2 {
		t.Fatalf("mails %d", n)
	}
	// Signing up again with a taken address mails its owner a login link and
	// changes nothing.
	again, err := e.cli.Register(ctx, "c@example.com", "new password 9")
	if err != nil || again.Challenge == "" {
		t.Fatal(err)
	}
	last := e.mails(t)[2]
	if !strings.Contains(last, "登录 XBot") {
		t.Fatalf("taken address mail %q", last)
	}
	tok2, _ := lastLink(t, last)
	e.openLink(t, tok2)
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
	p, _ := e.cli.Register(ctx, "g@example.com", "password 1")
	tok, _ := lastLink(t, e.mails(t)[0])
	e.openLink(t, tok)
	e.cli.Poll(ctx, p.Challenge)
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
		p, _ := e.cli.Register(ctx, addr, "password 1")
		mails := e.mails(t)
		tok, _ := lastLink(t, mails[len(mails)-1])
		e.openLink(t, tok)
		r, _ := e.cli.Poll(ctx, p.Challenge)
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
