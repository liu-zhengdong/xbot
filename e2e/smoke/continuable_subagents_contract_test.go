//go:build smoke

package smoke

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"xbot/internal/work"
)

// Each handler reads the current request to decide its next response. Recorded
// requests are observation only: they never fill in missing model context.
type continuationScript struct {
	t        *testing.T
	server   *httptest.Server
	handle   func(http.ResponseWriter, *http.Request, continuationRequest, string) bool
	mu       sync.Mutex
	requests []continuationRequest
}

func startContinuationScript(t *testing.T, handle func(http.ResponseWriter, *http.Request, continuationRequest, string) bool) (*Server, *continuationScript) {
	t.Helper()
	s := &continuationScript{t: t, handle: handle}
	s.server = httptest.NewServer(http.HandlerFunc(s.serveHTTP))
	t.Cleanup(s.server.Close)
	srv := Start(t, &Stub{srv: s.server})
	srv.CreateBot("collaborator", "Independent scripted acceptance of local child collaboration.")
	return srv, s
}

func (s *continuationScript) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/chat/completions") {
		s.t.Errorf("unexpected acceptance upstream route: %s %s", r.Method, r.URL.Path)
		http.Error(w, "unexpected route", http.StatusBadRequest)
		return
	}
	var req continuationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.t.Errorf("decode acceptance upstream: %v", err)
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.requests = append(s.requests, req)
	s.mu.Unlock()
	caller, _ := classify(req.stub())
	if s.handle(w, r, req, caller) {
		return
	}
	if caller == callerForeground && strings.Contains(lastUser(req.stub()), `source="subagent"`) {
		streamOwner(w, req.stub(), "Acceptance received the child's ordinary report.")
		return
	}
	s.t.Errorf("unexpected %s request in independent acceptance: %s", caller, joined(req.stub()))
	http.Error(w, "unexpected scripted request", http.StatusBadRequest)
}

func (s *continuationScript) childRequests() []continuationRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []continuationRequest
	for _, req := range s.requests {
		caller, _ := classify(req.stub())
		if caller == callerSubagent {
			out = append(out, req)
		}
	}
	return out
}

type continuationHold struct {
	entered, released, cancelled       chan struct{}
	enterOnce, releaseOnce, cancelOnce sync.Once
}

func newContinuationHold(t *testing.T) *continuationHold {
	h := &continuationHold{entered: make(chan struct{}), released: make(chan struct{}), cancelled: make(chan struct{})}
	t.Cleanup(h.release)
	return h
}

func (h *continuationHold) release() { h.releaseOnce.Do(func() { close(h.released) }) }

func (h *continuationHold) wait(ctx context.Context) bool {
	h.enterOnce.Do(func() { close(h.entered) })
	select {
	case <-h.released:
		return true
	case <-ctx.Done():
		h.cancelOnce.Do(func() { close(h.cancelled) })
		return false
	}
}

func awaitContinuationSignal(t *testing.T, ch <-chan struct{}, reason string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(15 * time.Second):
		t.Fatalf("timed out waiting for %s", reason)
	}
}

var continuationIDPattern = regexp.MustCompile(`sa_[a-z0-9]+`)

func continuationDispatchID(t *testing.T, req continuationRequest, callID string) string {
	t.Helper()
	result, ok := req.result(callID)
	id := continuationIDPattern.FindString(result)
	if !ok || id == "" {
		t.Errorf("dispatch did not return a child identity: %q", result)
	}
	return id
}

func waitContinuationChild(t *testing.T, srv *Server, task, status string) continuationAgent {
	t.Helper()
	var out continuationAgent
	WaitFor(t, 15*time.Second, "child "+task+" to become "+status, func() (bool, string) {
		agents := srv.Agents("collaborator")
		for _, agent := range agents {
			if agent.Task == task && (status == "" || agent.Status == status) {
				out = readContinuationAgent(t, srv, agent.ID)
				return true, ""
			}
		}
		return false, fmt.Sprintf("agents=%+v", agents)
	})
	return out
}

func sendContinuationMessage(t *testing.T, srv *Server, id, text, delivery string) continuationAgent {
	t.Helper()
	body := map[string]string{"text": text}
	if delivery != "" {
		body["delivery"] = delivery
	}
	status, raw := srv.do(http.MethodPost, "/api/bots/collaborator/agents/"+id+"/messages", body)
	if status != http.StatusOK {
		t.Fatalf("message original child %s: HTTP %d %s", id, status, raw)
	}
	var out continuationAgent
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.ID != id || (out.Status != "running" && out.Status != "queued") {
		t.Fatalf("child message was not accepted under original identity: %s", raw)
	}
	return out
}

