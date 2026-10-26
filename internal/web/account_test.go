package web

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"xbot/internal/account"
	"xbot/internal/bot"
	"xbot/internal/link"
)

type acctEnv struct {
	t    *testing.T
	s    *Server
	srv  *httptest.Server
	root string
}

func newAcctEnv(t *testing.T, app bool) *acctEnv {
	t.Helper()
	root := t.TempDir()
	s := &Server{Root: root, Owner: "u1", App: app}
	if _, _, err := s.EnsureToken(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"考公助手", "写作"} {
		b, err := bot.Create(root, name, "persona", []string{link.NameTerminal})
		if err != nil {
			t.Fatal(err)
		}
		if err := b.SetOwners([]string{"u1"}); err != nil {
			t.Fatal(err)
		}
	}
	mux := http.NewServeMux()
	s.Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &acctEnv{t: t, s: s, srv: srv, root: root}
}

// browser is one cookie jar: a desktop window, or a phone.
func (e *acctEnv) browser() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar}
}

func (e *acctEnv) do(c *http.Client, method, path string, body any, hdr map[string]string) (int, map[string]any) {
	e.t.Helper()
	var rdr io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rdr = strings.NewReader(string(raw))
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, rdr)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	res, err := c.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return res.StatusCode, out
}

var mailLink = regexp.MustCompile(regexp.QuoteMeta(account.LinkPath) + `\?t=([0-9a-f]+)`)
var mailCode = regexp.MustCompile(`验证码：(\d{6})`)

// lastMail is the newest file the stand-in mailer wrote.
func (e *acctEnv) lastMail() (string, string, string) {
	e.t.Helper()
	dir := filepath.Join(e.root, "account-api", "outbox")
	ents, _ := os.ReadDir(dir)
	var names []string
	for _, x := range ents {
		names = append(names, x.Name())
	}
	sort.Strings(names)
	if len(names) == 0 {
		e.t.Fatal("no mail")
	}
	raw, _ := os.ReadFile(filepath.Join(dir, names[len(names)-1]))
	l, c := mailLink.FindStringSubmatch(string(raw)), mailCode.FindStringSubmatch(string(raw))
	if l == nil || c == nil {
		e.t.Fatalf("mail %s", raw)
	}
	return string(raw), l[1], c[1]
}

// click opens the mail link and presses 确认, as the owner would in any browser.
func (e *acctEnv) click(tok string) {
	e.t.Helper()
	res, err := http.Get(e.srv.URL + account.LinkPath + "?t=" + tok)
	if err != nil || res.StatusCode != 200 {
		e.t.Fatalf("link page %v %v", res, err)
	}
	res.Body.Close()
	res, err = http.PostForm(e.srv.URL+account.LinkPath, url.Values{"t": {tok}})
	if err != nil || res.StatusCode != 200 {
		e.t.Fatalf("confirm %v %v", res, err)
	}
	res.Body.Close()
}

func acct(m map[string]any) map[string]any {
	a, _ := m["account"].(map[string]any)
	return a
}

// The way in on this computer: sign up, wait, confirm by mail, and the
// install is bound to the account; the main interface opens.
func TestAccountSignUpOpensTheOwnerInterface(t *testing.T) {
	e := newAcctEnv(t, false)
	c := e.browser()
	if code, _ := e.do(c, "GET", "/api/bots", nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("bots before sign-in %d", code)
	}
	if _, me := e.do(c, "GET", "/api/account/me", nil, nil); me["account"] != nil {
		t.Fatalf("me before %v", me)
	}
	const pass = "owner pass 123"
	code, p := e.do(c, "POST", "/api/account/register", map[string]string{"email": "zhengdong@example.com", "password": pass}, nil)
	if code != 200 || p["email"] != "zh***@example.com" || p["challenge"] == "" {
		t.Fatalf("register %d %v", code, p)
	}
	mail, tok, _ := e.lastMail()
	if strings.Contains(mail, pass) || !strings.Contains(mail, e.srv.URL+account.LinkPath) {
		t.Fatalf("mail %q", mail)
	}
	// Not confirmed: no way into the interface or the devices.
	if code, out := e.do(c, "POST", "/api/account/login", map[string]string{"email": "zhengdong@example.com", "password": pass}, nil); code != http.StatusForbidden || out["challenge"] != p["challenge"] {
		t.Fatalf("unverified login %d %v", code, out)
	}
	if code, _ := e.do(c, "GET", "/api/account/devices", nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("devices before confirm %d", code)
	}
	if code, out := e.do(c, "POST", "/api/account/poll", map[string]any{"challenge": p["challenge"]}, nil); code != 200 || out["state"] != "pending" {
		t.Fatalf("poll %d %v", code, out)
	}
	e.click(tok)
	code, out := e.do(c, "POST", "/api/account/poll", map[string]any{"challenge": p["challenge"]}, nil)
	if code != 200 || out["state"] != "done" || acct(out)["owner"] != true {
		t.Fatalf("poll after %d %v", code, out)
	}
	if code, _ := e.do(c, "GET", "/api/bots", nil, nil); code != 200 {
		t.Fatalf("owner interface %d", code)
	}
	raw, _ := os.ReadFile(filepath.Join(e.root, "account-binding.json"))
	if strings.Contains(string(raw), "zhengdong") || !strings.Contains(string(raw), `"key"`) {
		t.Fatalf("binding %s", raw)
	}
	// 我的设备: this computer with its bots.
	code, devs := e.do(c, "GET", "/api/account/devices", nil, nil)
	list, _ := devs["devices"].([]any)
	if code != 200 || len(list) != 1 {
		t.Fatalf("devices %d %v", code, devs)
	}
	d := list[0].(map[string]any)
	if d["self"] != true || d["online"] != true || len(d["bots"].([]any)) != 2 {
		t.Fatalf("self device %v", d)
	}
	// Signing out ends both the account and the owner session.
	if code, _ := e.do(c, "POST", "/api/account/logout", nil, nil); code != 200 {
		t.Fatal("logout")
	}
	if code, _ := e.do(c, "GET", "/api/bots", nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("bots after logout %d", code)
	}
	if _, me := e.do(c, "GET", "/api/account/me", nil, nil); me["account"] != nil {
		t.Fatalf("me after logout %v", me)
	}
	// Password login on another window opens the interface again.
	c2 := e.browser()
	code, out = e.do(c2, "POST", "/api/account/login", map[string]string{"email": "ZhengDong@example.com", "password": pass}, nil)
	if code != 200 || acct(out)["owner"] != true {
		t.Fatalf("login %d %v", code, out)
	}
	if code, _ := e.do(c2, "GET", "/api/bots", nil, nil); code != 200 {
		t.Fatalf("bots after login %d", code)
	}
	if code, out := e.do(c2, "POST", "/api/account/login", map[string]string{"email": "zhengdong@example.com", "password": "wrong pass 1"}, nil); code != http.StatusUnauthorized || out["error"] != "credentials" {
		t.Fatalf("wrong password %d %v", code, out)
	}
}

