//go:build smoke

package smoke

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"xbot/internal/provider"
	"xbot/internal/work"
)

// Independent acceptance: only the upstream model is scripted. Creation,
// admission, tools, notifications, persistence and child HTTP routes are real.
// The implementation must satisfy these expectations; fixture state must never
// manufacture history that is absent from an actual model request.
func TestContinuableSubagentBinary(t *testing.T) {
	var nonceBytes [8]byte
	if _, err := rand.Read(nonceBytes[:]); err != nil {
		t.Fatal(err)
	}
	nonce := hex.EncodeToString(nonceBytes[:])
	initial := "ACCEPT_CHILD_TASK_" + nonce
	feedback := "ACCEPT_CHILD_FEEDBACK_" + nonce
	firstBody := "version one " + nonce + "\n"
	secondBody := firstBody + "version two\n"
	firstConclusion := "ACCEPT_FIRST_CONCLUSION_" + nonce
	secondConclusion := "ACCEPT_SECOND_CONCLUSION_" + nonce
	firstGate, secondGate := make(chan struct{}), make(chan struct{})
	var firstRelease, secondRelease sync.Once
	releaseFirst := func() { firstRelease.Do(func() { close(firstGate) }) }
	releaseSecond := func() { secondRelease.Do(func() { close(secondGate) }) }
	// Cleanup runs in reverse registration order: release before stopping serve.
	defer releaseFirst()
	defer releaseSecond()

	var mu sync.Mutex
	var childRequests []continuationRequest
	var artifact string // Assigned before any message can invoke the model.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			t.Errorf("unexpected scripted upstream request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected upstream route", http.StatusBadRequest)
			return
		}
		var req continuationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode scripted request: %v", err)
			http.Error(w, "invalid scripted request", http.StatusBadRequest)
			return
		}
		view := req.stub()
		caller, _ := classify(view)
		if caller == callerCompaction || caller == callerExtract {
			t.Error("the tiny acceptance conversation unexpectedly requested summarization")
			http.Error(w, "unexpected summarization", http.StatusBadRequest)
			return
		}
		if caller != callerSubagent {
			// A completion is an ordinary input, not another instruction to spawn.
			if strings.Contains(lastUser(view), `source="subagent"`) {
				streamOwner(w, view, "Acceptance received the child's automatic result.")
				return
			}
			if !strings.Contains(joined(view), initial) {
				t.Error("unexpected parent input in independent acceptance")
				http.Error(w, "unexpected parent input", http.StatusBadRequest)
				return
			}
			if _, ok := req.result("accept_dispatch"); !ok {
				streamTool(w, "accept_dispatch", "dispatch", argsJSON(map[string]string{
					"type": "dev", "task": initial, "context": "fresh",
				}))
				return
			}
			streamOwner(w, view, "Acceptance dispatched one local collaborator.")
			return
		}

		mu.Lock()
		childRequests = append(childRequests, req)
		mu.Unlock()
		reject := func(reason string) {
			t.Errorf("child request violates continuation contract: %s", reason)
			http.Error(w, reason, http.StatusBadRequest)
		}
		if !req.text("user", initial) {
			reject("original task is absent from the child conversation")
			return
		}
		if req.text("user", feedback) {
			if !req.text("assistant", firstConclusion) || !req.call("accept_write_1", "write_file") {
				reject("second turn lost the first conclusion or first tool call")
				return
			}
			if result, ok := req.result("accept_write_1"); !ok || !strings.HasPrefix(result, "wrote ") {
				reject("second turn lost the successful first tool result")
				return
			}
			select {
			case <-secondGate:
			case <-r.Context().Done():
				return
			}
			if result, ok := req.result("accept_read_2"); !ok {
				streamTool(w, "accept_read_2", "read_file", argsJSON(map[string]string{"path": artifact}))
				return
			} else if !strings.Contains(result, strings.TrimSpace(firstBody)) {
				reject("second turn did not read the real first artifact")
				return
			}
			if result, ok := req.result("accept_write_2"); !ok {
				streamTool(w, "accept_write_2", "write_file", argsJSON(map[string]string{"path": artifact, "content": secondBody}))
				return
			} else if !strings.HasPrefix(result, "wrote ") {
				reject("second write failed: " + result)
				return
			}
			streamText(w, []string{secondConclusion})
			return
		}
		select {
		case <-firstGate:
		case <-r.Context().Done():
			return
		}
		if result, ok := req.result("accept_write_1"); !ok {
			streamTool(w, "accept_write_1", "write_file", argsJSON(map[string]string{"path": artifact, "content": firstBody}))
			return
		} else if !strings.HasPrefix(result, "wrote ") {
			reject("first write failed: " + result)
			return
		}
		streamText(w, []string{firstConclusion})
	}))
	t.Cleanup(upstream.Close)
	srv := Start(t, &Stub{srv: upstream})
	t.Cleanup(releaseFirst)
	t.Cleanup(releaseSecond)
	srv.CreateBot("collaborator", "Independent acceptance of a continuable local collaborator.")
	artifact = filepath.Join(work.BotDir(srv.Root, "collaborator"), "acceptance.txt")
	opening := srv.Send("collaborator", initial)
	if len(opening.Delivered) == 0 {
		t.Fatal("initial real message SSE did not deliver the parent's acknowledgement")
	}
	releaseFirst()

	var childID string
	WaitFor(t, 15*time.Second, "one completed local child and its real artifact", func() (bool, string) {
		agents := srv.Agents("collaborator")
		if len(agents) != 1 {
			return false, fmt.Sprintf("agents=%+v", agents)
		}
		childID = agents[0].ID
		body, err := os.ReadFile(artifact)
		return agents[0].Status == "done" && err == nil && string(body) == firstBody,
			fmt.Sprintf("agent=%+v artifact=%q error=%v", agents[0], body, err)
	})
	waitContinuationEnd(t, srv, childID, "", 1)
	t.Logf("real binary first turn passed: child=%s artifact=%q automatic end in parent transcript", childID, firstBody)

	// This is a public Web capability assertion, not a disk-reading substitute.
	before := readContinuationTranscript(t, srv, childID)
	if before.ID != childID || before.Status != "done" || !before.Continuable || before.TurnSeq <= 0 {
		t.Fatalf("completed child is not discoverable as continuable: %+v", before)
	}
	assertContinuationEntries(t, before.Entries, initial, firstConclusion, "accept_write_1")
	first := readContinuationAgent(t, srv, childID)
	if !first.Continuable || first.TurnSeq != before.TurnSeq {
		t.Fatalf("Bot detail and child conversation disagree: detail=%+v conversation=%+v", first, before)
	}
	assertContinuationResultPath(t, first.ResultPath, first.TurnSeq)
	oldResult, err := os.ReadFile(first.ResultPath)
	if err != nil || !strings.Contains(string(oldResult), firstConclusion) {
		t.Fatalf("first immutable result: %q, %v", oldResult, err)
	}
	waitContinuationEnd(t, srv, childID, first.ResultPath, 1)

	// The same real serve process now receives the follow-up from its shipped
	// browser UI, connecting the binary/tool evidence to the actual user entry.
	releaseSecond()
	status, raw := continueSubagentViaBrowser(t, srv, childID, feedback, firstBody, firstConclusion, secondConclusion)
	if status != http.StatusOK {
		t.Fatalf("browser follow-up to completed child: %d %s", status, raw)
	}
	var receipt struct {
		ID       string `json:"id"`
		Status   string `json:"status"`
		Position int    `json:"position"`
		TurnSeq  int    `json:"turn_seq"`
	}
	if err := json.Unmarshal(raw, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.ID != childID || receipt.Status != "running" {
		t.Fatalf("idle child did not resume under its original identity with free admission: %s", raw)
	}

	var second continuationAgent
	WaitFor(t, 15*time.Second, "second completed turn on the original child", func() (bool, string) {
		second = readContinuationAgent(t, srv, childID)
		body, err := os.ReadFile(artifact)
		return second.Status == "done" && second.TurnSeq > first.TurnSeq && err == nil && string(body) == secondBody,
			fmt.Sprintf("agent=%+v artifact=%q error=%v", second, body, err)
	})
	if !second.Continuable || second.ResultPath == first.ResultPath {
		t.Fatalf("second turn lost continuability or reused the first result reference: first=%+v second=%+v", first, second)
	}
	assertContinuationResultPath(t, second.ResultPath, second.TurnSeq)
	newResult, err := os.ReadFile(second.ResultPath)
	if err != nil || !strings.Contains(string(newResult), secondConclusion) {
		t.Fatalf("second result missing: %q, %v", newResult, err)
	}
	oldResultAfter, err := os.ReadFile(first.ResultPath)
	if err != nil || string(oldResultAfter) != string(oldResult) {
		t.Fatalf("a later turn changed the old result reference: before=%q after=%q error=%v", oldResult, oldResultAfter, err)
	}
	waitContinuationEnd(t, srv, childID, second.ResultPath, 2)
	after := readContinuationTranscript(t, srv, childID)
	if after.ID != childID || after.TurnSeq != second.TurnSeq || !after.Continuable || after.Status != "done" {
		t.Fatalf("second child conversation has wrong identity or state: %+v", after)
	}
	if len(after.Entries) <= len(before.Entries) || !reflect.DeepEqual(before.Entries, after.Entries[:len(before.Entries)]) {
		t.Fatalf("continuation rewrote or lost old child entries: before=%+v after=%+v", before.Entries, after.Entries)
	}
	assertContinuationEntries(t, after.Entries, feedback, secondConclusion, "accept_write_2")
	if agents := srv.Agents("collaborator"); len(agents) != 1 || agents[0].ID != childID {
		t.Fatalf("follow-up spawned another identity: %+v", agents)
	}
	mu.Lock()
	defer mu.Unlock()
	var resumed bool
	for _, req := range childRequests {
		resumed = resumed || req.text("user", feedback)
	}
	if !resumed {
		t.Fatal("HTTP follow-up never reached the real child provider")
	}
	t.Logf("same child %s completed turns %d/%d; old result unchanged; real artifact updated", childID, first.TurnSeq, second.TurnSeq)
}

