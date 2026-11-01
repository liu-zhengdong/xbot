//go:build smoke

package smoke

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func mcpRuntimeAPI(t *testing.T, srv *Server, method, path string, body any, want int) []byte {
	t.Helper()
	status, raw := srv.do(method, path, body)
	if status != want {
		t.Fatalf("%s %s: HTTP %d, want %d: %s", method, path, status, want, raw)
	}
	return raw
}

func mcpRuntimeTurnDone(t *testing.T, srv *Server, bot, cue string) {
	t.Helper()
	WaitFor(t, 12*time.Second, cue+" real turn completion", func() (bool, string) {
		for _, row := range srv.Transcript(bot) {
			if row.Role == "assistant" && len(row.ToolCalls) == 0 && row.Content == cue+"_FINISHED" {
				return true, ""
			}
		}
		diagnostic := "turn has not written its final transcript row"
		for _, line := range strings.Split(srv.Stderr(), "\n") {
			if strings.HasPrefix(line, "xbot:") {
				diagnostic = line
			}
		}
		return false, diagnostic
	})
}

type mcpRuntimeValue struct {
	Session string `json:"session"`
	Count   int    `json:"count"`
	Label   string `json:"label"`
}

var mcpRuntimeResultRE = regexp.MustCompile(`RUNTIME_RESULT (\{[^\n]*\})`)

func mcpRuntimeResult(t *testing.T, model *mcpRuntimeModel, cue string) mcpRuntimeValue {
	t.Helper()
	for _, text := range model.results(cue) {
		match := mcpRuntimeResultRE.FindStringSubmatch(text)
		if len(match) == 2 {
			var out mcpRuntimeValue
			if err := json.Unmarshal([]byte(match[1]), &out); err != nil {
				t.Fatal(err)
			}
			return out
		}
	}
	t.Fatalf("real MCP result absent for %s: %v", cue, model.results(cue))
	return mcpRuntimeValue{}
}

// Records are observed on disk, not injected through an internal Inbox or
// synthesized by the model. This also checks the original event's trust/source.
func mcpRuntimeInbox(t *testing.T, srv *Server, bot string) []map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(srv.Root, "bots", bot, "inbox.jsonl"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var record struct {
			Op    string         `json:"op"`
			Event map[string]any `json:"event"`
		}
		if json.Unmarshal([]byte(line), &record) == nil && record.Op == "post" {
			out = append(out, record.Event)
		}
	}
	return out
}

// The narrow 2b red starts with the one already specified runtime CLI entry.
// It deliberately does not run the accepted 2a adapter cases or invent the
// forthcoming discovery/result DTO shape. The full add-to-call path will use
// the finalized CLI contract when that design has landed.
func TestMCPRuntimeCLIEntry(t *testing.T) {
	cliProfileIsolation(t)
	model := newMCPRuntimeModel(t)
	const cue = "MCP_RUNTIME_CLI_ENTRY"
	model.add(cue, mcpRuntimeTool("sh", map[string]string{
		"description": "List current Bot MCP configuration through the running binary",
		"command":     `"$XBOT_BIN" mcp list --self --json`,
	}))
	srv := Start(t, &Stub{srv: model.server})
	srv.CreateBot("Worker", "Independent MCP runtime CLI entry acceptance.")
	srv.Send("Worker", cue)
	result := model.results(cue)[0]
	if !strings.HasPrefix(result, "exit 0\n") {
		t.Fatalf("runtime MCP CLI did not complete successfully in its real source Shell: %s", result)
	}
	if !json.Valid([]byte(commandOutput(result))) {
		t.Fatalf("MCP --json stdout is not one JSON document: %s", result)
	}
}