// Through a tunnel, an account cannot claim an unbound install; and once bound,
// another account never gets the owner session.
func TestAccountBindingNeedsTheOwner(t *testing.T) {
	e := newAcctEnv(t, false)
	tunnel := map[string]string{"Cf-Connecting-Ip": "203.0.113.9"}
	signUp := func(c *http.Client, email string, hdr map[string]string) map[string]any {
		_, p := e.do(c, "POST", "/api/account/register", map[string]string{"email": email, "password": "password 12"}, hdr)
		_, tok, _ := e.lastMail()
		e.click(tok)
		_, out := e.do(c, "POST", "/api/account/poll", map[string]any{"challenge": p["challenge"]}, hdr)
		return out
	}
	phone := e.browser()
	out := signUp(phone, "stranger@example.com", tunnel)
	if acct(out)["owner"] != false {
		t.Fatalf("a tunnel sign-in bound the install: %v", out)
	}
	if code, _ := e.do(phone, "GET", "/api/bots", nil, tunnel); code != http.StatusUnauthorized {
		t.Fatalf("stranger reached the interface %d", code)
	}
	if _, devs := e.do(phone, "GET", "/api/account/devices", nil, tunnel); len(devs["devices"].([]any)) != 0 {
		t.Fatalf("stranger sees devices %v", devs)
	}
	desk := e.browser()
	if out := signUp(desk, "owner@example.com", nil); acct(out)["owner"] != true {
		t.Fatalf("owner on this computer %v", out)
	}
	// Now bound: the stranger signing in here gets nothing more.
	other := e.browser()
	code, out := e.do(other, "POST", "/api/account/login", map[string]string{"email": "stranger@example.com", "password": "password 12"}, nil)
	if code != 200 || acct(out)["owner"] != false {
		t.Fatalf("second account %d %v", code, out)
	}
	if code, _ := e.do(other, "GET", "/api/bots", nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("second account reached the interface %d", code)
	}
	// The owner from the phone, through the tunnel, gets the interface.
	code, out = e.do(phone, "POST", "/api/account/login", map[string]string{"email": "owner@example.com", "password": "password 12"}, tunnel)
	if code != 200 || acct(out)["owner"] != true {
		t.Fatalf("owner on the phone %d %v", code, out)
	}
	if code, _ := e.do(phone, "GET", "/api/bots", nil, tunnel); code != 200 {
		t.Fatalf("owner phone %d", code)
	}
}