type continuationRequest struct {
	Model           string          `json:"model"`
	Thinking        json.RawMessage `json:"thinking"`
	ReasoningEffort string          `json:"reasoning_effort"`
	Tools           []struct {
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	} `json:"tools"`
	Messages []struct {
		Role       string              `json:"role"`
		Content    string              `json:"content"`
		ToolCallID string              `json:"tool_call_id"`
		ToolCalls  []provider.ToolCall `json:"tool_calls"`
	} `json:"messages"`
}

func (r continuationRequest) stub() stubRequest {
	var out stubRequest
	for _, msg := range r.Messages {
		m := stubMessage{Role: msg.Role, Content: msg.Content}
		for _, call := range msg.ToolCalls {
			raw, _ := json.Marshal(call)
			m.ToolCalls = append(m.ToolCalls, raw)
		}
		out.Messages = append(out.Messages, m)
	}
	return out
}

func (r continuationRequest) text(role, marker string) bool {
	for _, msg := range r.Messages {
		if msg.Role == role && strings.Contains(msg.Content, marker) {
			return true
		}
	}
	return false
}

func (r continuationRequest) result(id string) (string, bool) {
	for _, msg := range r.Messages {
		if msg.Role == "tool" && msg.ToolCallID == id {
			return msg.Content, true
		}
	}
	return "", false
}

