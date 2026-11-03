//go:build smoke

package smoke

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
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

type mcpRuntimeArtifact struct {
	Type     string `json:"type"`
	MIMEType string `json:"mime_type"`
	File     string `json:"file"`
	Access   string `json:"access"`
}
type mcpRuntimeContent struct {
	Type, Text, Preview, URI, File, Access string
	MIMEType                               string `json:"mime_type"`
	ProjectionError                        string `json:"projection_error"`
}
type mcpRuntimeCLIReply struct {
	Data             json.RawMessage     `json:"data"`
	Content          []mcpRuntimeContent `json:"content"`
	Raw              *mcpRuntimeArtifact `json:"raw"`
	Structured       *mcpRuntimeArtifact `json:"structured_content"`
	Artifact         *mcpRuntimeArtifact `json:"artifact"`
	IsError          bool                `json:"is_error"`
	Saved            bool                `json:"saved"`
	ProjectionErrors []string            `json:"projection_errors"`
}

func mcpRuntimeJSON(t *testing.T, raw string) mcpRuntimeCLIReply {
	t.Helper()
	if len(raw) > 64<<10 {
		t.Fatalf("MCP stdout exceeded 64KiB: %d bytes", len(raw))
	}
	var out mcpRuntimeCLIReply
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("MCP stdout must be one complete JSON reply: %v", err)
	}
	return out
}
func mcpRuntimeReply(t *testing.T, result string) mcpRuntimeCLIReply {
	t.Helper()
	if !strings.HasPrefix(result, "exit 0\n") {
		t.Fatalf("runtime CLI Shell did not exit successfully: %s", result)
	}
	return mcpRuntimeJSON(t, commandOutput(result))
}
func mcpRuntimeReadArtifact(t *testing.T, srv *Server, artifact *mcpRuntimeArtifact) []byte {
	t.Helper()
	if artifact == nil || artifact.File == "" || artifact.Access != "local" || filepath.IsAbs(artifact.File) {
		t.Fatalf("missing local workspace artifact: %+v", artifact)
	}
	path := filepath.Join(srv.Root, "work", "Worker", artifact.File)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("MCP artifact permissions: %v", info.Mode())
	}
	return raw
}

var mcpRuntimeResultRE = regexp.MustCompile(`RUNTIME_RESULT (\{[^\n]*\})`)

