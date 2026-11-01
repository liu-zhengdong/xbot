//go:build smoke

package smoke

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/config"
	"xbot/internal/work"
)

func TestContinuableSubagentModelBinary(t *testing.T) {
	const selectedTask = "SELECTED_MODEL_CHILD"
	const fakeCredential = "acceptance-only-provider-credential-7264"
	t.Setenv("ACCEPTANCE_CHILD_PROVIDER_KEY", fakeCredential)
	selected := &continuationScript{t: t}
	selected.handle = func(w http.ResponseWriter, r *http.Request, req continuationRequest, caller string) bool {
		if caller != callerSubagent || !req.text("user", selectedTask) {
			return false
		}
		var thinking struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(req.Thinking, &thinking); err != nil || thinking.Type != "enabled" || req.ReasoningEffort != "high" || req.Model != "deepseek-flash" {
			t.Errorf("single-dispatch effective route/settings not honored: model=%s thinking=%s effort=%s", req.Model, req.Thinking, req.ReasoningEffort)
		}
		switch {
		case req.text("user", "MODEL_AFTER_RESTART"):
			if !req.text("assistant", "MODEL_FIRST_RESULT") || !req.text("assistant", "MODEL_SECOND_RESULT") {
				t.Error("cold selected-model continuation lost earlier child conclusions")
			}
			streamText(w, []string{"MODEL_THIRD_RESULT"})
		case req.text("user", "MODEL_FOLLOWUP"):
			if !req.text("assistant", "MODEL_FIRST_RESULT") {
				t.Error("selected-model continuation lost its first conclusion")
			}
			streamText(w, []string{"MODEL_SECOND_RESULT"})
		default:
			streamText(w, []string{"MODEL_FIRST_RESULT"})
		}
		return true
	}
	selected.server = httptest.NewServer(http.HandlerFunc(selected.serveHTTP))
	t.Cleanup(selected.server.Close)
	srv, main := startContinuationScript(t, func(w http.ResponseWriter, r *http.Request, req continuationRequest, caller string) bool {
		if req.Model != "smoke" || req.ReasoningEffort == "high" {
			t.Errorf("a single child override mutated the inherited/parent route: model=%s effort=%s", req.Model, req.ReasoningEffort)
		}
		if caller == callerSubagent {
			if !req.text("user", "INHERITED_MODEL_CHILD") {
				t.Error("selected child incorrectly used the parent's endpoint")
			}
			streamText(w, []string{"INHERITED_MODEL_RESULT"})
			return true
		}
		if caller != callerForeground {
			return false
		}
		text := lastUser(req.stub())
		switch {
		case strings.Contains(text, "MODEL_SELECTED_START"):
			if _, ok := req.result("selected_model_dispatch"); !ok {
				streamTool(w, "selected_model_dispatch", "dispatch", argsJSON(map[string]string{
					"type": "research", "task": selectedTask, "provider": "acceptance-child", "model": "deepseek-flash",
					"thinking": "on", "reasoning_effort": "high",
				}))
			} else {
				streamOwner(w, req.stub(), "The selected child route was requested.")
			}
			return true
		case strings.Contains(text, "MODEL_LIST_COMPLETED"):
			if result, ok := req.result("list_completed_children"); !ok {
				streamTool(w, "list_completed_children", "list_subagents", `{}`)
			} else {
				id := continuationDispatchID(t, req, "selected_model_dispatch")
				if !strings.Contains(result, id) || !strings.Contains(result, "done") {
					t.Errorf("the parent cannot discover its completed continuable child: %s", result)
				}
				streamOwner(w, req.stub(), "The completed collaborator remains discoverable.")
			}
			return true
		case strings.Contains(text, "MODEL_INHERITED_START"):
			if _, ok := req.result("inherited_model_dispatch"); !ok {
				streamTool(w, "inherited_model_dispatch", "dispatch", `{"type":"research","task":"INHERITED_MODEL_CHILD"}`)
			} else {
				streamOwner(w, req.stub(), "The inherited child route was requested.")
			}
			return true
		}
		return false
	})
	if err := config.Add(srv.Root, config.Provider{
		Name: "acceptance-child", Protocol: config.ChatCompletions, BaseURL: selected.server.URL + "/v1", APIKeyEnv: "ACCEPTANCE_CHILD_PROVIDER_KEY",
	}, config.ModelSpec{Name: "deepseek-flash", Context: 128000, Thinking: "off", ReasoningEffort: "low"}); err != nil {
		t.Fatal(err)
	}
	before := continuationParentBinding(t, srv)
	srv.Send("collaborator", "MODEL_SELECTED_START")
	first := waitContinuationChild(t, srv, selectedTask, "done")
	waitContinuationEnd(t, srv, first.ID, first.ResultPath, 1)
	srv.Send("collaborator", "MODEL_LIST_COMPLETED")
	if got := continuationParentBinding(t, srv); got != before {
		t.Fatalf("dispatch override rebound the parent: before=%+v after=%+v", before, got)
	}
	sendContinuationMessage(t, srv, first.ID, "MODEL_FOLLOWUP", "")
	second := waitContinuationTurn(t, srv, first.ID, first.TurnSeq)
	waitContinuationEnd(t, srv, first.ID, second.ResultPath, 2)
	srv.Restart()
	sendContinuationMessage(t, srv, first.ID, "MODEL_AFTER_RESTART", "")
	third := waitContinuationTurn(t, srv, first.ID, second.TurnSeq)
	waitContinuationEnd(t, srv, first.ID, third.ResultPath, 3)
	srv.Send("collaborator", "MODEL_INHERITED_START")
	inherited := waitContinuationChild(t, srv, "INHERITED_MODEL_CHILD", "done")
	waitContinuationEnd(t, srv, inherited.ID, inherited.ResultPath, 1)
	if got := continuationParentBinding(t, srv); got != before {
		t.Fatalf("child continuation/restart rebound the parent: before=%+v after=%+v", before, got)
	}
	if got := len(selected.childRequests()); got != 3 {
		t.Fatalf("selected endpoint must see all three child activations, got %d", got)
	}
	if got := len(main.childRequests()); got != 1 {
		t.Fatalf("default child must inherit the parent endpoint, got %d calls", got)
	}
	for _, name := range []string{"descriptor.json", "transcript.jsonl"} {
		path := filepath.Join(srv.Root, "bots", "collaborator", "agents", first.ID, name)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), fakeCredential) || strings.Contains(string(raw), selected.server.URL) {
			t.Fatalf("child persistence contains a credential or live provider endpoint: %s", name)
		}
	}
}

