package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"xbot/internal/agenttype"
	"xbot/internal/bot"
	"xbot/internal/compact"
	"xbot/internal/config"
	"xbot/internal/exec"
	"xbot/internal/provider"
)

func TestContinuableKernelFollowupAndParentMessage(t *testing.T) {
	m := &roleModel{sub: []provider.Message{
		assistantCall("early", "", "send_message_to_parent", `{"text":"EARLY-FINDING"}`),
		{Role: provider.RoleAssistant, Content: provider.TextContent("FIRST-FACT")},
		{Role: provider.RoleAssistant, Content: provider.TextContent("SECOND-FACT")},
	}}
	k, b, box := dispatchKernel(t, m)
	task, err := k.StartSubagent(context.Background(), SubagentRequest{Type: "research", Task: "investigate"})
	if err != nil {
		t.Fatal(err)
	}
	first := waitEvent(t, box, exec.EvDone)
	if err := box.Ack(first.ID); err != nil {
		t.Fatal(err)
	}
	early := waitEvent(t, box, "message")
	if early.Sender != task.ID || !strings.Contains(early.Content, "EARLY-FINDING") {
		t.Fatalf("wrong parent message: %+v", early)
	}
	old, err := exec.ReadMeta(b.AgentDir(task.ID))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (agentMessageTool{k}).Run(context.Background(), json.RawMessage(fmt.Sprintf(`{"id":%q,"text":"next feedback"}`, task.ID))); err != nil {
		t.Fatal(err)
	}
	second := waitEvent(t, box, exec.EvDone)
	next, err := exec.ReadMeta(b.AgentDir(task.ID))
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID || next.TurnSeq == old.TurnSeq || next.ResultPath == old.ResultPath {
		t.Fatalf("reused round identity: old=%+v next=%+v", old, next)
	}
	if _, err := os.ReadFile(old.ResultPath); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	requests := append([]provider.Request(nil), m.subReq...)
	m.mu.Unlock()
	text := ""
	for _, msg := range requests[len(requests)-1].Messages {
		text += msg.Text() + "\n"
	}
	if !strings.Contains(text, "FIRST-FACT") || !strings.Contains(text, "来自 Bot "+b.Name()+" 的消息：") {
		t.Fatalf("second turn lost history or sender: %s", text)
	}
	rows, err := exec.ReadTranscript(b.AgentDir(task.ID), 0, 200)
	if err != nil {
		t.Fatal(err)
	}
	var sender string
	for _, row := range rows.Entries {
		if row.Message.Text() == "next feedback" {
			sender = row.Sender
		}
	}
	if sender != "bot:"+b.Name() {
		t.Fatalf("persisted sender = %q", sender)
	}
}

func TestContinuableForkExcludesWholeActiveTurn(t *testing.T) {
	k, b, _ := dispatchKernel(t, &roleModel{})
	appendLine := func(role, text string) int {
		t.Helper()
		seq, err := b.AppendSeq(bot.Entry{Message: provider.Message{Role: role, Content: provider.TextContent(text)}})
		if err != nil {
			t.Fatal(err)
		}
		return seq
	}
	appendLine(provider.RoleUser, "OLD-QUESTION")
	appendLine(provider.RoleAssistant, "COMPLETED-FACT")
	opening := appendLine(provider.RoleUser, "CURRENT-INPUT")
	appendLine(provider.RoleAssistant, "CURRENT-EARLY-ANSWER")
	appendLine(provider.RoleUser, "CURRENT-INTERJECTION")
	if err := b.Append(bot.Entry{Message: assistantCall("unfinished", "", "read_file", `{"path":"current"}`)}); err != nil {
		t.Fatal(err)
	}
	history, err := k.completedSubagentHistory(withTurnSeq(context.Background(), opening))
	if err != nil {
		t.Fatal(err)
	}
	text := ""
	for _, m := range history {
		text += m.Text()
		if len(m.Calls()) > 0 {
			t.Fatal("copied active tool call")
		}
	}
	if !strings.Contains(text, "COMPLETED-FACT") || strings.Contains(text, "CURRENT-") {
		t.Fatalf("wrong fork boundary: %s", text)
	}
	if _, err := k.completedSubagentHistory(context.Background()); err == nil {
		t.Fatal("fork guessed boundary without active turn")
	}
}

