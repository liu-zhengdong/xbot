//go:build smoke

package smoke

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"xbot/internal/browsertest"
)

// Independent acceptance for CLI platform batches 0/1. Only the provider is
// scripted: the binary, Shell, operation transport, persistence and UI are real.
// Research and remote runner are explicitly outside the first batch's contract.
type cliProfileCase struct {
	Commands []string
	// Pause before this command, with the caller's real turn still holding its
	// processing lock. -1 selects a natural short turn instead.
	PauseBefore int
	entered     chan struct{}
	release     chan struct{}
	once        sync.Once
}

type cliProfileModel struct {
	server *httptest.Server
	mu     sync.Mutex
	cases  map[string]*cliProfileCase
	result map[string]map[string]string
}

func newCLIProfileModel(t *testing.T) *cliProfileModel {
	t.Helper()
	m := &cliProfileModel{cases: map[string]*cliProfileCase{}, result: map[string]map[string]string{}}
	m.server = httptest.NewServer(http.HandlerFunc(m.handle))
	t.Cleanup(m.server.Close)
	return m
}

func (m *cliProfileModel) add(cue string, pause int, commands ...string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cases[cue] = &cliProfileCase{Commands: commands, PauseBefore: pause, entered: make(chan struct{}), release: make(chan struct{})}
	m.result[cue] = map[string]string{}
}

func (m *cliProfileModel) release(cue string) {
	m.mu.Lock()
	c := m.cases[cue]
	m.mu.Unlock()
	c.once.Do(func() { close(c.release) })
}

func (m *cliProfileModel) results(cue string) map[string]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]string{}
	for k, v := range m.result[cue] {
		out[k] = v
	}
	return out
}

func (m *cliProfileModel) handle(w http.ResponseWriter, r *http.Request) {
	// These controls belong only to the independent fixture. The product never
	// receives them, and the browser still talks to the shipped HTTP/SSE APIs.
	if strings.HasPrefix(r.URL.Path, "/validation/") {
		cue := r.URL.Query().Get("cue")
		m.mu.Lock()
		c := m.cases[cue]
		m.mu.Unlock()
		if c == nil {
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/validation/release" {
			m.release(cue)
		}
		held := false
		select {
		case <-c.entered:
			held = true
		default:
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"held": held, "results": m.results(cue)})
		return
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	var req stubRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	caller, _ := classify(req)
	if caller == callerCompaction || caller == callerExtract {
		streamText(w, []string{"No earlier facts needed for this scripted acceptance."})
		return
	}
	var wire struct {
		Messages []struct {
			Role       string `json:"role"`
			Content    string `json:"content"`
			ToolCallID string `json:"tool_call_id"`
		} `json:"messages"`
	}
	_ = json.Unmarshal(raw, &wire)
	m.mu.Lock()
	var cue string
	for _, msg := range req.Messages {
		if msg.Role != "user" {
			continue
		}
		for candidate := range m.cases {
			if strings.Contains(msg.Content, candidate) {
				cue = candidate
			}
		}
	}
	c := m.cases[cue]
	if c == nil {
		m.mu.Unlock()
		streamOwner(w, req, "scripted acceptance ready")
		return
	}
	for _, msg := range wire.Messages {
		if msg.Role == "tool" && strings.HasPrefix(msg.ToolCallID, cue+"_") {
			m.result[cue][msg.ToolCallID] = msg.Content
		}
	}
	step := 0
	for step < len(c.Commands) {
		if _, ok := m.result[cue][fmt.Sprintf("%s_%d", cue, step)]; !ok {
			break
		}
		step++
	}
	m.mu.Unlock()
	if step == c.PauseBefore {
		select {
		case <-c.entered:
		default:
			close(c.entered)
		}
		select {
		case <-c.release:
		case <-r.Context().Done():
			return
		}
	}
	if step < len(c.Commands) {
		streamTool(w, fmt.Sprintf("%s_%d", cue, step), "sh", argsJSON(map[string]string{
			"description": "Verify CLI profile through the current running instance",
			"command":     c.Commands[step],
		}))
		return
	}
	// A short turn uses the ordinary scripted provider delay, not a held gate
	// or a multi-second window that lets the periodic UI poll mask a regression.
	if c.PauseBefore < 0 {
		select {
		case <-time.After(stubFirstDelay):
		case <-r.Context().Done():
			return
		}
	}
	if sawTool(req, "send_message_to_user") {
		// The terminal's existing printer shows final model text on stdout;
		// the owner delivery itself is also recorded through the real tool.
		streamText(w, []string{cue + " completed"})
		return
	}
	streamOwner(w, req, cue+" completed")
}