func TestContinuableSubagentContextBinary(t *testing.T) {
	seed := "ONLY_COMPLETED_PARENT_HISTORY_" + filepath.Base(t.TempDir())
	var childDir string
	modes := []string{"default", "fresh", "fork"}
	srv, script := startContinuationScript(t, func(w http.ResponseWriter, r *http.Request, req continuationRequest, caller string) bool {
		if caller == callerSubagent {
			mode := ""
			for _, candidate := range modes {
				if req.text("user", "CONTEXT_CHILD_"+candidate) {
					mode = candidate
				}
			}
			if mode == "" {
				return false
			}
			if req.text("user", seed) != (mode == "fork") {
				t.Errorf("%s child inherited wrong completed parent history", mode)
			}
			raw, _ := json.Marshal(req)
			for _, forbidden := range []string{"CURRENT_PARENT_" + mode, "current_parent_tool_" + mode} {
				if strings.Contains(string(raw), forbidden) {
					t.Errorf("%s child inherited incomplete current parent turn %q", mode, forbidden)
				}
			}
			assertContinuationToolPairs(t, req)
			callID := "child_cwd_" + mode
			if result, ok := req.result(callID); !ok {
				streamTool(w, callID, "sh", argsJSON(map[string]string{"description": "Change only the child's working directory", "command": "cd " + shellQuote(childDir) + " && pwd"}))
			} else {
				if !strings.Contains(result, childDir) || !strings.HasPrefix(result, "exit 0") {
					t.Errorf("child scoped cd did not succeed: %s", result)
				}
				streamText(w, []string{"CONTEXT_DONE_" + mode})
			}
			return true
		}
		if caller != callerForeground {
			return false
		}
		text := lastUser(req.stub())
		if strings.Contains(text, seed) {
			streamOwner(w, req.stub(), "Recorded the completed parent fact.")
			return true
		}
		if strings.Contains(text, "CHECK_PARENT_CWD") {
			if result, ok := req.result("parent_pwd"); !ok {
				streamTool(w, "parent_pwd", "sh", `{"description":"Read the parent's independent working directory","command":"pwd"}`)
			} else {
				if strings.Contains(result, childDir) {
					t.Errorf("child cd changed the parent's directory: %s", result)
				}
				streamOwner(w, req.stub(), "Parent working directory stayed independent.")
			}
			return true
		}
		for _, mode := range modes {
			if !strings.Contains(text, "CURRENT_PARENT_"+mode) {
				continue
			}
			if _, ok := req.result("current_parent_tool_" + mode); !ok {
				streamTool(w, "current_parent_tool_"+mode, "sh", argsJSON(map[string]string{"description": "Mark the unfinished parent turn", "command": "printf current-parent-" + mode}))
			} else if _, ok := req.result("dispatch_context_" + mode); !ok {
				args := map[string]string{"type": "dev", "task": "CONTEXT_CHILD_" + mode}
				if mode != "default" {
					args["context"] = mode
				}
				streamTool(w, "dispatch_context_"+mode, "dispatch", argsJSON(args))
			} else {
				streamOwner(w, req.stub(), "Created the requested context variant.")
			}
			return true
		}
		return false
	})
	childDir = filepath.Join(work.BotDir(srv.Root, "collaborator"), "child-cwd")
	if err := os.MkdirAll(childDir, 0700); err != nil {
		t.Fatal(err)
	}
	srv.Send("collaborator", seed)
	for _, mode := range modes {
		srv.Send("collaborator", "CURRENT_PARENT_"+mode)
		child := waitContinuationChild(t, srv, "CONTEXT_CHILD_"+mode, "done")
		waitContinuationEnd(t, srv, child.ID, child.ResultPath, 1)
	}
	srv.Send("collaborator", "CHECK_PARENT_CWD")
	if len(script.childRequests()) < len(modes) {
		t.Fatal("not every context variant reached the real child provider")
	}
}

func assertContinuationToolPairs(t *testing.T, req continuationRequest) {
	t.Helper()
	calls, results := map[string]int{}, map[string]int{}
	for _, msg := range req.Messages {
		for _, call := range msg.ToolCalls {
			calls[call.ID]++
		}
		if msg.Role == "tool" {
			results[msg.ToolCallID]++
		}
	}
	for id, n := range calls {
		if n != 1 || results[id] != 1 {
			t.Errorf("incomplete/duplicate inherited tool pair %s: calls=%d results=%d", id, n, results[id])
		}
	}
	for id := range results {
		if calls[id] != 1 {
			t.Errorf("inherited orphan tool result: %s", id)
		}
	}
}