func (r continuationRequest) call(id, name string) bool {
	for _, msg := range r.Messages {
		for _, call := range msg.ToolCalls {
			if msg.Role == "assistant" && call.ID == id && call.Function.Name == name {
				return true
			}
		}
	}
	return false
}

type continuationEntry struct {
	Seq     int              `json:"seq"`
	Time    string           `json:"time"`
	Message provider.Message `json:"message"`
}

type continuationTranscript struct {
	ID          string              `json:"id"`
	Entries     []continuationEntry `json:"entries"`
	HasMore     bool                `json:"has_more"`
	NextBefore  int                 `json:"next_before"`
	TurnSeq     int                 `json:"turn_seq"`
	Status      string              `json:"status"`
	Continuable bool                `json:"continuable"`
}

func readContinuationTranscript(t *testing.T, srv *Server, id string) continuationTranscript {
	t.Helper()
	status, raw := srv.do(http.MethodGet, "/api/bots/collaborator/agents/"+id+"/transcript?limit=200", nil)
	if status != http.StatusOK {
		t.Fatalf("Web cannot read the completed child's full conversation: HTTP %d %s", status, raw)
	}
	var out continuationTranscript
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.HasMore || len(out.Entries) == 0 {
		t.Fatalf("tiny child conversation is incomplete: %+v", out)
	}
	for i, entry := range out.Entries {
		if entry.Seq <= 0 || entry.Time == "" || (i > 0 && entry.Seq <= out.Entries[i-1].Seq) {
			t.Fatalf("child entries do not preserve ordered identity/time: %+v", out.Entries)
		}
	}
	return out
}

