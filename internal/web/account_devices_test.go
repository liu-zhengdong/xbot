package web

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"xbot/internal/account"
	"xbot/internal/bot"
	"xbot/internal/deviceexec"
	"xbot/internal/deviceidentity"
	"xbot/internal/fsutil"
	"xbot/internal/inbox"
)

// The test wire only replaces transport. Account membership, web routes, Jobs,
// task control and artifact downloads use their production implementations.
type accountHTTPDeviceWire struct {
	accounts account.Client
	token    string
	base     string
	account  string
}

func (w accountHTTPDeviceWire) Devices(ctx context.Context) ([]account.DeviceView, error) {
	return w.accounts.BindingDevices(ctx, w.token)
}
func (w accountHTTPDeviceWire) Do(ctx context.Context, source deviceexec.Source, target, method, path string, body []byte) (int, []byte, string, error) {
	source.Account = w.account
	req, err := http.NewRequestWithContext(ctx, method, w.base+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, "", err
	}
	// This trusted test header is consumed only by the isolated target fixture.
	raw, _ := json.Marshal(source)
	req.Header.Set("X-Test-Device-Source", string(raw))
	if source.Owner {
		req.Header.Set("X-Test-Device-Owner", "true")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, "", err
	}
	defer res.Body.Close()
	raw, err = io.ReadAll(io.LimitReader(res.Body, 1<<20))
	return res.StatusCode, raw, res.Header.Get("Content-Type"), err
}

func accountBrowserSession(t *testing.T, e *acctEnv, c *http.Client) string {
	t.Helper()
	u, _ := url.Parse(e.srv.URL)
	for _, cookie := range c.Jar.Cookies(u) {
		if cookie.Name == accountCookie {
			return cookie.Value
		}
	}
	t.Fatal("account session cookie missing")
	return ""
}

func boundAccountBrowser(t *testing.T) (*acctEnv, *http.Client, accountBinding, string) {
	t.Helper()
	e := newAcctEnv(t, false)
	t.Cleanup(e.s.CloseDeviceExecution)
	c := e.browser()
	if status, _ := e.do(c, "POST", "/api/account/register", map[string]string{"email": "devices@example.com", "password": "correct horse battery"}, nil); status != 200 {
		t.Fatal(status)
	}
	e.claim(c, nil)
	b, err := e.s.readBinding()
	if err != nil || b.Token == "" {
		t.Fatal("missing installation credential")
	}
	return e, c, b, accountBrowserSession(t, e, c)
}

