//go:build smoke

package smoke

import (
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
		return false, srv.Stderr()
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
	// Existing authenticated management entry. No status/describe probe is used
	// to make the new configuration start its notification receiver.
	mcpRuntimeAPI(t, srv, http.MethodPost, "/api/apps/custom", map[string]string{"name": "runtime-fixture", "url": peer.server.URL + "/original"}, 200)
	WaitFor(t, 8*time.Second, "new MCP notification receivers", func() (bool, string) {
		return peer.sessionCount() >= len(bots.Bots), fmt.Sprint(peer.sessionCount())
	})
	model.add("MCP_RUNTIME_WEB_ONE", mcpRuntimeCall("counter", "web-one")...)
	srv.Send("Worker", "MCP_RUNTIME_WEB_ONE")
	first := mcpRuntimeResult(t, model, "MCP_RUNTIME_WEB_ONE")
	if first.Count != 1 {
		t.Fatalf("first counter result: %+v", first)
	}
	if got := peer.sessionCount(); got != len(bots.Bots) {
		t.Fatalf("same Bot opened additional MCP owners for background and web: initializes=%d configured Bots=%d", got, len(bots.Bots))
	}
	model.add("MCP_RUNTIME_NOTIFY", mcpRuntimeCall("counter", "notification-pass")...)
	peer.note(t, first.Session, "MCP_RUNTIME_NOTIFY")
	mcpRuntimeTurnDone(t, srv, "Worker", "MCP_RUNTIME_NOTIFY")
	notified := mcpRuntimeResult(t, model, "MCP_RUNTIME_NOTIFY")
	if notified.Session != first.Session || notified.Count != 2 {
		t.Fatalf("notification/short Pass did not share the calling session: first=%+v notification=%+v", first, notified)
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
	t.Logf("one initialize per Bot; Worker web/notification-short-Pass/web counter 1/2/3 in %s; Other counter 1 in independent %s", first.Session, other.Session)
}