type continuationAgent struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	Continuable bool   `json:"continuable"`
	TurnSeq     int    `json:"turn_seq"`
	ResultPath  string `json:"result_path"`
}

func readContinuationAgent(t *testing.T, srv *Server, id string) continuationAgent {
	t.Helper()
	status, raw := srv.do(http.MethodGet, "/api/bots/collaborator", nil)
	if status != http.StatusOK {
		t.Fatalf("Bot detail: HTTP %d %s", status, raw)
	}
	var out struct {
		Agents []continuationAgent `json:"agents"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	for _, child := range out.Agents {
		if child.ID == id {
			return child
		}
	}
	t.Fatalf("original child %s disappeared from its parent: %s", id, raw)
	return continuationAgent{}
}

func assertContinuationResultPath(t *testing.T, path string, seq int) {
	t.Helper()
	if !filepath.IsAbs(path) || filepath.Base(filepath.Dir(path)) != "results" || filepath.Base(path) != fmt.Sprintf("%d.txt", seq) {
		t.Fatalf("turn %d lacks its stable results/<first-input-seq>.txt reference: %q", seq, path)
	}
}

func assertContinuationEntries(t *testing.T, entries []continuationEntry, input, conclusion, callID string) {
	t.Helper()
	var sawInput, sawConclusion, sawCall, sawResult bool
	for _, entry := range entries {
		msg := entry.Message
		sawInput = sawInput || msg.Role == "user" && strings.Contains(msg.Text(), input)
		sawConclusion = sawConclusion || msg.Role == "assistant" && strings.Contains(msg.Text(), conclusion)
		for _, call := range msg.Calls() {
			sawCall = sawCall || call.ID == callID && call.Function.Name == "write_file"
		}
		sawResult = sawResult || msg.Role == "tool" && msg.CallID() == callID && strings.HasPrefix(msg.Text(), "wrote ")
	}
	if !sawInput || !sawConclusion || !sawCall || !sawResult {
		t.Fatalf("Web child conversation missing input/conclusion/tool call/successful result: %t/%t/%t/%t entries=%+v", sawInput, sawConclusion, sawCall, sawResult, entries)
	}
}

func waitContinuationEnd(t *testing.T, srv *Server, id, resultPath string, count int) {
	t.Helper()
	WaitFor(t, 15*time.Second, fmt.Sprintf("%d ordinary automatic child results in the parent", count), func() (bool, string) {
		rows := srv.Transcript("collaborator")
		n := 0
		pathSeen := resultPath == ""
		for _, row := range rows {
			if row.Role == "user" && strings.Contains(row.Content, `source="subagent"`) &&
				strings.Contains(row.Content, `sender="`+id+`"`) && strings.Contains(row.Content, `type="done"`) {
				n++
				pathSeen = pathSeen || strings.Contains(row.Content, resultPath)
			}
		}
		return n == count && pathSeen, dumpRows(rows)
	})
}