func accountDeviceRaw(t *testing.T, c *http.Client, method, address string) (int, []byte, http.Header) {
	t.Helper()
	req, err := http.NewRequest(method, address, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, raw, res.Header
}

func accountDeviceWaitDetail(t *testing.T, m *deviceexec.Manager, source deviceexec.Source, ref string) deviceexec.Detail {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		d, err := m.Detail(source, ref, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		if d.Task.Status != "running" {
			return d
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("device task did not finish")
	return deviceexec.Detail{}
}

type taskDirectoryClient struct {
	account.Client
	List    []account.DeviceView
	Problem error
}

func (c taskDirectoryClient) BindingDevices(context.Context, string) ([]account.DeviceView, error) {
	return c.List, c.Problem
}

type taskPartialWire struct {
	t             *testing.T
	Bot, SourceID string
}

func (w taskPartialWire) Devices(context.Context) ([]account.DeviceView, error) { return nil, nil }
func (w taskPartialWire) Do(_ context.Context, _ deviceexec.Source, target, method, path string, _ []byte) (int, []byte, string, error) {
	if target == "broken" {
		return 0, nil, "", errors.New("target unreachable")
	}
	if target != "reachable" {
		w.t.Errorf("offline target was contacted: %s", target)
	}
	u, _ := url.Parse(path)
	if method != "GET" || u.Query().Get("bot") != w.Bot || u.Query().Get("source_device_id") != w.SourceID {
		w.t.Errorf("chat task source filter lost: %s %s", method, path)
	}
	raw, _ := json.Marshal(map[string]any{"tasks": []deviceexec.Task{{Ref: "kept", Bot: w.Bot, DeviceID: target, Status: "running"}}, "next_offset": 1, "has_more": false})
	return 200, raw, "application/json", nil
}
func TestAccountBotDeviceTasksPreserveUnavailableFacts(t *testing.T) {
	e, c, binding, _ := boundAccountBrowser(t)
	original := e.s.acct.client
	list := []account.DeviceView{{ID: "reachable", Name: "Reachable", Online: true}, {ID: "offline", Name: "Offline", Online: false}, {ID: "broken", Name: "Broken", Online: true}}
	e.s.acct.client = taskDirectoryClient{Client: original, List: list}
	e.s.SetDeviceClient(taskPartialWire{t: t, Bot: "考公助手", SourceID: binding.DeviceID})
	path := "/api/bots/" + url.PathEscape("考公助手") + "/device-tasks"
	status, out := e.do(c, "GET", path, nil, nil)
	if status != 200 {
		t.Fatalf("partial result failed: %d %v", status, out)
	}
	tasks, ok := out["tasks"].([]any)
	if !ok || len(tasks) != 1 || tasks[0].(map[string]any)["ref"] != "kept" {
		t.Fatalf("reachable task lost: %v", out)
	}
	unavailable, ok := out["unavailable_devices"].([]any)
	if !ok || len(unavailable) != 2 {
		t.Fatalf("unavailable states hidden: %v", out)
	}
	if out["tasks_truncated"] != false {
		t.Fatal("small task list incorrectly marked truncated")
	}
	e.s.acct.client = taskDirectoryClient{Client: original, Problem: errors.New("account: service unreachable")}
	if status, _ := e.do(c, "GET", path, nil, nil); status != 502 {
		t.Fatalf("account outage appeared as empty tasks: %d", status)
	}
}

func TestAccountDeviceManagementHTTP(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX process fixture")
	}
	t.Setenv("XBOT_VAULT_SOCKET", "")
	e, c, local, session := boundAccountBrowser(t)
	ctx := context.Background()
	target, err := e.s.acct.client.BindDevice(ctx, session, account.DeviceReport{ID: "remote-target", Name: "Remote target", OS: "linux", Capabilities: []string{"shell", "files"}})
	if err != nil {
		t.Fatal(err)
	}
	m, err := deviceexec.New(t.TempDir(), target.DeviceID, "Remote target")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var source deviceexec.Source
		if json.Unmarshal([]byte(r.Header.Get("X-Test-Device-Source")), &source) != nil {
			http.Error(w, "missing test transport identity", 401)
			return
		}
		source.Owner = r.Header.Get("X-Test-Device-Owner") == "true"
		m.HandlerFor(source).ServeHTTP(w, r)
	}))
	t.Cleanup(targetServer.Close)
	e.s.SetDeviceClient(accountHTTPDeviceWire{accounts: e.s.acct.client, token: local.Token, account: local.Key, base: targetServer.URL})
	source := deviceexec.Source{Account: local.Key, DeviceID: local.DeviceID, Bot: "考公助手", ContextID: "main"}
	artifact := make([]byte, 3*deviceexec.MaxArtifactChunk)
	for i := range artifact {
		artifact[i] = byte(i % 256)
	}
	path := filepath.Join(m.Root, "binary.dat")
	if err := os.WriteFile(path, artifact, 0600); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]any{"command": fmt.Sprintf("head -c 100000 '%s'", path)})
	if _, err := m.Submit(ctx, source, deviceexec.Call{Ref: "output", Kind: "exec", Tool: "sh", Args: args, Description: "bounded output", Artifacts: []string{path}}); err != nil {
		t.Fatal(err)
	}
	done := accountDeviceWaitDetail(t, m, source, "output")
	if done.Task.Status != "completed" || len(done.Task.Artifacts) != 1 {
		t.Fatalf("target task: %+v", done.Task)
	}
	base := "/api/account/devices/" + target.DeviceID
	status, out := e.do(c, "GET", base, nil, nil)
	device, _ := out["device"].(map[string]any)
	tasks, _ := out["tasks"].([]any)
	if status != 200 || device["self"] != false || device["os"] != "linux" || len(tasks) != 1 {
		t.Fatalf("detail: %d %v", status, out)
	}
	if status, out := e.do(c, "PATCH", base, map[string]string{"name": "  Saved remote name  "}, nil); status != 200 || out["device"].(map[string]any)["name"] != "Saved remote name" {
		t.Fatalf("rename: %d %v", status, out)
	}
	if err := e.s.acct.client.ReportMetadata(ctx, target.Token, account.DeviceReport{ID: target.DeviceID, Name: "stale hostname", OS: "linux", Capabilities: []string{"shell", "files"}}); err != nil {
		t.Fatal(err)
	}
	if status, out := e.do(c, "GET", base, nil, nil); status != 200 || out["device"].(map[string]any)["name"] != "Saved remote name" {
		t.Fatal("heartbeat overwrote saved name")
	}
	status, raw, _ := accountDeviceRaw(t, c, "GET", e.srv.URL+base+"/tasks/output?offset=2&limit=999999")
	var detail deviceexec.Detail
	if err := json.Unmarshal(raw, &detail); err != nil || status != 200 || len(detail.Output) > deviceexec.MaxOutputLimit || detail.Offset != 2 || detail.NextOffset <= 2 {
		t.Fatalf("bounded output: code=%d bytes=%d offset=%d next=%d err=%v", status, len(detail.Output), detail.Offset, detail.NextOffset, err)
	}
	artifactURL := e.srv.URL + base + "/tasks/output/artifacts/" + done.Task.Artifacts[0].ID
	status, raw, header := accountDeviceRaw(t, c, "GET", artifactURL+"?offset=17&limit=999999")
	if status != 200 || header.Get("Content-Type") != "application/octet-stream" || len(raw) != deviceexec.MaxArtifactChunk || !bytes.Equal(raw, artifact[17:17+deviceexec.MaxArtifactChunk]) {
		t.Fatalf("artifact bytes were changed or unbounded: status=%d bytes=%d", status, len(raw))
	}
	if status, raw, _ := accountDeviceRaw(t, c, "GET", artifactURL+"?offset=29&limit=73"); status != 200 || !bytes.Equal(raw, artifact[29:102]) {
		t.Fatalf("artifact range was not preserved: status=%d bytes=%d", status, len(raw))
	}
	if status, _, _ := accountDeviceRaw(t, c, "GET", artifactURL+"?offset=-1"); status != 400 {
		t.Fatalf("invalid artifact range: %d", status)
	}
	longArgs, _ := json.Marshal(map[string]any{"command": "printf ready; sleep 30", "timeout_seconds": 1})
	if _, err := m.Submit(ctx, source, deviceexec.Call{Ref: "long", Kind: "exec", Tool: "sh", Args: longArgs, Description: "stop original job"}); err != nil {
		t.Fatal(err)
	}
	if status, out := e.do(c, "GET", base+"/tasks/long", nil, nil); status != 200 || out["task"].(map[string]any)["status"] != "running" {
		t.Fatalf("live status: %d %v", status, out)
	}
	if status, _ := e.do(c, "POST", base+"/tasks/long/stop", nil, nil); status != 200 {
		t.Fatalf("stop: %d", status)
	}
	if d := accountDeviceWaitDetail(t, m, source, "long"); d.Task.Status != "cancelled" {
		t.Fatalf("stop did not reach original Jobs: %+v", d.Task)
	}
	if status, out := e.do(c, "GET", base+"/tasks?bot="+url.QueryEscape(source.Bot), nil, nil); status != 200 || len(out["tasks"].([]any)) != 2 {
		t.Fatalf("task list/filter: %d %v", status, out)
	}
	if status, out := e.do(c, "DELETE", base, nil, nil); status != 200 || out["unbound"] != true {
		t.Fatalf("unbind: %d %v", status, out)
	}
	if _, err := e.s.acct.client.AuthenticateBinding(ctx, target.Token); err != account.ErrBinding {
		t.Fatalf("deleted target still authenticated: %v", err)
	}
	if status, _ := e.do(c, "GET", base, nil, nil); status != 404 {
		t.Fatalf("deleted target detail: %d", status)
	}
}