// 用邮件里的链接登录 and the code fallback.
func TestAccountMailLinkAndCode(t *testing.T) {
	e := newAcctEnv(t, true)
	c := e.browser()
	_, p := e.do(c, "POST", "/api/account/register", map[string]string{"email": "a@example.com", "password": "password 12"}, nil)
	_, _, code := e.lastMail()
	if st, out := e.do(c, "POST", "/api/account/code", map[string]any{"challenge": p["challenge"], "code": "000000"}, nil); st != http.StatusUnauthorized || out["error"] != "credentials" {
		t.Fatalf("wrong code %d %v", st, out)
	}
	if st, out := e.do(c, "POST", "/api/account/code", map[string]any{"challenge": p["challenge"], "code": code}, nil); st != 200 || acct(out)["owner"] != true || acct(out)["app"] != true {
		t.Fatalf("code %d %v", st, out)
	}
	// The desktop app may sign out of the account.
	if st, _ := e.do(c, "POST", "/api/account/logout", nil, nil); st != 200 {
		t.Fatalf("app logout %d", st)
	}
	_, m := e.do(c, "POST", "/api/account/magic", map[string]string{"email": "a@example.com"}, nil)
	_, tok, _ := e.lastMail()
	e.click(tok)
	if st, out := e.do(c, "POST", "/api/account/poll", map[string]any{"challenge": m["challenge"]}, nil); st != 200 || acct(out)["owner"] != true {
		t.Fatalf("magic %d %v", st, out)
	}
	// An unknown address answers the same and gets no mail.
	before, _, _ := e.lastMail()
	if st, out := e.do(c, "POST", "/api/account/magic", map[string]string{"email": "nobody@example.com"}, nil); st != 200 || out["challenge"] == "" {
		t.Fatalf("unknown %d %v", st, out)
	}
	if after, _, _ := e.lastMail(); after != before {
		t.Fatal("mail sent to an unknown address")
	}
	// A cross-site page cannot drive these routes.
	if st, _ := e.do(c, "POST", "/api/account/login", map[string]string{"email": "a@example.com", "password": "password 12"}, map[string]string{"Origin": "https://evil.example"}); st != http.StatusForbidden {
		t.Fatalf("cross-origin %d", st)
	}
}

// The old install code still works underneath (debugging and regression),
// separately from accounts.
func TestInstallCodeStillWorks(t *testing.T) {
	e := newAcctEnv(t, false)
	c := e.browser()
	if st, _ := e.do(c, "POST", "/api/login", map[string]string{"token": e.s.token}, nil); st != 200 {
		t.Fatalf("install code %d", st)
	}
	if st, _ := e.do(c, "GET", "/api/bots", nil, nil); st != 200 {
		t.Fatalf("bots %d", st)
	}
	if _, me := e.do(c, "GET", "/api/account/me", nil, nil); me["account"] != nil {
		t.Fatalf("the install code made an account %v", me)
	}
	launch, err := e.s.NewLaunchCode()
	if err != nil {
		t.Fatal(err)
	}
	d := e.browser()
	res, err := d.Get(e.srv.URL + "/app/launch?code=" + launch)
	if err != nil || res.StatusCode >= 400 {
		t.Fatalf("launch %v %v", res, err)
	}
	res.Body.Close()
	if st, _ := e.do(d, "GET", "/api/bots", nil, nil); st != 200 {
		t.Fatalf("launch session %d", st)
	}
}

// With XBOT_ACCOUNT_API set, serve uses the hosted service over HTTP: the mail
// link points at the service, and confirming it there still lets this window in.
func TestAccountWithHostedService(t *testing.T) {
	data := t.TempDir()
	hosted := httptest.NewServer(nil)
	t.Cleanup(hosted.Close)
	svc, err := account.Open(account.Env{DataDir: data, BaseURL: hosted.URL, Outbox: filepath.Join(data, "outbox")})
	if err != nil {
		t.Fatal(err)
	}
	hosted.Config.Handler = svc.Handler()

	root := t.TempDir()
	s := &Server{Root: root, Owner: "u1", Account: account.NewRemote(hosted.URL)}
	if _, _, err := s.EnsureToken(); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	e := &acctEnv{t: t, s: s, srv: srv, root: root}
	c := e.browser()
	_, p := e.do(c, "POST", "/api/account/register", map[string]string{"email": "h@example.com", "password": "password 12"}, nil)
	ents, _ := os.ReadDir(filepath.Join(data, "outbox"))
	if len(ents) != 1 {
		t.Fatalf("hosted outbox %v", ents)
	}
	raw, _ := os.ReadFile(filepath.Join(data, "outbox", ents[0].Name()))
	if !strings.Contains(string(raw), hosted.URL+account.LinkPath) {
		t.Fatalf("link does not point at the service: %s", raw)
	}
	tok := mailLink.FindStringSubmatch(string(raw))[1]
	res, err := http.PostForm(hosted.URL+account.LinkPath, url.Values{"t": {tok}})
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("confirm %v %v", res, err)
	}
	res.Body.Close()
	if st, out := e.do(c, "POST", "/api/account/poll", map[string]any{"challenge": p["challenge"]}, nil); st != 200 || acct(out)["owner"] != true || acct(out)["hosted"] != true {
		t.Fatalf("poll %d %v", st, out)
	}
	if st, _ := e.do(c, "GET", "/api/bots", nil, nil); st != 200 {
		t.Fatalf("owner interface %d", st)
	}
	if _, err := os.Stat(filepath.Join(root, "account-api")); err == nil {
		t.Fatal("the local stand-in ran although a hosted service is set")
	}
	if st, _ := e.do(c, "GET", "/account/v1/link?t=x", nil, nil); st == 200 {
		t.Fatal("serve mounted the link page although the service is hosted")
	}
}