func TestContinuableSubagentDeliveryBinary(t *testing.T) {
	const task, steer, queued = "DELIVERY_CHILD", "DELIVERY_STEER", "DELIVERY_QUEUE"
	initialHold := newContinuationHold(t)
	srv, _ := startContinuationScript(t, func(w http.ResponseWriter, r *http.Request, req continuationRequest, caller string) bool {
		if caller == callerSubagent {
			for _, marker := range []string{steer, queued} {
				for _, message := range req.Messages {
					if message.Role == "user" && strings.Contains(message.Content, marker) && !strings.Contains(message.Content, "collaborator") {
						t.Errorf("parent-to-child message lost the real parent Bot identity: %s", message.Content)
					}
				}
			}
			if req.text("user", queued) {
				if !req.text("assistant", "DELIVERY_FIRST_END") || !req.text("user", steer) {
					t.Error("queued turn started before the first turn ended or lost steered history")
				}
				streamText(w, []string{"DELIVERY_SECOND_END"})
				return true
			}
			if _, ok := req.result("delivery_boundary"); !ok {
				if initialHold.wait(r.Context()) {
					streamTool(w, "delivery_boundary", "sh", `{"description":"Reach the next real child step","command":"pwd"}`)
				}
				return true
			}
			if !req.text("user", steer) {
				t.Error("accepted steer was not consumed at the next child step")
			}
			streamText(w, []string{"DELIVERY_FIRST_END"})
			return true
		}
		if caller != callerForeground {
			return false
		}
		text := lastUser(req.stub())
		if strings.Contains(text, "DELIVERY_START") {
			if _, ok := req.result("delivery_dispatch"); !ok {
				streamTool(w, "delivery_dispatch", "dispatch", argsJSON(map[string]string{"type": "research", "task": task}))
			} else {
				streamOwner(w, req.stub(), "Child is running.")
			}
			return true
		}
		if strings.Contains(text, "DELIVERY_CONTROLS") {
			id := continuationDispatchID(t, req, "delivery_dispatch")
			if _, ok := req.result("delivery_steer"); !ok {
				streamTool(w, "delivery_steer", "agent_message", argsJSON(map[string]string{"id": id, "text": steer}))
			} else if _, ok := req.result("delivery_queue"); !ok {
				streamTool(w, "delivery_queue", "agent_message", argsJSON(map[string]string{"id": id, "text": queued, "delivery": "queue"}))
			} else {
				streamOwner(w, req.stub(), "Steer and a separate next turn were accepted.")
			}
			return true
		}
		return false
	})
	t.Cleanup(initialHold.release)
	srv.Send("collaborator", "DELIVERY_START")
	awaitContinuationSignal(t, initialHold.entered, "the running child provider")
	child := waitContinuationChild(t, srv, task, "running")
	srv.Send("collaborator", "DELIVERY_CONTROLS")
	initialHold.release()
	waitContinuationEnd(t, srv, child.ID, "", 2)
	final := waitContinuationTurn(t, srv, child.ID, child.TurnSeq)
	if final.ID != child.ID || final.TurnSeq <= child.TurnSeq {
		t.Fatalf("queue did not create an independent turn on the same identity: before=%+v after=%+v", child, final)
	}
	entries := readContinuationTranscript(t, srv, child.ID).Entries
	var first, nextInput, second int
	for _, entry := range entries {
		if entry.Message.Role == "assistant" && entry.Message.Text() == "DELIVERY_FIRST_END" {
			first = entry.Seq
		}
		if entry.Message.Role == "user" && strings.Contains(entry.Message.Text(), queued) {
			nextInput = entry.Seq
		}
		if entry.Message.Role == "assistant" && entry.Message.Text() == "DELIVERY_SECOND_END" {
			second = entry.Seq
		}
	}
	// Queue may be durably recorded before the first turn ends. Its consumption
	// is established by the second model request seeing the first conclusion.
	if first == 0 || nextInput == 0 || second <= first {
		t.Fatalf("the public child conversation lost distinct queued turn results: first=%d input=%d second=%d", first, nextInput, second)
	}
}