func TestContinuableChildToolsAndSelfRead(t *testing.T) {
	k, _, _ := dispatchKernel(t, &roleModel{})
	for _, ty := range agenttype.Builtin() {
		t.Run(ty.Name, func(t *testing.T) {
			req := exec.StartRequest{ID: "sa_12345678", Descriptor: &exec.Descriptor{Tools: ty.Tools}}
			list := k.toolsFor(req)
			names := map[string]bool{}
			for _, tool := range list {
				names[tool.Name()] = true
			}
			if !names["send_message_to_parent"] {
				t.Fatal("cannot send to parent")
			}
			for _, name := range []string{"dispatch", "spawn", "agent_message", "agent_stop", "list_subagents", "read_subagent", "expand_subagent", "steer", "queue", "interrupt", "cancel", "send_message_to_user"} {
				if names[name] {
					t.Errorf("child exposes %s", name)
				}
			}
		})
	}
}

func TestContinuableExplicitRouteOverridesInjectedParent(t *testing.T) {
	var mu sync.Mutex
	var models []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model           string `json:"model"`
			ReasoningEffort string `json:"reasoning_effort"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		mu.Lock()
		models = append(models, req.Model)
		mu.Unlock()
		if req.Model != "child-model" || req.ReasoningEffort != "high" {
			t.Errorf("wrong route/effort: %+v", req)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"child-result\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	t.Setenv("CONTINUABLE_FAKE_KEY", "scripted-only")
	parent := &roleModel{}
	k, b, box := dispatchKernel(t, parent)
	if err := config.Add(b.Root, config.Provider{Name: "fixture", Protocol: config.ChatCompletions, BaseURL: server.URL, APIKeyEnv: "CONTINUABLE_FAKE_KEY"}, config.ModelSpec{Name: "child-model", Context: 20000, Thinking: "on"}); err != nil {
		t.Fatal(err)
	}
	before := b.Config()
	task, err := k.StartSubagent(context.Background(), SubagentRequest{Type: "research", Task: "one", Provider: "fixture", Model: "child-model", ReasoningEffort: "high"})
	if err != nil {
		t.Fatal(err)
	}
	first := waitEvent(t, box, exec.EvDone)
	if err := box.Ack(first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := k.MessageSubagent(context.Background(), task.ID, "two", ""); err != nil {
		t.Fatal(err)
	}
	waitEvent(t, box, exec.EvDone)
	d, err := exec.ReadDescriptor(b.AgentDir(task.ID))
	if err != nil {
		t.Fatal(err)
	}
	if d.Provider != "fixture" || d.Model != "child-model" || d.ReasoningEffort != "high" {
		t.Fatalf("wrong persisted route: %+v", d)
	}
	raw, err := os.ReadFile(b.AgentDir(task.ID) + "/descriptor.json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "scripted-only") || strings.Contains(string(raw), "api_key") {
		t.Fatal("descriptor persisted credentials")
	}
	after := b.Config()
	if after.Provider != before.Provider || after.Model != before.Model {
		t.Fatal("changed parent binding")
	}
	parent.mu.Lock()
	calls := len(parent.subReq)
	parent.mu.Unlock()
	if calls != 0 {
		t.Fatal("parent fake overrode explicit child route")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(models) != 2 {
		t.Fatalf("requests: %v", models)
	}
	rows, err := exec.ReadTranscript(b.AgentDir(task.ID), 0, 200)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows.Entries {
		if row.Message.Text() == "two" && row.Sender != "owner" {
			t.Fatalf("Web domain message impersonated bot: %+v", row)
		}
	}
}

func TestContinuableFitUsesPrivateWindowAndFailsBeforeOversizeCall(t *testing.T) {
	parent := &roleModel{}
	k, _, _ := dispatchKernel(t, parent)
	k.Budget = compact.Budget{Target: 100000, Ceiling: 200000}
	f := newTurnFit(1, nil)
	f.budget = &subagentBudget{target: 100, ceiling: 200, ratio: 1}
	p := &fitProvider{k: k, inner: parent, fit: f}
	req := provider.Request{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: provider.TextContent("child")},
		{Role: provider.RoleUser, Content: provider.TextContent(strings.Repeat("x", 2000))},
	}}
	if _, err := p.Chat(context.Background(), req, nil); err == nil {
		t.Fatal("sent oversized child request")
	}
	if k.Budget.Ceiling != 200000 || k.scaled(100) != 100 {
		t.Fatal("child changed parent budget/calibration")
	}
	parent.mu.Lock()
	defer parent.mu.Unlock()
	if len(parent.foreReq)+len(parent.subReq) != 0 {
		t.Fatal("called model beyond child ceiling")
	}
}