func cliProfileIsolation(t *testing.T) {
	t.Helper()
	for _, key := range []string{"XBOT_VAULT_SOCKET", "XBOT_SMOKE_OAUTH", "XBOT_DEV_STATIC", "XBOT_ROOT", "XBOT_ENDPOINT", "XBOT_BIN", "XBOT_BOT", "XBOT_RUNNER_URL", "XBOT_RUNNER_TOKEN", "XBOT_CODE_AGENT"} {
		t.Setenv(key, "")
	}
}

func cliProfileRun(t *testing.T, env []string, args ...string) (string, string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, xbotBinary, args...)
	cmd.Env = env
	var out, diagnostic bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &diagnostic
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("CLI did not return: %v %v", args, ctx.Err())
	}
	return out.String(), diagnostic.String(), err
}

func cliProfileMustRun(t *testing.T, env []string, args ...string) string {
	t.Helper()
	out, diagnostic, err := cliProfileRun(t, env, args...)
	if err != nil {
		t.Fatalf("CLI %v: %v\nstdout=%s\nstderr=%s", args, err, out, diagnostic)
	}
	return out
}

func cliProfileDisk(t *testing.T, root, name string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "bots", name, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func cliProfileWant(t *testing.T, got map[string]any, name, description, timezone string) {
	t.Helper()
	if got["name"] != name || got["description"] != description || got["timezone"] != timezone {
		t.Fatalf("profile=%v; want name=%q description=%q timezone=%q", got, name, description, timezone)
	}
}

func cliProfileJSON(t *testing.T, result string) map[string]any {
	t.Helper()
	if !strings.HasPrefix(result, "exit 0\n") {
		t.Fatalf("CLI shell failed or did not finish inside the active turn: %s", result)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(commandOutput(result)), &got); err != nil {
		t.Fatalf("--json stdout is not one JSON object: %v\n%s", err, result)
	}
	if got["scope"] != "runtime" {
		t.Fatalf("runtime call reported wrong scope: %v", got)
	}
	return got
}

func cliProfileResult(t *testing.T, m *cliProfileModel, cue string, index int) string {
	t.Helper()
	got := m.results(cue)[fmt.Sprintf("%s_%d", cue, index)]
	if got == "" {
		t.Fatalf("missing real Shell result %s/%d", cue, index)
	}
	return got
}

func cliProfileWaitHeld(t *testing.T, m *cliProfileModel, cue string) {
	t.Helper()
	select {
	case <-m.cases[cue].entered:
	case <-time.After(15 * time.Second):
		t.Fatalf("CLI never reached the next model request while the turn was active: %s results=%v", cue, m.results(cue))
	}
}

func TestCLIProfileServeIsolation(t *testing.T) {
	cliProfileIsolation(t)
	model := newCLIProfileModel(t)
	first := Start(t, &Stub{srv: model.server})
	second := Start(t, &Stub{srv: model.server})
	for _, srv := range []*Server{first, second} {
		srv.CreateBot("Sentinel", "The primary Bot must not be mistaken for --self.")
		srv.CreateBot("Worker", "Only follow the independent CLI acceptance script.")
		cliProfileMustRun(t, srv.env, "--root", srv.Root, "primary", "set", "Sentinel")
		srv.PatchBot("Sentinel", map[string]any{"description": "untouched primary", "timezone": "UTC"})
		srv.PatchBot("Worker", map[string]any{"description": "before", "timezone": "UTC"})
	}
	const show = `"$XBOT_BIN" profile show --self --json`
	canonicalBinary, err := filepath.EvalSymlinks(xbotBinary)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		cue, description, otherRoot string
	}{
		{"CLI_SELF_FIRST", "first instance saved", second.Root},
		{"CLI_SELF_SECOND", "second instance saved", first.Root},
	} {
		model.add(item.cue, 7,
			`test "$XBOT_BIN" = `+shellQuote(canonicalBinary)+` && `+show,
			`"$XBOT_BIN" profile set --self --description `+shellQuote(item.description)+` --timezone Asia/Tokyo --json`,
			show,
			`"$XBOT_BIN" profile set --self --description rejected-partial-change --timezone Invalid/Nowhere --json`,
			`"$XBOT_BIN" --root `+shellQuote(item.otherRoot)+` profile set --self --description wrong-instance --json`,
			`XBOT_BOT=Sentinel `+show,
			`printf '%s\n' "$XBOT_ENDPOINT"`,
			show,
		)
	}
	defer model.release("CLI_SELF_FIRST")
	defer model.release("CLI_SELF_SECOND")
	a := first.BeginSend("Worker", "CLI_SELF_FIRST")
	b := second.BeginSend("Worker", "CLI_SELF_SECOND")
	for _, item := range []struct {
		cue, description string
		srv              *Server
	}{
		{"CLI_SELF_FIRST", "first instance saved", first},
		{"CLI_SELF_SECOND", "second instance saved", second},
	} {
		cliProfileWaitHeld(t, model, item.cue)
		cliProfileWant(t, cliProfileJSON(t, cliProfileResult(t, model, item.cue, 0)), "Worker", "before", "UTC")
		updated := cliProfileJSON(t, cliProfileResult(t, model, item.cue, 1))
		if updated["applied"] != true {
			t.Fatalf("set did not acknowledge application: %v", updated)
		}
		cliProfileWant(t, updated, "Worker", item.description, "Asia/Tokyo")
		for _, index := range []int{2, 5} {
			cliProfileWant(t, cliProfileJSON(t, cliProfileResult(t, model, item.cue, index)), "Worker", item.description, "Asia/Tokyo")
		}
		for _, index := range []int{3, 4} {
			result := cliProfileResult(t, model, item.cue, index)
			if strings.HasPrefix(result, "exit 0") || !strings.Contains(result, "exit ") || strings.TrimSpace(commandOutput(result)) == "" {
				t.Fatalf("invalid timezone/root did not fail explicitly: %s", result)
			}
		}
		cliProfileWant(t, cliProfileDisk(t, item.srv.Root, "Worker"), "Worker", item.description, "Asia/Tokyo")
		cliProfileWant(t, cliProfileDisk(t, item.srv.Root, "Sentinel"), "Sentinel", "untouched primary", "UTC")
		if state := item.srv.State("Worker"); state != "running" {
			t.Fatalf("CLI return was not observed in an active turn: state=%s", state)
		}
		// Web owns a separately loaded Bot. The runtime's following show must
		// read this update rather than its earlier in-memory configuration.
		item.srv.PatchBot("Worker", map[string]any{"description": "web update " + item.cue, "timezone": "Europe/London"})
		model.release(item.cue)
	}
	a.Wait()
	b.Wait()
	for _, cue := range []string{"CLI_SELF_FIRST", "CLI_SELF_SECOND"} {
		cliProfileWant(t, cliProfileJSON(t, cliProfileResult(t, model, cue, 7)), "Worker", "web update "+cue, "Europe/London")
	}
	endpoint := strings.TrimSpace(commandOutput(cliProfileResult(t, model, "CLI_SELF_FIRST", 6)))
	if endpoint == "" {
		t.Fatal("Shell did not receive the current operation endpoint")
	}
	first.Stop()
	out, diagnostic, err := cliProfileRun(t, append(first.env, "XBOT_ENDPOINT="+endpoint, "XBOT_ROOT="+first.Root), "profile", "set", "--self", "--description", "must-not-write-offline", "--json")
	if err == nil || strings.TrimSpace(diagnostic) == "" || strings.TrimSpace(out) != "" {
		t.Fatalf("closed runtime silently succeeded: err=%v stdout=%q stderr=%q", err, out, diagnostic)
	}
	cliProfileWant(t, cliProfileDisk(t, first.Root, "Worker"), "Worker", "web update CLI_SELF_FIRST", "Europe/London")
}