func continuationParentBinding(t *testing.T, srv *Server) [2]string {
	t.Helper()
	status, raw := srv.do(http.MethodGet, "/api/bots/collaborator", nil)
	if status != http.StatusOK {
		t.Fatalf("read parent binding: HTTP %d %s", status, raw)
	}
	var out struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return [2]string{out.Provider, out.Model}
}

func waitContinuationTurn(t *testing.T, srv *Server, id string, previous int) continuationAgent {
	t.Helper()
	var out continuationAgent
	WaitFor(t, 15*time.Second, "a new completed child turn after explicit input", func() (bool, string) {
		out = readContinuationAgent(t, srv, id)
		return out.Status == "done" && out.TurnSeq > previous, fmt.Sprintf("child=%+v", out)
	})
	return out
}

func TestContinuableSubagentStopRestartBinary(t *testing.T) {
	for _, mode := range []string{"stop", "failed", "restart_completed", "restart_interrupted"} {
		t.Run(mode, func(t *testing.T) {
			const task = "LIFECYCLE_CHILD"
			const pending = "LIFECYCLE_OLD_QUEUE_MUST_NOT_WAKE"
			const resume = "LIFECYCLE_EXPLICIT_NEW_INPUT"
			initialHold := newContinuationHold(t)
			var effects string
			srv, script := startContinuationScript(t, func(w http.ResponseWriter, r *http.Request, req continuationRequest, caller string) bool {
				if caller == callerSubagent {
					if !req.text("user", task) {
						t.Error("continued child lost its original task")
					}
					assertContinuationToolPairs(t, req)
					if req.text("user", resume) {
						for _, message := range req.Messages {
							if message.Role == "user" && strings.Contains(message.Content, resume) && !strings.Contains(message.Content, "来自用户") {
								t.Errorf("direct authenticated HTTP input lost its owner source: %s", message.Content)
							}
						}
						if result, ok := req.result("lifecycle_before"); !ok || !strings.HasPrefix(result, "exit 0") {
							t.Error("explicit continuation lost the completed side-effect tool result")
						}
						if mode == "restart_completed" && !req.text("assistant", "LIFECYCLE_FIRST_END") {
							t.Error("cold completed-child continuation lost its earlier conclusion")
						}
						if result, ok := req.result("lifecycle_after"); !ok {
							streamTool(w, "lifecycle_after", "sh", argsJSON(map[string]string{
								"description": "Append only the explicitly requested new side effect", "command": "printf 'after\\n' >> " + shellQuote(effects),
							}))
						} else {
							if !strings.HasPrefix(result, "exit 0") {
								t.Errorf("new side effect failed: %s", result)
							}
							streamText(w, []string{"LIFECYCLE_SECOND_END"})
						}
						return true
					}
					if req.text("user", pending) {
						t.Error("stop/restart replayed an old accepted but unconsumed input")
						http.Error(w, "old input replayed", http.StatusBadRequest)
						return true
					}
					if result, ok := req.result("lifecycle_before"); !ok {
						streamTool(w, "lifecycle_before", "sh", argsJSON(map[string]string{
							"description": "Record a real side effect before stopping or restarting", "command": "printf 'before\\n' >> " + shellQuote(effects),
						}))
					} else {
						if !strings.HasPrefix(result, "exit 0") {
							t.Errorf("initial side effect failed: %s", result)
						}
						if mode == "failed" {
							http.Error(w, "scripted first-turn failure", http.StatusBadRequest)
							return true
						}
						if mode == "restart_completed" || initialHold.wait(r.Context()) {
							streamText(w, []string{"LIFECYCLE_FIRST_END"})
						}
					}
					return true
				}
				if caller != callerForeground {
					return false
				}
				text := lastUser(req.stub())
				switch {
				case strings.Contains(text, "LIFECYCLE_START"):
					if _, ok := req.result("lifecycle_dispatch"); !ok {
						streamTool(w, "lifecycle_dispatch", "dispatch", argsJSON(map[string]string{"type": "dev", "task": task}))
					} else {
						streamOwner(w, req.stub(), "Lifecycle child started.")
					}
					return true
				case strings.Contains(text, "LIFECYCLE_STOP"):
					if _, ok := req.result("lifecycle_stop"); !ok {
						id := continuationDispatchID(t, req, "lifecycle_dispatch")
						streamTool(w, "lifecycle_stop", "agent_stop", argsJSON(map[string]string{"id": id}))
					} else {
						streamOwner(w, req.stub(), "Requested stop of the existing collaborator.")
					}
					return true
				case strings.Contains(text, "LIFECYCLE_PROBE_1"), strings.Contains(text, "LIFECYCLE_PROBE_2"):
					streamOwner(w, req.stub(), "A separate parent wake completed without replaying child work.")
					return true
				}
				return false
			})
			t.Cleanup(initialHold.release)
			effects = filepath.Join(work.BotDir(srv.Root, "collaborator"), "effects.log")
			srv.Send("collaborator", "LIFECYCLE_START")
			var first continuationAgent
			if mode == "restart_completed" {
				first = waitContinuationChild(t, srv, task, "done")
				waitContinuationEnd(t, srv, first.ID, first.ResultPath, 1)
			} else if mode == "failed" {
				first = waitContinuationChild(t, srv, task, "failed")
			} else {
				awaitContinuationSignal(t, initialHold.entered, "child model after the completed side effect")
				first = waitContinuationChild(t, srv, task, "running")
				sendContinuationMessage(t, srv, first.ID, pending, "queue")
			}
			assertContinuationEffects(t, effects, "before\n")
			initialCalls := len(script.childRequests())
			if mode == "stop" {
				srv.Send("collaborator", "LIFECYCLE_STOP")
				awaitContinuationSignal(t, initialHold.cancelled, "stop to cancel the real active upstream request")
				waitContinuationChild(t, srv, task, "cancelled")
				for i := 0; i < 2; i++ {
					status, raw := srv.do(http.MethodPost, "/api/bots/collaborator/agents/"+first.ID+"/stop", map[string]any{})
					if status != http.StatusOK {
						t.Fatalf("idempotent HTTP stop: %d %s", status, raw)
					}
					var receipt continuationAgent
					if err := json.Unmarshal(raw, &receipt); err != nil || receipt.ID != first.ID || receipt.Status != "cancelled" {
						t.Fatalf("stop changed a terminal child's identity/state: %s, %v", raw, err)
					}
				}
			} else if mode != "failed" {
				if mode == "restart_interrupted" {
					// Kill this test-owned serve process at a known provider boundary.
					// The completed Shell invocation is not in flight and must not replay.
					if err := srv.cmd.Process.Kill(); err != nil {
						t.Fatal(err)
					}
					awaitContinuationSignal(t, srv.exited, "the original serve process to exit")
					awaitContinuationSignal(t, initialHold.cancelled, "the crashed child's upstream disconnect")
				}
				srv.Restart()
				if mode == "restart_interrupted" {
					waitContinuationChild(t, srv, task, "interrupted")
				}
			}
			// Two genuine parent wake boundaries replace an arbitrary sleep when
			// checking that recovery/stop did not schedule the old child input.
			srv.Send("collaborator", "LIFECYCLE_PROBE_1")
			srv.Send("collaborator", "LIFECYCLE_PROBE_2")
			if got := len(script.childRequests()); got != initialCalls {
				t.Fatalf("child ran without an explicit new input: before=%d after=%d", initialCalls, got)
			}
			assertContinuationEffects(t, effects, "before\n")
			sendContinuationMessage(t, srv, first.ID, resume, "")
			second := waitContinuationTurn(t, srv, first.ID, first.TurnSeq)
			if second.ID != first.ID {
				t.Fatalf("explicit continuation changed identity: %s -> %s", first.ID, second.ID)
			}
			assertContinuationEffects(t, effects, "before\nafter\n")
			endCount := 1
			if mode == "restart_completed" {
				endCount = 2
			}
			waitContinuationEnd(t, srv, first.ID, second.ResultPath, endCount)
			view := readContinuationTranscript(t, srv, first.ID)
			if !view.Continuable || view.TurnSeq != second.TurnSeq {
				t.Fatalf("restored child is not available through its original Web conversation: %+v", view)
			}
		})
	}
}

func assertContinuationEffects(t *testing.T, path, want string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != want {
		t.Fatalf("real tool side effects were lost or replayed: want=%q got=%q error=%v", want, raw, err)
	}
}