func TestAccountDeviceHTTPPermissionsAndGeneration(t *testing.T) {
	e, owner, local, session := boundAccountBrowser(t)
	ctx := context.Background()
	target, err := e.s.acct.client.BindDevice(ctx, session, account.DeviceReport{ID: "target", Name: "Target", Capabilities: []string{"shell"}})
	if err != nil {
		t.Fatal(err)
	}
	base := "/api/account/devices/" + target.DeviceID
	foreignSession, err := e.s.acct.client.Register(ctx, "foreign@example.com", "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(e.srv.URL)
	accountOnly := e.browser()
	accountOnly.Jar.SetCookies(u, []*http.Cookie{{Name: accountCookie, Value: session, Path: "/"}})
	foreign := e.browser()
	// Holding the local owner cookie does not grant another account's member.
	foreign.Jar.SetCookies(u, owner.Jar.Cookies(u))
	foreign.Jar.SetCookies(u, []*http.Cookie{{Name: accountCookie, Value: foreignSession.Session, Path: "/"}})
	for _, route := range []struct{ method, suffix string }{
		{"GET", ""}, {"PATCH", ""}, {"DELETE", ""}, {"GET", "/tasks"},
		{"GET", "/tasks/ref"}, {"POST", "/tasks/ref/stop"}, {"GET", "/tasks/ref/artifacts/file"},
	} {
		body := map[string]string{"name": "forbidden rename"}
		if status, _ := e.do(accountOnly, route.method, base+route.suffix, body, nil); status != 401 {
			t.Fatalf("account without local owner accessed %s %s: %d", route.method, route.suffix, status)
		}
		if status, _ := e.do(foreign, route.method, base+route.suffix, body, nil); status != 404 {
			t.Fatalf("foreign account accessed target via %s %s: %d", route.method, route.suffix, status)
		}
	}
	if _, err := e.s.acct.client.AuthenticateBinding(ctx, target.Token); err != nil {
		t.Fatal("forbidden delete changed target membership")
	}
	// Equal device IDs in another account cannot expose this install's bots.
	other, err := e.s.acct.client.BindDevice(ctx, foreignSession.Session, account.DeviceReport{ID: local.DeviceID, Name: "Foreign collision", Bots: []string{"foreign-bot"}})
	if err != nil {
		t.Fatal(err)
	}
	otherList, _ := e.s.acct.client.BindingDevices(ctx, other.Token)
	row := e.s.accountDeviceRow(otherList[0])
	if row.Self || len(row.Bots) != 1 || row.Bots[0].Name != "foreign-bot" {
		t.Fatal("device ID collision exposed local owner bot list")
	}
	ownList, _ := e.s.acct.client.BindingDevices(ctx, local.Token)
	for _, d := range ownList {
		if d.ID == local.DeviceID {
			if !e.s.accountDeviceRow(d).Self {
				t.Fatal("current local binding generation was not recognized")
			}
			d.BindingID = "stale-local-generation"
			if e.s.accountDeviceRow(d).Self {
				t.Fatal("stale local binding generation was recognized as self")
			}
		}
	}
}

func TestAccountDeviceExecutionRevocationGenerations(t *testing.T) {
	e, c, local, session := boundAccountBrowser(t)
	ctx := context.Background()
	source, err := e.s.acct.client.BindDevice(ctx, session, account.DeviceReport{ID: "source", Name: "Source"})
	if err != nil {
		t.Fatal(err)
	}
	identity := deviceidentity.Identity{AccountKey: local.Key, DeviceID: source.DeviceID, BindingID: source.ID, TargetBindingID: local.BindingID, Bot: "考公助手", ContextID: "main"}
	if err := e.s.ValidateDeviceExecution(ctx, identity); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"account", "source", "target"} {
		invalid := identity
		switch field {
		case "account":
			invalid.AccountKey = "foreign-account"
		case "source":
			invalid.BindingID = "old-source-generation"
		case "target":
			invalid.TargetBindingID = "old-target-generation"
		}
		if err := e.s.ValidateDeviceExecution(ctx, invalid); err == nil {
			t.Fatalf("invalid %s generation authenticated", field)
		}
	}
	if err := e.s.acct.client.DeleteDevice(ctx, session, source.DeviceID); err != nil {
		t.Fatal(err)
	}
	if err := e.s.ValidateDeviceExecution(ctx, identity); err == nil {
		t.Fatal("deleted source retained execution eligibility")
	}
	rebound, err := e.s.acct.client.BindDevice(ctx, session, account.DeviceReport{ID: source.DeviceID, Name: "Source"})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.s.ValidateDeviceExecution(ctx, identity); err == nil {
		t.Fatal("source rebind restored old execution ticket")
	}
	identity.BindingID = rebound.ID
	if err := e.s.ValidateDeviceExecution(ctx, identity); err != nil {
		t.Fatal(err)
	}
	if err := e.s.acct.client.DeleteDevice(ctx, session, local.DeviceID); err != nil {
		t.Fatal(err)
	}
	if err := e.s.ValidateDeviceExecution(ctx, identity); err == nil {
		t.Fatal("deleted target accepted execution")
	}
	e.claim(c, nil)
	current, _ := e.s.readBinding()
	if err := e.s.ValidateDeviceExecution(ctx, identity); err == nil {
		t.Fatal("target rebind restored old execution ticket")
	}
	identity.TargetBindingID = current.BindingID
	if err := e.s.ValidateDeviceExecution(ctx, identity); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "/api/bots", nil)
	if err := e.s.AuthorizeAccountPipe(local.Token, request); err == nil || request.Header.Get("Cookie") != "" {
		t.Fatal("old owner ticket acquired console credential")
	}
	if err := e.s.AuthorizeAccountPipe(current.Token, request); err != nil || request.Header.Get("Cookie") == "" {
		t.Fatalf("current owner binding could not open console: %v", err)
	}
	if _, err := request.Cookie(accountCookie); err == nil {
		t.Fatal("binding credential invented a UI account session")
	}
	console := httptest.NewRequest("GET", "/api/account/devices", nil).WithContext(deviceidentity.WithConsoleSession(ctx, session))
	if err := e.s.AuthorizeAccountPipe(current.Token, console); err != nil {
		t.Fatal(err)
	}
	if cookie, err := console.Cookie(accountCookie); err != nil || cookie.Value != session {
		t.Fatal("authenticated console ticket lost its actual account session")
	}
	foreign, err := e.s.acct.client.Register(ctx, "foreign-console@example.com", "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	invalidConsole := httptest.NewRequest("GET", "/api/account/devices", nil).WithContext(deviceidentity.WithConsoleSession(ctx, foreign.Session))
	if err := e.s.AuthorizeAccountPipe(current.Token, invalidConsole); err == nil || invalidConsole.Header.Get("Cookie") != "" {
		t.Fatal("foreign console session acquired local owner credential")
	}
	if err := e.s.acct.client.Logout(ctx, session); err != nil {
		t.Fatal(err)
	}
	expiredConsole := httptest.NewRequest("GET", "/api/account/devices", nil).WithContext(deviceidentity.WithConsoleSession(ctx, session))
	if err := e.s.AuthorizeAccountPipe(current.Token, expiredConsole); err == nil || expiredConsole.Header.Get("Cookie") != "" {
		t.Fatal("ended console session acquired local owner credential")
	}
}