func TestCLIProfileStandaloneChat(t *testing.T) {
	cliProfileIsolation(t)
	model := newCLIProfileModel(t)
	const cue = "CLI_CHAT_SELF"
	model.add(cue, 3,
		`"$XBOT_BIN" profile show --self --json`,
		`"$XBOT_BIN" profile set --self --description 'standalone chat saved' --timezone America/New_York --json`,
		`"$XBOT_BIN" profile show --self --json`,
		`printf '%s\n' "$XBOT_ENDPOINT"`,
	)
	defer model.release(cue)
	home := t.TempDir()
	root := filepath.Join(home, "data")
	env := append(os.Environ(), "HOME="+home, "XBOT_BASE_URL="+model.server.URL+"/v1", "XBOT_MODEL=smoke", "XBOT_API_KEY=smoke", "XBOT_AUTO_UPGRADE=0")
	cliProfileMustRun(t, env, "--root", root, "new", "Sentinel")
	cliProfileMustRun(t, env, "--root", root, "new", "Worker")
	cliProfileMustRun(t, env, "--root", root, "primary", "set", "Sentinel")
	cliProfileMustRun(t, env, "--root", root, "profile", "set", "Worker", "--description", "before", "--timezone", "UTC")
	// Reuse only the terminal launcher. No serve process has ever used this
	// private root, and no HTTP product server is started by this test.
	launcher := &Server{t: t, Root: root, env: env}
	chat := launcher.Chat("Worker")
	chat.Say(cue)
	cliProfileWaitHeld(t, model, cue)
	cliProfileWant(t, cliProfileJSON(t, cliProfileResult(t, model, cue, 0)), "Worker", "before", "UTC")
	for _, index := range []int{1, 2} {
		cliProfileWant(t, cliProfileJSON(t, cliProfileResult(t, model, cue, index)), "Worker", "standalone chat saved", "America/New_York")
	}
	cliProfileWant(t, cliProfileDisk(t, root, "Worker"), "Worker", "standalone chat saved", "America/New_York")
	model.release(cue)
	WaitFor(t, 10*time.Second, "standalone chat's owner-visible result", func() (bool, string) {
		return strings.Contains(chat.Stdout(), cue+" completed"), chat.Stdout() + "\n" + chat.Stderr()
	})
	chat.Quit()
	endpoint := strings.TrimSpace(commandOutput(cliProfileResult(t, model, cue, 3)))
	out, diagnostic, err := cliProfileRun(t, append(env, "XBOT_ENDPOINT="+endpoint, "XBOT_ROOT="+root), "profile", "show", "--self", "--json")
	if err == nil || diagnostic == "" || strings.TrimSpace(out) != "" {
		t.Fatalf("closed chat endpoint remained usable: err=%v stdout=%q stderr=%q", err, out, diagnostic)
	}
}