func mcpRuntimeCounter(t *testing.T, reply mcpRuntimeCLIReply) mcpRuntimeValue {
	t.Helper()
	for _, content := range reply.Content {
		if match := mcpRuntimeResultRE.FindStringSubmatch(content.Text); len(match) == 2 {
			var out mcpRuntimeValue
			if err := json.Unmarshal([]byte(match[1]), &out); err != nil {
				t.Fatal(err)
			}
			return out
		}
	}
	t.Fatal("real remote counter absent from CLI projected content")
	return mcpRuntimeValue{}
}
func mcpRuntimeResult(t *testing.T, model *mcpRuntimeModel, cue string) mcpRuntimeValue {
	t.Helper()
	results := model.results(cue)
	return mcpRuntimeCounter(t, mcpRuntimeReply(t, results[len(results)-1]))
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

// Full 2b path after the independently captured narrow list-entry red.
func TestMCPRuntimeCLIEntry(t *testing.T) {
	cliProfileIsolation(t)
	peer := newMCPRuntimePeer(t)
	model := newMCPRuntimeModel(t)
	const cue = "MCP_RUNTIME_CLI_ENTRY"
	model.add(cue,
		mcpRuntimeCLI("add runtime-fixture --url "+shellQuote(peer.server.URL+"/cli-entry")),
		mcpRuntimeCLI("list"),
		mcpRuntimeCLI("tools runtime-fixture"),
		mcpRuntimeCLI("describe runtime-fixture "+shellQuote(mcpRuntimeRawName)),
		mcpRuntimeCLI("call runtime-fixture "+shellQuote(mcpRuntimeRawName)+" --args "+shellQuote(`{"label":"entry-first"}`)),
		mcpRuntimeShell(`XBOT_BOT=deliberately-stale "$XBOT_BIN" mcp call runtime-fixture `+shellQuote(mcpRuntimeRawName)+` --args '{"label":"entry-second"}' --self --json`),
		mcpRuntimeTool("tools", map[string]any{"action": "call", "id": "list_bots", "args": map[string]any{}}),
	)
	srv := Start(t, &Stub{srv: model.server})
	srv.CreateBot("Worker", "Independent MCP runtime CLI entry acceptance.")
	srv.CreateBot("DoorPeer", "Independent peer for the existing non-MCP Tools call.")
	srv.Send("Worker", cue)
	results := model.results(cue)
	added := mcpRuntimeReply(t, results[0])
	var receipt struct{ Scope, Name, State string }
	if err := json.Unmarshal(added.Data, &receipt); err != nil {
		t.Fatal(err)
	}
	if !added.Saved || receipt.Scope != "global" || receipt.Name != "runtime-fixture" || receipt.State != "configured" {
		t.Fatalf("CLI add did not distinguish saved configuration from readiness: %s", added.Data)
	}
	var list struct {
		Scope   string
		Servers []struct {
			Name, Source string
			Runtime      struct{ State string }
		}
	}
	if err := json.Unmarshal(mcpRuntimeReply(t, results[1]).Data, &list); err != nil {
		t.Fatal(err)
	}
	if list.Scope != "global" || len(list.Servers) != 1 || list.Servers[0].Name != "runtime-fixture" || list.Servers[0].Source != "global" || list.Servers[0].Runtime.State == "" {
		t.Fatalf("runtime CLI list metadata: %+v", list)
	}
	var tools struct {
		Server string
		Tools  []map[string]json.RawMessage
	}
	if err := json.Unmarshal(mcpRuntimeReply(t, results[2]).Data, &tools); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tool := range tools.Tools {
		if string(tool["name"]) == `"`+mcpRuntimeRawName+`"` {
			found = true
		}
	}
	if tools.Server != "runtime-fixture" || !found {
		t.Fatal("CLI tools lost the exact remote name containing slash/dot")
	}
	var description map[string]json.RawMessage
	if err := json.Unmarshal(mcpRuntimeReply(t, results[3]).Data, &description); err != nil {
		t.Fatal(err)
	}
	if string(description["name"]) != `"`+mcpRuntimeRawName+`"` || string(description["x-independent-spec"]) != `"retained"` || !bytes.Contains(description["annotations"], []byte("x-independent-hint")) {
		t.Fatal("describe discarded the exact name or unknown ToolSpec/annotation fields")
	}
	first := mcpRuntimeCounter(t, mcpRuntimeReply(t, results[4]))
	second := mcpRuntimeCounter(t, mcpRuntimeReply(t, results[5]))
	if first.Count != 1 || second.Count != 2 || first.Session != second.Session {
		t.Fatalf("real CLI calls did not borrow one session: %+v %+v", first, second)
	}
	if !strings.Contains(results[6], "DoorPeer") {
		t.Fatalf("non-MCP Tools call did not list the explicit other Bot: %s", results[6])
	}
	mcpRuntimeNoSchemas(t, model, cue, callerForeground)
	t.Log("real source CLI add/list/tools/describe/exact-name call passed; diagnostic Bot name did not route self; non-MCP Tools call remains functional")
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

// The retired shell_output/shell_status/shell_cancel model tools reached the
// same live jobs table the runtime jobs CLI now shares (ADR 0066), so scripted
// inspection goes through the source Shell like the model would.
func mcpRuntimeJobs(t *testing.T, args string) mcpRuntimeStep {
	t.Helper()
	return mcpRuntimeTool("sh", map[string]string{"description": "Inspect the original job through the runtime jobs CLI", "command": `"$XBOT_BIN" jobs ` + args + ` --self`})
}

func mcpRuntimeInspect(t *testing.T, srv *Server, model *mcpRuntimeModel, cue, id string, cancel bool) []string {
	t.Helper()
	steps := []mcpRuntimeStep{mcpRuntimeJobs(t, "status "+id), mcpRuntimeJobs(t, "output "+id)}
	if cancel {
		steps = append(steps, mcpRuntimeJobs(t, "cancel "+id))
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
	// disk record loaded into a second table. Inspection uses the runtime CLI.
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
	if !strings.Contains(finished[0], slowID+" done") {
		t.Fatalf("original job state unavailable from later web Kernel: %v", finished)
	}
	actual := mcpRuntimeCounter(t, mcpRuntimeOutputReply(t, finished[1]))
	if actual.Label != "over-fifteen" || actual.Count != 1 {
		t.Fatalf("original CLI call result missing from later web output: %+v", actual)
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
	if !strings.HasPrefix(beforeCancel[2], "exit 0\n") ||
		!(strings.Contains(beforeCancel[2], "cancellation requested "+cancelID) || strings.Contains(beforeCancel[2], "cancelled "+cancelID)) {
		t.Fatalf("runtime jobs cancel did not stop the original call: %v", beforeCancel)
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
	hotJob := mcpRuntimeJobEnded(t, srv, hotID, 8*time.Second)
	exitCode, _ := hotJob["exit_code"].(float64)
	if (hotJob["status"] != "done" && hotJob["status"] != "failed") || exitCode == 0 {
		t.Fatalf("configuration-changed original Shell job did not finish with a nonzero CLI exit: %v", hotJob)
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

func mcpRuntimeOutputReply(t *testing.T, text string) mcpRuntimeCLIReply {
	t.Helper()
	start := strings.Index(text, "{")
	if start < 0 {
		t.Fatal("original job output lacks the CLI JSON reply")
	}
	var out mcpRuntimeCLIReply
	if err := json.NewDecoder(strings.NewReader(text[start:])).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}
func mcpRuntimeNoSchemas(t *testing.T, model *mcpRuntimeModel, cue, caller string) {
	t.Helper()
	model.mu.Lock()
	defer model.mu.Unlock()
	script := model.scripts[cue]
	if script.mcpSchema {
		t.Fatal("actual provider tool parameters contain remote MCP schema")
	}
	if !script.callers[caller] || !script.declared["sh"] {
		t.Fatalf("actual %s provider request missing for %s", caller, cue)
	}
	for name := range script.declared {
		if name == "mcp" || strings.HasPrefix(name, "mcp_") || strings.HasPrefix(name, "mcp:") || name == mcpRuntimeRawName || name == "counter" || name == "held" || name == "mixed-result" || name == "remote-error" || name == "bad-image" {
			t.Fatalf("actual provider request declared MCP schema %q", name)
		}
	}
}

func TestMCPRuntimeCLIBoundaries(t *testing.T) {
	cliProfileIsolation(t)
	clientHome := t.TempDir()
	ghostRoot := filepath.Join(t.TempDir(), "not-created")
	env := envWith(os.Environ(), "HOME="+clientHome, "XBOT_ROOT="+ghostRoot, "XBOT_ENDPOINT=")
	out, diagnostic, err := mcpRuntimeExternal(t, env, "", "mcp", "--help")
	if err != nil || !strings.Contains(out, "mcp") || diagnostic != "" {
		t.Fatalf("offline help was not successful stdout help: err=%v stderr=%s", err, diagnostic)
	}
	for _, args := range [][]string{{"mcp", "list", "--self", "--json"}, {"mcp", "list", "--json"}} {
		out, _, err = mcpRuntimeExternal(t, env, "", args...)
		if err == nil || json.Valid([]byte(out)) {
			t.Fatal("unavailable or unspecified runtime silently succeeded/fell back to files")
		}
	}
	if _, err := os.Stat(ghostRoot); !os.IsNotExist(err) {
		t.Fatal("online/help created the client root")
	}
	entries, err := os.ReadDir(clientHome)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatal("offline help/online failure initialized local credentials or default config")
	}

	peer := newMCPRuntimePeer(t)
	model := newMCPRuntimeModel(t)
	srv := Start(t, &Stub{srv: model.server})
	srv.CreateBot("Worker", "Independent human MCP CLI and JSON boundary acceptance.")
	mcpRuntimeAPI(t, srv, http.MethodPost, "/api/bots/Worker/mcp", map[string]string{"name": "runtime-fixture", "url": peer.server.URL + "/human"}, 200)
	wrongRoot := t.TempDir()
	humanEnv := envWith(srv.env, "HOME="+clientHome, "XBOT_ROOT="+ghostRoot, "XBOT_ENDPOINT=http://127.0.0.1:1", "XBOT_BOT=not-the-selected-Bot")
	target := []string{"--http", srv.URL, "--bot", "Worker", "--json"}
	args := append([]string{"mcp", "list"}, target...)
	out, diagnostic, err = mcpRuntimeExternal(t, humanEnv, "", args...)
	if err != nil {
		t.Fatalf("explicit human dev instance was blocked by implicit client root/env: %s", diagnostic)
	}
	var list struct{ Scope string }
	if err := json.Unmarshal(mcpRuntimeJSON(t, out).Data, &list); err != nil {
		t.Fatal(err)
	}
	if list.Scope != "global" {
		t.Fatal("human command did not reach selected instance")
	}
	wrongArgs := append([]string{"--root", wrongRoot}, args...)
	out, diagnostic, err = mcpRuntimeExternal(t, humanEnv, "", wrongArgs...)
	if err == nil || out != "" || strings.TrimSpace(diagnostic) == "" {
		t.Fatal("explicit wrong human root was not rejected")
	}
	entries, err = os.ReadDir(wrongRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatal("wrong-root failure wrote local config")
	}
	if _, err := os.Stat(ghostRoot); !os.IsNotExist(err) {
		t.Fatal("human explicit HTTP initialized an implicit root")
	}

	call := append([]string{"mcp", "call", "runtime-fixture", mcpRuntimeRawName}, target...)
	stdin := `{"label":"stdin-number","integer":9007199254740993}`
	out, diagnostic, err = mcpRuntimeExternal(t, humanEnv, stdin, append(append([]string(nil), call...), "--args-file", "-")...)
	if err != nil {
		t.Fatalf("real stdin JSON call failed: %s", diagnostic)
	}
	if mcpRuntimeCounter(t, mcpRuntimeJSON(t, out)).Label != "stdin-number" {
		t.Fatal("stdin arguments did not reach remote tool")
	}
	peer.mu.Lock()
	rawArgs := string(peer.arguments["stdin-number"])
	peer.mu.Unlock()
	if !strings.Contains(rawArgs, "9007199254740993") {
		t.Fatal("CLI/HTTP/core changed the exact integer through float conversion")
	}
	file := filepath.Join(t.TempDir(), "args.json")
	if err := os.WriteFile(file, []byte(`{"label":"file-args"}`), 0600); err != nil {
		t.Fatal(err)
	}
	out, diagnostic, err = mcpRuntimeExternal(t, humanEnv, "", append(append([]string(nil), call...), "--args-file", file)...)
	if err != nil || mcpRuntimeCounter(t, mcpRuntimeJSON(t, out)).Label != "file-args" {
		t.Fatalf("real args-file call failed: %s", diagnostic)
	}
	validRPCs := peer.rpcCount()
	for _, bad := range []struct {
		flags []string
		stdin string
	}{
		{[]string{"--args", "[]"}, ""},
		{[]string{"--args", `{} {}`}, ""},
		{[]string{"--args", "{}", "--args-file", "-"}, "{}"},
		{[]string{"--args-file", "-"}, "null"},
	} {
		out, diagnostic, err = mcpRuntimeExternal(t, humanEnv, bad.stdin, append(append([]string(nil), call...), bad.flags...)...)
		if err == nil || out != "" || strings.TrimSpace(diagnostic) == "" {
			t.Fatal("invalid/ambiguous/non-object JSON did not fail before invocation")
		}
	}
	if peer.rpcCount() != validRPCs || peer.callCount("") != 0 {
		t.Fatal("invalid input reached tools/call")
	}
}

func TestMCPRuntimeCLIResults(t *testing.T) {
	cliProfileIsolation(t)
	peer := newMCPRuntimePeer(t)
	model := newMCPRuntimeModel(t)
	const cue = "MCP_CLI_MIXED_RESULT"
	model.add(cue, mcpRuntimeCLI(`call runtime-fixture mixed-result --args '{"label":"mixed"}'`))
	srv := Start(t, &Stub{srv: model.server})
	srv.CreateBot("Worker", "Independent MCP JSON projection and real artifact acceptance.")
	mcpRuntimeAPI(t, srv, http.MethodPost, "/api/bots/Worker/mcp", map[string]string{"name": "runtime-fixture", "url": peer.server.URL + "/results"}, 200)
	srv.Send("Worker", cue)
	result := model.results(cue)[0]
	reply := mcpRuntimeReply(t, result)
	stdout := commandOutput(result)
	if strings.Contains(stdout, base64.StdEncoding.EncodeToString(peer.image)) || strings.Contains(stdout, strings.Repeat("COMPLETE_TEXT_", 300)) {
		t.Fatal("CLI printed image base64 or a large result instead of artifact references")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(mcpRuntimeReadArtifact(t, srv, reply.Raw), &raw); err != nil {
		t.Fatal(err)
	}
	if string(raw["x-independent-raw"]) != `"RAW_EXTENSION_RETAINED"` {
		t.Fatal("raw unknown result fields were discarded")
	}
	structured := mcpRuntimeReadArtifact(t, srv, reply.Structured)
	if !json.Valid(structured) || !bytes.Contains(structured, []byte("STRUCTURED_RETAINED")) || !bytes.Contains(structured, []byte("9007199254740993")) {
		t.Fatal("structured result was not preserved completely")
	}
	seen := map[string]bool{}
	for _, part := range reply.Content {
		seen[part.Type] = true
		switch part.Type {
		case "text":
			if part.Text != "" || len(part.Preview) > 2048 {
				t.Fatal("large text did not use a bounded preview")
			}
			file := mcpRuntimeReadArtifact(t, srv, &mcpRuntimeArtifact{File: part.File, Access: part.Access})
			if string(file) != strings.Repeat("COMPLETE_TEXT_", 8000) {
				t.Fatal("large text artifact was truncated")
			}
		case "image":
			data := mcpRuntimeReadArtifact(t, srv, &mcpRuntimeArtifact{File: part.File, Access: part.Access})
			config, format, err := image.DecodeConfig(bytes.NewReader(data))
			if err != nil || format != "png" || config.Width != 4 || config.Height != 3 || part.MIMEType != "image/png" || !bytes.Equal(data, peer.image) {
				t.Fatal("known image is not the actual decoded PNG artifact")
			}
		case "independent-unknown":
			data := mcpRuntimeReadArtifact(t, srv, &mcpRuntimeArtifact{File: part.File, Access: part.Access})
			if !json.Valid(data) || !bytes.Contains(data, []byte("UNKNOWN_BLOCK_RETAINED")) {
				t.Fatal("unknown result block did not survive as full JSON")
			}
		case "resource_link":
			if part.URI != peer.server.URL+"/resource-must-not-fetch" {
				t.Fatal("resource link URI was discarded")
			}
		}
	}
	for _, kind := range []string{"text", "image", "independent-unknown", "resource_link"} {
		if !seen[kind] {
			t.Fatalf("projected content type %s was discarded", kind)
		}
	}
	peer.mu.Lock()
	fetches := peer.fetches
	peer.mu.Unlock()
	if fetches != 0 {
		t.Fatal("resource_link was fetched")
	}

	human := func(tool string) (mcpRuntimeCLIReply, error) {
		t.Helper()
		out, diagnostic, err := mcpRuntimeExternal(t, srv.env, "", "mcp", "call", "runtime-fixture", tool, "--http", srv.URL, "--bot", "Worker", "--json")
		if out == "" {
			t.Fatalf("business error did not preserve JSON content: %s", diagnostic)
		}
		return mcpRuntimeJSON(t, out), err
	}
	failed, err := human("remote-error")
	if err == nil || !failed.IsError || len(failed.Content) == 0 || failed.Content[0].Text != "REMOTE_ERROR_CONTENT_RETAINED" {
		t.Fatal("isError did not return preserved content and a nonzero CLI exit")
	}
	if !bytes.Contains(mcpRuntimeReadArtifact(t, srv, failed.Raw), []byte("REMOTE_ERROR_CONTENT_RETAINED")) {
		t.Fatal("isError lost the raw result")
	}
	bad, err := human("bad-image")
	if err == nil || len(bad.ProjectionErrors) == 0 || len(bad.Content) == 0 || bad.Content[0].ProjectionError == "" {
		t.Fatal("image projection failure was silent or reported successful exit")
	}
	original := mcpRuntimeReadArtifact(t, srv, &mcpRuntimeArtifact{File: bad.Content[0].File, Access: bad.Content[0].Access})
	if !json.Valid(original) || !bytes.Contains(original, []byte("not-valid-base64")) {
		t.Fatal("failed image projection discarded its original block")
	}

	// Metadata too large for stdout must retain complete schemas in a file.
	oldSession := peer.firstSession()
	spec, _ := json.Marshal(map[string]any{"servers": []any{map[string]string{"name": "runtime-fixture", "url": peer.server.URL + "/large-tools"}}})
	mcpRuntimeReplace(t, filepath.Join(srv.Root, "bots", "Worker", "mcp.json"), spec)
	mcpRuntimeClosed(t, peer, oldSession)
	for _, command := range [][]string{{"tools", "runtime-fixture"}, {"describe", "runtime-fixture", mcpRuntimeRawName}} {
		args := append([]string{"mcp"}, command...)
		args = append(args, "--http", srv.URL, "--bot", "Worker", "--json")
		out, diagnostic, err := mcpRuntimeExternal(t, srv.env, "", args...)
		if err != nil {
			t.Fatalf("large metadata CLI failed: %s", diagnostic)
		}
		metadata := mcpRuntimeJSON(t, out)
		complete := mcpRuntimeReadArtifact(t, srv, metadata.Artifact)
		if !json.Valid(complete) || !bytes.Contains(complete, []byte(strings.Repeat("COMPLETE_SCHEMA_", 6000))) || !bytes.Contains(complete, []byte(mcpRuntimeRawName)) || !bytes.Contains(complete, []byte("inputSchema")) {
			t.Fatal("large tools/describe metadata did not preserve complete schema JSON")
		}
	}
}

func TestMCPRuntimeCLIResearch(t *testing.T) {
	cliProfileIsolation(t)
	peer := newMCPRuntimePeer(t)
	model := newMCPRuntimeModel(t)
	srv := Start(t, &Stub{srv: model.server})
	srv.CreateBot("Worker", "Independent MCP research CLI acceptance.")
	mcpRuntimeAPI(t, srv, http.MethodPost, "/api/bots/Worker/mcp", map[string]string{"name": "runtime-fixture", "url": peer.server.URL + "/research"}, 200)
	model.add("MCP_CLI_MAIN_WARM", mcpRuntimeCall("counter", "main-warm")...)
	srv.Send("Worker", "MCP_CLI_MAIN_WARM")
	warm := mcpRuntimeResult(t, model, "MCP_CLI_MAIN_WARM")
	sentinel := filepath.Join(srv.Root, "research-injection-must-not-run")
	const child = "MCP_CLI_RESEARCH_A"
	model.add(child,
		mcpRuntimeCLI("list"), mcpRuntimeCLI("tools runtime-fixture"), mcpRuntimeCLI("describe runtime-fixture "+shellQuote(mcpRuntimeRawName)),
		mcpRuntimeCLI("call runtime-fixture "+shellQuote(mcpRuntimeRawName)+` --readonly --args '{"label":"research-positive"}'`),
		mcpRuntimeCLI(`call runtime-fixture held --readonly --args '{"label":"unhinted"}'`),
		mcpRuntimeShell(`"$XBOT_BIN" mcp list --self --json; printf injected > `+shellQuote(sentinel)),
	)
	model.add("MCP_CLI_PARENT_PLAN", mcpRuntimeTool("dispatch", map[string]string{"type": "research", "task": child}))
	srv.Send("Worker", "MCP_CLI_PARENT_PLAN")
	WaitFor(t, 12*time.Second, "research real CLI steps", func() (bool, string) {
		r := model.results(child)
		return r[len(r)-1] != "", "child has not completed its CLI sequence"
	})
	results := model.results(child)
	for _, result := range results[:4] {
		mcpRuntimeReply(t, result)
	}
	read := mcpRuntimeCounter(t, mcpRuntimeReply(t, results[3]))
	if read.Session != warm.Session || read.Count != warm.Count+1 {
		t.Fatal("research CLI did not use the same Bot session")
	}
	rejected := strings.ToLower(results[4])
	if !regexp.MustCompile(`^exit [1-9][0-9]*\n`).MatchString(results[4]) || peer.callCount("unhinted") != 0 || !(strings.Contains(rejected, "read") || strings.Contains(rejected, "hint") || strings.Contains(rejected, "只读")) {
		t.Fatal("readonly call did not reject the exact false hint before RPC")
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatal("research accepted a shell operator instead of one literal CLI call")
	}
	if strings.HasPrefix(results[5], "exit 0\n") {
		t.Fatal("research operator command was reported successful")
	}
	mcpRuntimeNoSchemas(t, model, "MCP_CLI_PARENT_PLAN", callerForeground)
	mcpRuntimeNoSchemas(t, model, child, callerSubagent)

	// Hold the same session's actual discovery request, replace its spec,
	// then require zero call RPC on both the old and new session.
	beforeSwapRPCs := peer.rpcCount()
	listGate := peer.gate("readonly-list", 0)
	defer listGate.allow()
	path := filepath.Join(srv.Root, "bots", "Worker", "mcp.json")
	replace := func(endpoint string) {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"servers": []any{map[string]string{"name": "runtime-fixture", "url": peer.server.URL + endpoint}}})
		mcpRuntimeReplace(t, path, raw)
	}
	replace("/hint-check")
	const changedChild = "MCP_CLI_RESEARCH_B"
	model.add(changedChild, mcpRuntimeCLI("call runtime-fixture "+shellQuote(mcpRuntimeRawName)+` --readonly --args '{"label":"readonly-swap"}'`))
	model.add("MCP_CLI_PARENT_SWAP", mcpRuntimeTool("dispatch", map[string]string{"type": "research", "task": changedChild}))
	srv.Send("Worker", "MCP_CLI_PARENT_SWAP")
	mcpRuntimeWait(t, listGate.entered, 10*time.Second, "readonly did not inspect its actual session tool list")
	replace("/hint-replacement")
	mcpRuntimeWait(t, listGate.canceled, 8*time.Second, "spec change did not stop the old readonly discovery")
	WaitFor(t, 10*time.Second, "readonly CLI observes spec-change failure", func() (bool, string) { return model.results(changedChild)[0] != "", "child CLI still running" })
	if strings.HasPrefix(model.results(changedChild)[0], "exit 0\n") || peer.rpcCount() != beforeSwapRPCs || peer.callCount("readonly-swap") != 0 {
		t.Fatal("readonly discovery and call were routed to different sessions after replacement")
	}
	mcpRuntimeNoSchemas(t, model, changedChild, callerSubagent)
}