func TestAccountLegacyRevocationCannotMigrateAutomatically(t *testing.T) {
	e, c, binding, session := boundAccountBrowser(t)
	ctx := context.Background()
	if err := e.s.acct.client.DeleteDevice(ctx, session, binding.DeviceID); err != nil {
		t.Fatal(err)
	}
	// Restore the exact old file shape: no binding token or generation.
	binding.Token, binding.BindingID = "", ""
	raw, _ := fsutil.Marshal(binding)
	if err := fsutil.WriteAtomic(e.s.bindingPath(), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := fsutil.WriteAtomic(e.s.accountSessionPath(), []byte(session), 0600); err != nil {
		t.Fatal(err)
	}
	started := make(chan string, 2)
	stop := e.s.StartAccountPipe(ctx, func(ctx context.Context, token, _ string) error {
		started <- token
		<-ctx.Done()
		return ctx.Err()
	})
	defer stop()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(e.s.accountSessionPath()); os.IsNotExist(err) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("legacy session migration did not finish")
		}
		time.Sleep(5 * time.Millisecond)
	}
	checkRevoked := func() {
		t.Helper()
		current, err := e.s.readBinding()
		if err != nil || current.Token != "" || current.BindingID != "" {
			t.Fatal("automatic migration created credentials for revoked legacy member")
		}
		list, err := e.s.acct.client.Devices(ctx, session)
		if err != nil || len(list) != 0 {
			t.Fatalf("automatic migration restored revoked member: count=%d err=%v", len(list), err)
		}
		select {
		case <-started:
			t.Fatal("revoked legacy member started standby")
		default:
		}
	}
	checkRevoked()
	if status, _ := e.do(c, "GET", "/api/account/me", nil, nil); status != 200 {
		t.Fatalf("UI identity check failed: %d", status)
	}
	checkRevoked()
	if status, _ := e.do(c, "POST", "/api/account/login", map[string]string{"email": "devices@example.com", "password": "correct horse battery"}, nil); status != 200 {
		t.Fatalf("ordinary UI login failed: %d", status)
	}
	checkRevoked()
	e.claim(c, nil)
	current, _ := e.s.readBinding()
	if current.Token == "" || current.BindingID == "" {
		t.Fatal("explicit owner claim did not restore installation binding")
	}
	expectPipeEvent(t, started, current.Token)
}

