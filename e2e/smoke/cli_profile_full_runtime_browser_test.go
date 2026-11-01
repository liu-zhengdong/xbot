//go:build smoke

package smoke

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"xbot/internal/browsertest"
)

// Only fixture controls and the upstream model are scripted. The browser uses
// shipped UI, real messages SSE, actual Shell CLI and a real Local child.
type cliFullBrowserGate struct {
	entered, release chan struct{}
	enterOnce, once  sync.Once
}

func (g *cliFullBrowserGate) open() { g.once.Do(func() { close(g.release) }) }

func (g *cliFullBrowserGate) hold(ctx context.Context) {
	g.enterOnce.Do(func() { close(g.entered) })
	select {
	case <-g.release:
	case <-ctx.Done():
	}
}

// Keep added HTTP failure diagnostics independent of the shared harness's raw
// serve stderr, which includes the login token.
func cliFullBrowserAPI(t *testing.T, srv *Server, method, route string, value any) []byte {
	t.Helper()
	var body io.Reader
	if value != nil {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(raw)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, srv.URL+route, body)
	if err != nil {
		t.Fatal(err)
	}
	if value != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := srv.client.Do(req)
	if err != nil {
		t.Fatalf("fixture API %s %s: %v", method, route, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("fixture API %s %s: HTTP %d, read error=%v", method, route, resp.StatusCode, err)
	}
	return raw
}

func TestCLIProfileFullRuntimeBrowser(t *testing.T) {
	cliProfileIsolation(t)
	chrome := browsertest.Chrome(t)
	model := newCLIProfileModel(t)
	const currentCue = "CLI_FULL_CURRENT"
	const lateCue = "CLI_FULL_LATE"
	gates := map[string]*cliFullBrowserGate{}
	for _, key := range []string{currentCue + "_pre", currentCue + "_tail", lateCue + "_pre", lateCue + "_tail"} {
		gates[key] = &cliFullBrowserGate{entered: make(chan struct{}), release: make(chan struct{})}
	}
	releaseAll := func() {
		for _, gate := range gates {
			gate.open()
		}
		model.release(currentCue)
		model.release(lateCue)
	}
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gate := gates[r.URL.Query().Get("gate")]
		if gate == nil {
			http.NotFound(w, r)
			return
		}
		switch r.URL.Path {
		case "/hold":
			gate.hold(r.Context())
		case "/release":
			if r.Method != http.MethodPost {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			gate.open()
		case "/status":
		default:
			http.NotFound(w, r)
			return
		}
		held := false
		select {
		case <-gate.entered:
			held = true
		default:
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"held": held})
	}))
	t.Cleanup(control.Close)
	// The pre-gate is released immediately after the browser types its draft;
	// the following model gate keeps the turn held without detaching Shell.
	for _, tc := range []struct{ cue, next string }{{currentCue, "NewName"}, {lateCue, "LateNew"}} {
		model.add(tc.cue, 1,
			"curl -fsS --max-time 8 "+shellQuote(control.URL+"/hold?gate="+tc.cue+"_pre")+` >/dev/null && "$XBOT_BIN" profile set --self --name `+tc.next+` --description 'renamed through real Shell' --json`,
		)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "fixture request read failed", http.StatusBadRequest)
			return
		}
		var req stubRequest
		if json.Unmarshal(raw, &req) != nil {
			http.Error(w, "fixture request JSON failed", http.StatusBadRequest)
			return
		}
		caller, _ := classify(req)
		if caller == callerSubagent {
			text := "CLI_FULL_CHILD_READY"
			if strings.Contains(joined(req), "CLI_FULL_CHILD_FOLLOWUP") {
				text = "CLI_FULL_CHILD_FOLLOWED"
			}
			streamText(w, []string{text})
			return
		}
		if strings.Contains(lastUser(req), `source="subagent"`) {
			streamOwner(w, req, "Local child result received.")
			return
		}
		if strings.Contains(lastUser(req), "CLI_FULL_SETUP") {
			if !sawTool(req, "dispatch") {
				streamTool(w, "cli_full_dispatch", "dispatch", argsJSON(map[string]string{
					"type": "dev", "task": "CLI_FULL_CHILD_TASK", "context": "fresh",
				}))
				return
			}
			streamOwner(w, req, "CLI_FULL_HISTORY\n\n"+strings.Repeat("Local history paragraph for a real scroll anchor.\n\n", 65))
			return
		}
		// Hold the final model response after actual owner delivery, allowing
		// assertions on rendered text while the original SSE is still pending.
		for _, cue := range []string{currentCue, lateCue} {
			if strings.Contains(lastUser(req), cue) && sawTool(req, "send_message_to_user") {
				gates[cue+"_tail"].hold(r.Context())
			}
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		model.handle(w, r)
	}))
	t.Cleanup(upstream.Close)
	srv := Start(t, &Stub{srv: upstream})
	t.Cleanup(releaseAll)
	defer releaseAll()
	for _, name := range []string{"Worker", "Late", "Other"} {
		cliFullBrowserAPI(t, srv, http.MethodPost, "/api/bots", map[string]string{
			"name": name, "persona": "Local scripted browser acceptance. BOTNAME=" + name,
		})
	}
	setup := cliFullBrowserAPI(t, srv, http.MethodPost, "/api/bots/Worker/messages", map[string]string{"text": "CLI_FULL_SETUP"})
	if !bytes.Contains(setup, []byte("CLI_FULL_HISTORY")) || !bytes.Contains(setup, []byte("event: done")) {
		t.Fatal("real setup turn did not deliver its scroll history and complete")
	}
	var childID string
	WaitFor(t, 12*time.Second, "real Local child completion and idle parent", func() (bool, string) {
		var profile struct {
			Agents []Agent `json:"agents"`
		}
		if err := json.Unmarshal(cliFullBrowserAPI(t, srv, http.MethodGet, "/api/bots/Worker", nil), &profile); err != nil {
			t.Fatal(err)
		}
		var status struct {
			Bots []struct {
				Bot   string `json:"bot"`
				State string `json:"state"`
			} `json:"bots"`
		}
		if err := json.Unmarshal(cliFullBrowserAPI(t, srv, http.MethodGet, "/api/bots/status", nil), &status); err != nil {
			t.Fatal(err)
		}
		idle := false
		for _, bot := range status.Bots {
			idle = idle || bot.Bot == "Worker" && bot.State == "idle"
		}
		if len(profile.Agents) == 1 && profile.Agents[0].Status == "done" && idle {
			childID = profile.Agents[0].ID
			return true, ""
		}
		return false, "waiting for the actual Local child and parent settlement"
	})
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	evidence := os.Getenv("XBOT_CLI_PROFILE_FULL_EVIDENCE")
	if evidence == "" {
		evidence = t.TempDir()
	}
	cfg, err := json.Marshal(map[string]any{
		"chrome": chrome, "url": srv.URL, "cookies": srv.client.Jar.Cookies(u),
		"model": model.server.URL, "control": control.URL, "child": childID, "evidence": evidence,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 65*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "testdata/cli-profile-full-runtime.cjs")
	cmd.Stdin = bytes.NewReader(cfg)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("full profile real browser: %v\n%s", err, out)
	}
	t.Log(strings.TrimSpace(string(out)))
	for _, tc := range []struct{ cue, previous, name string }{{currentCue, "Worker", "NewName"}, {lateCue, "Late", "LateNew"}} {
		result := cliProfileJSON(t, cliProfileResult(t, model, tc.cue, 0))
		if result["name"] != tc.name || result["previous_name"] != tc.previous || result["moved"] != true || result["description"] != "renamed through real Shell" {
			t.Fatalf("real Shell rename receipt: name=%v previous_name=%v moved=%v description=%v", result["name"], result["previous_name"], result["moved"], result["description"])
		}
	}
}