func TestMCPRuntimeOwner(t *testing.T) {
	cliProfileIsolation(t)
	peer := newMCPRuntimePeer(t)
	model := newMCPRuntimeModel(t)
	srv := Start(t, &Stub{srv: model.server})
	srv.CreateBot("Worker", "Independent MCP runtime acceptance.")
	srv.CreateBot("Other", "Independent MCP session isolation.")
	var bots struct {
		Bots []struct {
			Name string `json:"name"`
		} `json:"bots"`
	}
	if err := json.Unmarshal(mcpRuntimeAPI(t, srv, http.MethodGet, "/api/bots", nil, 200), &bots); err != nil {
		t.Fatal(err)
	}
	if peer.sessionCount() != 0 {
		t.Fatal("an MCP session existed before any MCP was configured")
	}
	// Start a local receiver and deliver a notification before the first web
	// message or tools/describe call. Merely loading tools must not hide a
	// missing hot-add notification reconcile. The later global spec is equal.
	model.add("MCP_RUNTIME_HOT_ADD")
	mcpRuntimeAPI(t, srv, http.MethodPost, "/api/bots/Worker/mcp", map[string]string{"name": "runtime-fixture", "url": peer.server.URL + "/original"}, 200)
	WaitFor(t, 8*time.Second, "hot-added MCP receiver without a probe", func() (bool, string) { return peer.sessionCount() > 0, "no initialize yet" })
	hotSession := peer.firstSession()
	peer.note(t, hotSession, "MCP_RUNTIME_HOT_ADD")
	mcpRuntimeTurnDone(t, srv, "Worker", "MCP_RUNTIME_HOT_ADD")
	mcpRuntimeAPI(t, srv, http.MethodPost, "/api/apps/custom", map[string]string{"name": "runtime-fixture", "url": peer.server.URL + "/original"}, 200)
	WaitFor(t, 8*time.Second, "new MCP notification receivers", func() (bool, string) {
		return peer.sessionCount() >= len(bots.Bots), fmt.Sprint(peer.sessionCount())
	})
	model.add("MCP_RUNTIME_WEB_ONE", mcpRuntimeCall("counter", "web-one")...)
	srv.Send("Worker", "MCP_RUNTIME_WEB_ONE")
	first := mcpRuntimeResult(t, model, "MCP_RUNTIME_WEB_ONE")
	if first.Count != 1 || first.Session != hotSession {
		t.Fatalf("first counter did not borrow the hot-added notification session: %+v", first)
	}
	if got := peer.sessionCount(); got != len(bots.Bots) {
		t.Fatalf("same Bot opened additional MCP owners for background and web: initializes=%d configured Bots=%d", got, len(bots.Bots))
	}
	model.add("MCP_RUNTIME_NOTIFY", mcpRuntimeCall("counter", "notification-pass")...)
	peer.note(t, first.Session, "MCP_RUNTIME_NOTIFY")
	mcpRuntimeTurnDone(t, srv, "Worker", "MCP_RUNTIME_NOTIFY")
	notified := mcpRuntimeResult(t, model, "MCP_RUNTIME_NOTIFY")
	if notified.Session != first.Session || notified.Count != 2 {
		t.Fatalf("notification turn did not share the calling session: first=%+v notification=%+v", first, notified)
	}
	var noteCount int
	for _, event := range mcpRuntimeInbox(t, srv, "Worker") {
		if event["source"] == "mcp:runtime-fixture" && strings.Contains(fmt.Sprint(event["content"]), "MCP_RUNTIME_NOTIFY") {
			noteCount++
			if event["untrusted"] != true {
				t.Fatalf("MCP notification was stored trusted: %v", event)
			}
		}
	}
	if noteCount != 1 {
		t.Fatalf("notification must use one original Inbox event, got %d", noteCount)
	}
	model.mu.Lock()
	noteRequest := model.scripts["MCP_RUNTIME_NOTIFY"].request
	model.mu.Unlock()
	if !strings.Contains(noteRequest, "<untrusted_event") {
		t.Fatalf("MCP notification lost untrusted projection: %s", noteRequest)
	}
	model.add("MCP_RUNTIME_WEB_TWO", mcpRuntimeCall("counter", "web-two")...)
	srv.Send("Worker", "MCP_RUNTIME_WEB_TWO")
	second := mcpRuntimeResult(t, model, "MCP_RUNTIME_WEB_TWO")
	if second.Session != first.Session || second.Count != 3 {
		t.Fatalf("web turn session/counter reset: first=%+v second=%+v", first, second)
	}
	model.add("MCP_RUNTIME_OTHER", mcpRuntimeCall("counter", "other-bot")...)
	srv.Send("Other", "MCP_RUNTIME_OTHER")
	other := mcpRuntimeResult(t, model, "MCP_RUNTIME_OTHER")
	if other.Session == first.Session || other.Count != 1 {
		t.Fatalf("Bots sharing one global spec shared session state: Worker=%+v Other=%+v", first, other)
	}
	if got := peer.sessionCount(); got != len(bots.Bots) {
		t.Fatalf("cross-turn initialize count=%d; want one per configured Bot (%d)", got, len(bots.Bots))
	}
	t.Logf("one initialize per Bot; Worker web/notification/web counter 1/2/3 in %s; Other counter 1 in independent %s", first.Session, other.Session)
}