func TestAccountMessageDeviceSelectionPersistsOwnedEvent(t *testing.T) {
	e, c, _, session := boundAccountBrowser(t)
	ctx := context.Background()
	target, err := e.s.acct.client.BindDevice(ctx, session, account.DeviceReport{ID: "target", Name: "Target", Capabilities: []string{"shell"}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := bot.Load(e.root, "考公助手")
	if err != nil {
		t.Fatal(err)
	}
	defer bot.CloseStore(b)
	lock, ok, err := b.TryLockHandle()
	if err != nil || !ok {
		t.Fatal("cannot hold bot for queued-message fixture")
	}
	defer lock.Release()
	base := "/api/bots/" + url.PathEscape(b.Name()) + "/messages"
	foreignBot := mustWebBot(t, e.root, "private-bot", "other-owner")
	defer bot.CloseStore(foreignBot)
	if status, _ := e.do(c, "POST", "/api/bots/private-bot/messages", map[string]string{"text": "not mine", "device_id": target.DeviceID}, nil); status != 404 {
		t.Fatalf("device selection bypassed bot ownership: %d", status)
	}
	foreignBox, err := inbox.Open(foreignBot.InboxPath())
	if err != nil || len(foreignBox.Pending()) != 0 {
		t.Fatalf("device selection enqueued foreign bot event: %v", err)
	}
	for _, id := range []string{"not-a-member", target.DeviceID} {
		if id == target.DeviceID {
			if err := e.s.acct.client.ReportMetadata(ctx, target.Token, account.DeviceReport{ID: id, Name: "Target"}); err != nil {
				t.Fatal(err)
			}
		}
		if status, _ := e.do(c, "POST", base, map[string]string{"text": "must not enqueue", "device_id": id}, nil); status != 409 {
			t.Fatalf("unavailable device was accepted: %d", status)
		}
		box, err := inbox.Open(b.InboxPath())
		if err != nil || len(box.Pending()) != 0 {
			t.Fatalf("unavailable target enqueued message: %v", err)
		}
	}
	if err := e.s.acct.client.ReportMetadata(ctx, target.Token, account.DeviceReport{ID: target.DeviceID, Name: "Target", Capabilities: []string{"shell"}}); err != nil {
		t.Fatal(err)
	}
	// Advancing the real account service clock marks a silent member offline.
	store, err := account.NewFileStore(filepath.Join(e.root, "account-api"))
	if err != nil {
		t.Fatal(err)
	}
	realClient := e.s.acct.client
	later := time.Now().Add(4 * time.Minute)
	e.s.acct.client = account.Local{S: account.New(store, nil, account.Config{Now: func() time.Time { return later }})}
	if status, _ := e.do(c, "POST", base, map[string]string{"text": "offline device input", "device_id": target.DeviceID}, nil); status != 409 {
		t.Fatalf("offline device accepted: %d", status)
	}
	offlineBox, err := inbox.Open(b.InboxPath())
	if err != nil || len(offlineBox.Pending()) != 0 {
		t.Fatalf("offline device enqueued event: %v", err)
	}
	e.s.acct.client = realClient
	requestCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	clientID := "web_" + strings.Repeat("b", 32)
	raw, _ := json.Marshal(map[string]string{"text": "queued device input", "device_id": target.DeviceID, "client_id": clientID})
	req, _ := http.NewRequestWithContext(requestCtx, "POST", e.srv.URL+base, bytes.NewReader(raw))
	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("available target rejected: %d", res.StatusCode)
	}
	scanner := bufio.NewScanner(res.Body)
	queued := false
	for scanner.Scan() {
		line := scanner.Text()
		if line == "event: queued" {
			queued = true
		}
		if line == "" {
			break
		}
	}
	if !queued {
		t.Fatal("available target did not reach real inbox path")
	}
	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	pending := box.Pending()
	if len(pending) != 1 || pending[0].ID != clientID || pending[0].DeviceID != target.DeviceID || pending[0].Content != "queued device input" {
		t.Fatalf("selected device not frozen on event: %+v", pending)
	}
	cancel()
}