func TestContinuableSubagentParentMessagesBinary(t *testing.T) {
	for _, busy := range []bool{false, true} {
		t.Run(fmt.Sprintf("parent_busy=%t", busy), func(t *testing.T) {
			childStart, childFinish := newContinuationHold(t), newContinuationHold(t)
			parentBusy, parentEndBusy := newContinuationHold(t), newContinuationHold(t)
			const task, report = "PARENT_MESSAGE_CHILD", "CHILD_EARLY_FINDING_71"
			parentObserved := make(chan struct{})
			var observedOnce sync.Once
			srv, script := startContinuationScript(t, func(w http.ResponseWriter, r *http.Request, req continuationRequest, caller string) bool {
				if caller == callerSubagent {
					for _, tool := range req.Tools {
						switch tool.Function.Name {
						case "dispatch", "spawn", "agent_message", "agent_stop", "list_subagents", "read_subagent", "expand_subagent", "steer", "queue", "interrupt", "cancel", "send_message_to_user":
							t.Errorf("child was offered a tool outside direct parent communication: %s", tool.Function.Name)
						}
					}
					if _, ok := req.result("early_parent_message"); !ok {
						if childStart.wait(r.Context()) {
							streamTool(w, "early_parent_message", "send_message_to_parent", argsJSON(map[string]string{"text": report}))
						}
					} else if childFinish.wait(r.Context()) {
						streamText(w, []string{"PARENT_MESSAGE_FINAL_RESULT"})
					}
					return true
				}
				if caller != callerForeground {
					return false
				}
				text := lastUser(req.stub())
				if strings.Contains(text, "PARENT_END_BUSY_GATE") {
					if _, ok := req.result("parent_end_boundary"); !ok {
						if parentEndBusy.wait(r.Context()) {
							streamTool(w, "parent_end_boundary", "sh", `{"description":"Complete the parent's step while a child finishes","command":"pwd"}`)
						}
					} else {
						streamOwner(w, req.stub(), "Parent remained responsive while the child finished.")
					}
					return true
				}
				if req.text("user", report) {
					observedOnce.Do(func() { close(parentObserved) })
					streamOwner(w, req.stub(), "Received the child's early finding.")
					return true
				}
				if strings.Contains(text, "PARENT_MESSAGE_START") {
					if _, ok := req.result("parent_message_dispatch"); !ok {
						streamTool(w, "parent_message_dispatch", "dispatch", argsJSON(map[string]string{"type": "research", "task": task}))
					} else {
						streamOwner(w, req.stub(), "Collaborator is working.")
					}
					return true
				}
				if strings.Contains(text, "PARENT_BUSY_GATE") {
					if _, ok := req.result("parent_busy_boundary"); !ok {
						if parentBusy.wait(r.Context()) {
							streamTool(w, "parent_busy_boundary", "sh", `{"description":"Complete the parent's current step","command":"printf parent-boundary"}`)
						}
					} else {
						streamOwner(w, req.stub(), "Parent step finished.")
					}
					return true
				}
				return false
			})
			t.Cleanup(childStart.release)
			t.Cleanup(childFinish.release)
			t.Cleanup(parentBusy.release)
			t.Cleanup(parentEndBusy.release)
			srv.Send("collaborator", "PARENT_MESSAGE_START")
			awaitContinuationSignal(t, childStart.entered, "the child before its early message")
			child := waitContinuationChild(t, srv, task, "running")
			var parentStream *Stream
			if busy {
				parentStream = srv.BeginSend("collaborator", "PARENT_BUSY_GATE")
				awaitContinuationSignal(t, parentBusy.entered, "the busy parent model request")
			}
			childStart.release()
			awaitContinuationSignal(t, childFinish.entered, "successful send_message_to_parent before child completion")
			if busy {
				select {
				case <-parentBusy.cancelled:
					t.Fatal("child message cancelled the parent's unrelated active model call")
				default:
				}
				parentBusy.release()
				parentStream.Wait()
			}
			awaitContinuationSignal(t, parentObserved, "the parent to observe the real early child message")
			if status := readContinuationAgent(t, srv, child.ID).Status; status != "running" {
				t.Fatalf("early child message was conflated with completion: %s", status)
			}
			var endStream *Stream
			if busy {
				endStream = srv.BeginSend("collaborator", "PARENT_END_BUSY_GATE")
				awaitContinuationSignal(t, parentEndBusy.entered, "a busy parent while the child finishes")
			}
			childFinish.release()
			if busy {
				waitContinuationChild(t, srv, task, "done")
				parentEndBusy.release()
				endStream.Wait()
			}
			waitContinuationEnd(t, srv, child.ID, "", 1)
			rows := srv.Transcript("collaborator")
			messages, ends := 0, 0
			for _, row := range rows {
				if row.Role != "user" || !strings.Contains(row.Content, `source="subagent"`) || !strings.Contains(row.Content, `sender="`+child.ID+`"`) {
					continue
				}
				if strings.Contains(row.Content, `type="message"`) && strings.Contains(row.Content, report) {
					messages++
				}
				if strings.Contains(row.Content, `type="done"`) {
					ends++
				}
				if strings.Contains(row.Content, `type="progress"`) {
					t.Fatal("ordinary child tool progress leaked into the parent conversation")
				}
			}
			if messages != 1 || ends != 1 || len(script.childRequests()) != 2 {
				t.Fatalf("explicit message and automatic result must each arrive once: messages=%d ends=%d child calls=%d", messages, ends, len(script.childRequests()))
			}
		})
	}
}