func mcpRuntimeWait(t *testing.T, signal <-chan struct{}, timeout time.Duration, reason string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(timeout):
		t.Fatal(reason)
	}
}

func mcpRuntimeHook(t *testing.T, srv *Server) func(string) {
	t.Helper()
	var hook struct {
		URL    string `json:"local_url"`
		Secret string `json:"secret"`
	}
	raw := mcpRuntimeAPI(t, srv, http.MethodPost, "/api/bots/Worker/webhooks", map[string]string{"name": "mcp-runtime", "auth": "bearer"}, 201)
	if json.Unmarshal(raw, &hook) != nil || hook.URL == "" || hook.Secret == "" {
		t.Fatal("private webhook setup failed")
	}
	return func(cue string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, hook.URL, strings.NewReader(cue))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+hook.Secret)
		req.Header.Set("X-XBot-Delivery", cue)
		res, err := srv.client.Do(req)
		if err != nil {
			t.Fatal("private webhook delivery failed")
		}
		res.Body.Close()
		if res.StatusCode != 202 {
			t.Fatalf("private webhook: HTTP %d", res.StatusCode)
		}
	}
}

func mcpRuntimeJobID(t *testing.T, model *mcpRuntimeModel, cue string) string {
	t.Helper()
	for _, result := range model.results(cue) {
		if match := regexp.MustCompile(`\bbackground (sh_[a-f0-9]+)`).FindStringSubmatch(result); len(match) == 2 {
			return match[1]
		}
	}
	t.Fatalf("no original background receipt for %s: %v", cue, model.results(cue))
	return ""
}