func TestCLIProfileOffline(t *testing.T) {
	cliProfileIsolation(t)
	home := t.TempDir()
	env := append(os.Environ(), "HOME="+home)
	for _, args := range [][]string{{"--help"}, {"profile", "--help"}, {"profile", "show", "--help"}, {"profile", "set", "--help"}} {
		out, diagnostic, err := cliProfileRun(t, env, args...)
		if err != nil || strings.TrimSpace(out+diagnostic) == "" {
			t.Fatalf("offline help failed: %v err=%v stdout=%q stderr=%q", args, err, out, diagnostic)
		}
	}
	for _, args := range [][]string{{"profile", "show", "--self", "--json"}, {"profile", "set", "--self", "--description", "not saved", "--json"}} {
		out, diagnostic, err := cliProfileRun(t, env, args...)
		if err == nil || strings.TrimSpace(diagnostic) == "" || strings.TrimSpace(out) != "" {
			t.Fatalf("missing runtime/identity did not fail clearly: %v err=%v stdout=%q stderr=%q", args, err, out, diagnostic)
		}
	}
}

func TestCLIProfileBrowser(t *testing.T) {
	cliProfileIsolation(t)
	chrome := browsertest.Chrome(t)
	model := newCLIProfileModel(t)
	model.add("CLI_BROWSER_LONG", 2,
		`"$XBOT_BIN" profile set --self --description 'browser live saved' --timezone Asia/Tokyo --json`,
		`"$XBOT_BIN" profile show --self --json`,
	)
	model.add("CLI_BROWSER_SHORT", -1,
		`"$XBOT_BIN" profile set --self --description 'browser short saved' --timezone UTC --json`,
	)
	defer model.release("CLI_BROWSER_LONG")
	srv := Start(t, &Stub{srv: model.server})
	srv.CreateBot("Worker", "Independent CLI profile browser acceptance.")
	srv.PatchBot("Worker", map[string]any{"description": "before browser", "timezone": "UTC"})
	u, _ := url.Parse(srv.URL)
	evidence := os.Getenv("XBOT_CLI_PROFILE_EVIDENCE")
	if evidence == "" {
		evidence = t.TempDir()
	}
	cfg, _ := json.Marshal(map[string]any{"chrome": chrome, "url": srv.URL, "control": model.server.URL, "cookies": srv.client.Jar.Cookies(u), "evidence": evidence})
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "testdata/cli-profile.cjs")
	cmd.Stdin = bytes.NewReader(cfg)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("real CLI profile browser: %v\n%s", err, out)
	}
	t.Log(strings.TrimSpace(string(out)))
	cliProfileWant(t, cliProfileDisk(t, srv.Root, "Worker"), "Worker", "browser short saved", "UTC")
	for _, item := range []struct{ cue, description, timezone string }{
		{"CLI_BROWSER_LONG", "browser live saved", "Asia/Tokyo"},
		{"CLI_BROWSER_SHORT", "browser short saved", "UTC"},
	} {
		cliProfileWant(t, cliProfileJSON(t, cliProfileResult(t, model, item.cue, 0)), "Worker", item.description, item.timezone)
	}
}