func mcpRuntimeJob(t *testing.T, srv *Server, id string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(srv.Root, "bots", "Worker", "shell-jobs", id+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var job map[string]any
	if err := json.Unmarshal(raw, &job); err != nil {
		t.Fatal(err)
	}
	return job
}

func mcpRuntimeJobEnded(t *testing.T, srv *Server, id string, timeout time.Duration) map[string]any {
	t.Helper()
	var job map[string]any
	WaitFor(t, timeout, "original job completion "+id, func() (bool, string) {
		job = mcpRuntimeJob(t, srv, id)
		return job["status"] != "running", fmt.Sprint(job["status"])
	})
	return job
}

func mcpRuntimeOneJobEvent(t *testing.T, srv *Server, id string) {
	t.Helper()
	WaitFor(t, 8*time.Second, "original completion Inbox event "+id, func() (bool, string) {
		count := 0
		for _, event := range mcpRuntimeInbox(t, srv, "Worker") {
			if event["type"] == "shell_job" && event["sender"] == id {
				count++
				if event["source"] != "shell" || event["id"] != "shelljob_"+id || event["untrusted"] != true {
					t.Fatalf("job did not use original untrusted shell event: %v", event)
				}
			}
		}
		if count > 1 {
			t.Fatalf("duplicate completion events for original job %s: %d", id, count)
		}
		return count == 1, fmt.Sprintf("events=%d", count)
	})
}

func mcpRuntimeInspect(t *testing.T, srv *Server, model *mcpRuntimeModel, cue, id string, cancel bool) []string {
	t.Helper()
	steps := []mcpRuntimeStep{mcpRuntimeTool("shell_status", map[string]string{"id": id}), mcpRuntimeTool("shell_output", map[string]string{"id": id})}
	if cancel {
		steps = append(steps, mcpRuntimeTool("shell_cancel", map[string]string{"id": id}))
	}
	model.add(cue, steps...)
	srv.Send("Worker", cue)
	return model.results(cue)
}

// No cached web Kernel exists when these three webhook turns run. The second
// and third deliveries prove the preceding short Pass has actually returned.
func TestMCPRuntimeCallsAndSourceLease(t *testing.T) {
	cliProfileIsolation(t)
	peer := newMCPRuntimePeer(t)
	model := newMCPRuntimeModel(t)
	srv := Start(t, &Stub{srv: model.server})
	srv.CreateBot("Worker", "Independent MCP slow call and source lease acceptance.")
	post := mcpRuntimeHook(t, srv)
	mcpRuntimeAPI(t, srv, http.MethodPost, "/api/bots/Worker/mcp", map[string]string{"name": "runtime-fixture", "url": peer.server.URL + "/calls"}, 200)
	// One real HTTP call crosses the former 15s transport cap. Release and
	// minimum are separate gates; all useful parent/lease checks overlap it.
	slow := peer.gate("over-fifteen", 15500*time.Millisecond)
	defer slow.allow()
	model.add("MCP_SLOW_PARENT", mcpRuntimeCall("held", "over-fifteen")...)
	post("MCP_SLOW_PARENT")
	mcpRuntimeWait(t, slow.entered, 12*time.Second, "source Pass never started the real MCP call")
	mcpRuntimeTurnDone(t, srv, "Worker", "MCP_SLOW_PARENT")
	slowID := mcpRuntimeJobID(t, model, "MCP_SLOW_PARENT")
	if job := mcpRuntimeJob(t, srv, slowID); job["status"] != "running" {
		t.Fatalf("parent completion stopped its MCP call: %v", job)
	}

	endpointCommand := `"$XBOT_BIN" profile show --self --json && printf 'SOURCE_ENDPOINT=%s\n' "$XBOT_ENDPOINT"`
	model.add("MCP_EMPTY_PASS", mcpRuntimeTool("sh", map[string]string{"description": "observe this short source endpoint", "command": endpointCommand}))
	post("MCP_EMPTY_PASS")
	mcpRuntimeTurnDone(t, srv, "Worker", "MCP_EMPTY_PASS")
	endpointRE := regexp.MustCompile(`SOURCE_ENDPOINT=(http://127\.0\.0\.1:[0-9]+)`)
	match := endpointRE.FindStringSubmatch(model.results("MCP_EMPTY_PASS")[0])
	if len(match) != 2 {
		t.Fatalf("source endpoint CLI failed: %v", model.results("MCP_EMPTY_PASS"))
	}
	emptyEndpoint := match[1]

	lease := peer.gate("source-lease", 0)
	defer lease.allow()
	command := `"$XBOT_BIN" profile show --self --json && curl --fail --silent --show-error "` + peer.server.URL + `/gate/source-lease?endpoint=$XBOT_ENDPOINT" && "$XBOT_BIN" profile set --self --description 'source lease saved' --json`
	model.add("MCP_LEASE_PARENT", mcpRuntimeTool("sh", map[string]any{"description": "two real profile CLI calls in one source Shell", "command": command, "background": true}))
	post("MCP_LEASE_PARENT")
	mcpRuntimeWait(t, lease.entered, 12*time.Second, "source Shell did not reach its gate after first real CLI")
	mcpRuntimeTurnDone(t, srv, "Worker", "MCP_LEASE_PARENT")
	leaseID := mcpRuntimeJobID(t, model, "MCP_LEASE_PARENT")
	peer.mu.Lock()
	leaseEndpoint := lease.endpoint
	peer.mu.Unlock()
	if leaseEndpoint == "" || leaseEndpoint == emptyEndpoint {
		t.Fatal("fresh short Passes did not expose distinct source listeners")
	}
	out, diagnostic, err := cliProfileRun(t, envWith(srv.env, "XBOT_ENDPOINT="+emptyEndpoint), "--root", srv.Root, "profile", "show", "--self", "--json")
	if err == nil || out != "" || strings.TrimSpace(diagnostic) == "" {
		t.Fatal("an empty retired source kept its listener because another source owned a shared job")
	}
	// First actual web Kernel: it must see the original live job, not a lost
	// disk record loaded into a second table. Inspection uses the old real tools.
	live := mcpRuntimeInspect(t, srv, model, "MCP_WEB_INSPECT_SLOW", slowID, false)
	if !strings.Contains(live[0], slowID+" running") || !strings.Contains(live[1], "status: running") {
		t.Fatalf("later web Kernel lost original live job: %v", live)
	}
	slow.allow()
	job := mcpRuntimeJobEnded(t, srv, slowID, 22*time.Second)
	if job["status"] != "done" {
		t.Fatalf("real >15s call did not survive parent Pass: %v", job)
	}
	peer.mu.Lock()
	elapsed := time.Since(slow.started)
	peer.mu.Unlock()
	if elapsed < 15*time.Second || peer.callCount("over-fifteen") != 1 {
		t.Fatalf("slow-call proof invalid: elapsed=%s calls=%d", elapsed, peer.callCount("over-fifteen"))
	}
	finished := mcpRuntimeInspect(t, srv, model, "MCP_WEB_OUTPUT_SLOW", slowID, false)
	if !strings.Contains(finished[0], slowID+" done") || !strings.Contains(finished[1], `"label":"over-fifteen"`) {
		t.Fatalf("original job output unavailable from later web Kernel: %v", finished)
	}
	mcpRuntimeOneJobEvent(t, srv, slowID)

	// No MCP call remains to accidentally keep this source alive. Its own
	// actual Shell lease must protect the second CLI until complete cleanup.
	lease.allow()
	job = mcpRuntimeJobEnded(t, srv, leaseID, 8*time.Second)
	if job["status"] != "done" {
		t.Fatalf("source Shell lost its CLI after parent completion: %v", job)
	}
	leaseOut := mcpRuntimeInspect(t, srv, model, "MCP_WEB_OUTPUT_LEASE", leaseID, false)[1]
	var values []map[string]any
	for _, line := range strings.Split(leaseOut, "\n") {
		if strings.HasPrefix(line, "{") {
			var value map[string]any
			if json.Unmarshal([]byte(line), &value) == nil {
				values = append(values, value)
			}
		}
	}
	if len(values) != 2 || values[0]["scope"] != "runtime" || values[1]["scope"] != "runtime" || values[1]["description"] != "source lease saved" {
		t.Fatalf("two CLI calls in the original source Shell were not both successful: %s", leaseOut)
	}
	WaitFor(t, 5*time.Second, "source listener closes after full Shell cleanup", func() (bool, string) {
		_, _, err := cliProfileRun(t, envWith(srv.env, "XBOT_ENDPOINT="+leaseEndpoint), "--root", srv.Root, "profile", "show", "--self", "--json")
		return err != nil, "source endpoint still accepts calls"
	})
	mcpRuntimeOneJobEvent(t, srv, leaseID)

	cancelGate := peer.gate("cross-k-cancel", 0)
	defer cancelGate.allow()
	model.add("MCP_CANCEL_PARENT", mcpRuntimeCall("held", "cross-k-cancel")...)
	post("MCP_CANCEL_PARENT")
	mcpRuntimeWait(t, cancelGate.entered, 10*time.Second, "cancel target was not actually sent to MCP")
	mcpRuntimeTurnDone(t, srv, "Worker", "MCP_CANCEL_PARENT")
	cancelID := mcpRuntimeJobID(t, model, "MCP_CANCEL_PARENT")
	beforeCancel := mcpRuntimeInspect(t, srv, model, "MCP_WEB_CANCEL_ORIGINAL", cancelID, true)
	if !strings.Contains(beforeCancel[0], cancelID+" running") || !strings.Contains(beforeCancel[1], "status: running") {
		t.Fatalf("web controls did not find original cancel target: %v", beforeCancel)
	}
	mcpRuntimeWait(t, cancelGate.canceled, 8*time.Second, "original HTTP call did not observe cross-K cancellation")
	if job := mcpRuntimeJobEnded(t, srv, cancelID, 8*time.Second); job["status"] != "cancelled" {
		t.Fatalf("original job was not cancelled: %v", job)
	}
	if peer.callCount("cross-k-cancel") != 1 {
		t.Fatal("cancelled MCP action was retried")
	}
	mcpRuntimeOneJobEvent(t, srv, cancelID)
	mcpRuntimeOneJobEvent(t, srv, slowID)
	if peer.sessionCount() != 1 {
		t.Fatalf("fresh short Passes/web controls opened more than the one Worker MCP session: %d", peer.sessionCount())
	}
	t.Logf("real MCP call survived %s; original %s controlled from web; original source Shell %s retained then released its own listener; cross-K cancel %s posted one event", elapsed, slowID, leaseID, cancelID)
}

// This is the supported external configuration-edit entry, not a fabricated
// PUT endpoint. Existing API setup creates the files; replace stays in this
// fixture's private data root and is followed by a real inbound serve pass.
func mcpRuntimeReplace(t *testing.T, path string, raw []byte) {
	t.Helper()
	if _, err := os.ReadFile(path); err != nil {
		t.Fatal(err)
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".mcp-runtime-*")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(raw); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(file.Name(), path); err != nil {
		t.Fatal(err)
	}
}

func mcpRuntimeClosed(t *testing.T, peer *mcpRuntimePeer, id string) {
	t.Helper()
	mcpRuntimeWait(t, peer.session(id).closed, 8*time.Second, "old MCP notification transport was not actually closed")
}

func TestMCPRuntimeReconcile(t *testing.T) {
	cliProfileIsolation(t)
	const keyName = "MCP_RUNTIME_TOKEN"
	t.Setenv(keyName, "")
	peer := newMCPRuntimePeer(t)
	model := newMCPRuntimeModel(t)
	srv := Start(t, &Stub{srv: model.server})
	srv.CreateBot("Worker", "Independent MCP configuration and credential reconcile.")
	srv.CreateBot("Other", "Unaffected global MCP session.")
	post := mcpRuntimeHook(t, srv)
	// All values are disposable fake credentials. Only equality booleans are
	// asserted; values are never put in model results, failures or logs.
	const firstValue, nextValue = "independent-credential-v1", "independent-credential-v2"
	setSecret := func(name, value string) {
		t.Helper()
		mcpRuntimeAPI(t, srv, http.MethodPost, "/api/secrets", map[string]any{"name": name, "value": value, "domains": []string{"127.0.0.1"}}, 200)
	}
	setSecret(keyName, firstValue)
	mcpRuntimeAPI(t, srv, http.MethodPost, "/api/apps/custom", map[string]string{"name": "runtime-fixture", "url": peer.server.URL + "/global"}, 200)
	counter := func(cue string, throughPass bool) mcpRuntimeValue {
		t.Helper()
		model.add(cue, mcpRuntimeCall("counter", cue)...)
		if throughPass {
			post(cue)
			mcpRuntimeTurnDone(t, srv, "Worker", cue)
		} else {
			srv.Send("Worker", cue)
		}
		return mcpRuntimeResult(t, model, cue)
	}
	global := counter("MCP_SPEC_GLOBAL", false)
	localSpec := map[string]any{"name": "runtime-fixture", "url": peer.server.URL + "/local", "secret_envs": []string{keyName}}
	mcpRuntimeAPI(t, srv, http.MethodPost, "/api/bots/Worker/mcp", localSpec, 200)
	local := counter("MCP_SPEC_LOCAL", false)
	if local.Session == global.Session || local.Count != 1 {
		t.Fatalf("local override did not replace global session: global=%+v local=%+v", global, local)
	}
	mcpRuntimeClosed(t, peer, global.Session)
	if peer.session(local.Session).credential != firstValue {
		t.Fatal("HTTP session did not use the saved credential snapshot")
	}
	localPath := filepath.Join(srv.Root, "bots", "Worker", "mcp.json")
	globalPath := filepath.Join(srv.Root, "mcp.json")
	localBytes, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatal(err)
	}
	mcpRuntimeReplace(t, localPath, localBytes)
	same := counter("MCP_SPEC_SAME_REWRITE", true)
	if same.Session != local.Session || same.Count != 2 {
		t.Fatalf("same-spec atomic rewrite reconnected: local=%+v after=%+v", local, same)
	}
	globalBytes, _ := json.Marshal(map[string]any{"servers": []any{map[string]string{"name": "runtime-fixture", "url": peer.server.URL + "/global-next"}}})
	mcpRuntimeReplace(t, globalPath, globalBytes)
	shadowed := counter("MCP_SPEC_SHADOWED_GLOBAL", true)
	if shadowed.Session != local.Session || shadowed.Count != 3 {
		t.Fatalf("local override reconnected after hidden global change: %v", shadowed)
	}
	mcpRuntimeReplace(t, localPath, []byte(`{"servers":[{"name":"runtime-fixture","url":"ftp://invalid"}]}`))
	invalid := counter("MCP_SPEC_INVALID_KEEPS_WORKING", true)
	if invalid.Session != local.Session || invalid.Count != 4 {
		t.Fatalf("invalid configuration destroyed the existing valid session: %v", invalid)
	}
	mcpRuntimeReplace(t, localPath, localBytes)

	hot := peer.gate("spec-changed", 0)
	defer hot.allow()
	model.add("MCP_SPEC_HELD_CALL", mcpRuntimeCall("held", "spec-changed")...)
	srv.Send("Worker", "MCP_SPEC_HELD_CALL")
	mcpRuntimeWait(t, hot.entered, 8*time.Second, "hot-change call was not sent")
	hotID := mcpRuntimeJobID(t, model, "MCP_SPEC_HELD_CALL")
	localSpec["url"] = peer.server.URL + "/replacement"
	replacementBytes, _ := json.Marshal(map[string]any{"servers": []any{localSpec}})
	mcpRuntimeReplace(t, localPath, replacementBytes)
	// No drain: old held action must terminate without releasing its gate.
	mcpRuntimeWait(t, hot.canceled, 8*time.Second, "spec replacement waited for old action to drain")
	mcpRuntimeClosed(t, peer, local.Session)
	if job := mcpRuntimeJobEnded(t, srv, hotID, 8*time.Second); job["status"] != "failed" {
		t.Fatalf("configuration-changed job did not fail explicitly: %v", job)
	}
	unknown := strings.ToLower(strings.Join(mcpRuntimeInspect(t, srv, model, "MCP_SPEC_OLD_OUTPUT", hotID, false), "\n"))
	if !strings.Contains(unknown, "configuration_changed") || !strings.Contains(unknown, "outcome_unknown") {
		t.Fatalf("sent action did not report configuration change and unknown side effects: %s", unknown)
	}
	if peer.callCount("spec-changed") != 1 {
		t.Fatal("configuration change retried an action that may already have side effects")
	}
	mcpRuntimeOneJobEvent(t, srv, hotID)
	replaced := counter("MCP_SPEC_REPLACED", false)
	if replaced.Session == local.Session || replaced.Count != 1 {
		t.Fatalf("new effective spec did not start a fresh session: %v", replaced)
	}

	model.add("MCP_KEY_OTHER_BEFORE", mcpRuntimeCall("counter", "other-before-key")...)
	srv.Send("Other", "MCP_KEY_OTHER_BEFORE")
	other := mcpRuntimeResult(t, model, "MCP_KEY_OTHER_BEFORE")
	for _, action := range []struct {
		cue   string
		apply func()
	}{
		{"MCP_KEY_SAME", func() { setSecret(keyName, firstValue) }},
		{"MCP_KEY_UNRELATED", func() { setSecret("MCP_RUNTIME_UNRELATED", "independent-unrelated") }},
		{"MCP_KEY_DOMAINS", func() {
			mcpRuntimeAPI(t, srv, http.MethodPost, "/api/secrets", map[string]any{"name": keyName, "domains": []string{"localhost"}}, 200)
		}},
	} {
		action.apply()
		value := counter(action.cue, false)
		if value.Session != replaced.Session {
			t.Fatalf("same value, unrelated name or domains-only setting reconnected the session: %s", action.cue)
		}
	}
	setSecret(keyName, nextValue)
	rotated := counter("MCP_KEY_ROTATED", false)
	if rotated.Session == replaced.Session || rotated.Count != 1 || peer.session(rotated.Session).credential != nextValue {
		t.Fatal("referenced credential value change did not replace the session with one new frozen input")
	}
	mcpRuntimeClosed(t, peer, replaced.Session)
	mcpRuntimeAPI(t, srv, http.MethodDelete, "/api/secrets/"+keyName, nil, 200)
	mcpRuntimeClosed(t, peer, rotated.Session)
	model.add("MCP_KEY_DELETED", mcpRuntimeCall("counter", "deleted-key")...)
	srv.Send("Worker", "MCP_KEY_DELETED")
	missing := strings.ToLower(strings.Join(model.results("MCP_KEY_DELETED"), "\n"))
	if strings.Contains(missing, "runtime_result") || !(strings.Contains(missing, "secret") || strings.Contains(missing, "credential") || strings.Contains(missing, "密钥") || strings.Contains(missing, strings.ToLower(keyName))) {
		t.Fatal("deleted referenced credential silently kept the old authenticated session or lacked a useful diagnostic")
	}
	if peer.callCount("deleted-key") != 0 {
		t.Fatal("deleted credential still allowed an authenticated MCP action")
	}
	setSecret(keyName, nextValue)
	restored := counter("MCP_KEY_RESTORED", false)
	if restored.Session == rotated.Session || restored.Count != 1 || peer.session(restored.Session).credential != nextValue {
		t.Fatal("restored credential did not recover a new session")
	}
	model.add("MCP_KEY_OTHER_AFTER", mcpRuntimeCall("counter", "other-after-key")...)
	srv.Send("Other", "MCP_KEY_OTHER_AFTER")
	otherAfter := mcpRuntimeResult(t, model, "MCP_KEY_OTHER_AFTER")
	if otherAfter.Session != other.Session || otherAfter.Count != other.Count+1 {
		t.Fatal("credential setting changes restarted a Bot that did not reference that key")
	}
	t.Log("same-spec and shadowed-global edits preserve sessions; invalid config preserves working session; hot replacement closes the old call once without retry; real secret POST/DELETE changes only referencing sessions")
}
