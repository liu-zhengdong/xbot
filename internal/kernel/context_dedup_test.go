package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/inbox"
	"xbot/internal/link"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

func TestTodoReceiptsAndRemindersOnlyCarryChanges(t *testing.T) {
	items := make([]TodoItem, 8)
	for i := range items {
		items[i] = TodoItem{ID: fmt.Sprintf("t%d", i+1), Content: fmt.Sprintf("ITEM_%d_%s", i+1, strings.TrimSpace(strings.Repeat("task detail ", 8))), Status: "pending"}
	}
	args := func() string {
		raw, err := json.Marshal(map[string]any{"items": items})
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	first := args()
	items[0].Status = "done"
	second := args()
	items[1].Status = "done"
	third := args()
	p := &scripted{resps: []provider.Message{todoCall("create", first), todoCall("update", second), todoCall("update2", third), {Role: provider.RoleAssistant, Content: provider.TextContent("继续吗？")}}}
	k := &Kernel{Bot: testBot(t, link.NameTerminal), Provider: p, MaxSteps: 5}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "work", time.Now())); err != nil {
		t.Fatal(err)
	}
	var results []string
	for _, m := range p.reqs[len(p.reqs)-1].Messages {
		if m.Role == provider.RoleTool && m.Name == "todo" {
			results = append(results, m.Text())
		}
	}
	if len(results) != 3 {
		t.Fatalf("receipts: %v", results)
	}
	for i := 1; i < len(results); i++ {
		if len(results[i])*2 >= len(results[0]) || !strings.Contains(results[i], "8 items") || !strings.Contains(results[i], "pending -> done") || !strings.Contains(results[i], items[i-1].Content) {
			t.Fatalf("receipt repeated list: %s", results[i])
		}
		if strings.Contains(results[i], items[7].Content) {
			t.Fatal("unchanged content repeated")
		}
	}
	stored, err := ReadTodos(k.Bot)
	if err != nil || len(stored) != 8 || stored[0].Status != "done" || stored[1].Status != "done" || stored[7].Content != items[7].Content {
		t.Fatalf("incomplete disk list: %+v %v", stored, err)
	}
	tool := todoTool{k}
	unchanged, err := tool.Run(context.Background(), json.RawMessage(third))
	if err != nil || !strings.Contains(unchanged, "No changes.") || strings.Contains(unchanged, "ITEM_") {
		t.Fatalf("unchanged receipt: %s %v", unchanged, err)
	}
	reminder := todoDiffText(stored, nil)
	if !strings.Contains(reminder, "[pending] t8") || strings.Contains(reminder, "ITEM_") || strings.Contains(reminder, "[done] t1") {
		t.Fatalf("initial reminder repeated content: %s", reminder)
	}
	changed := append([]TodoItem(nil), stored...)
	changed[2].Status = "dropped"
	changed[2].Reason = "owner removed scope"
	diff := todoDiffText(changed, stored)
	if !strings.Contains(diff, "t3 "+changed[2].Content) || !strings.Contains(diff, "pending -> dropped (owner removed scope)") || strings.Contains(diff, stored[7].Content) || todoDiffText(stored, stored) != "" {
		t.Fatalf("bad reminder delta: %s", diff)
	}
	t.Logf("todo receipts: initial=%d update=%d update2=%d bytes; reminder=%d bytes\n%s", len(results[0]), len(results[1]), len(results[2]), len(reminder), results[1])
}

func TestForkCompletionCarriesPointerIntoMainRequest(t *testing.T) {
	for _, body := range []string{"SHORT_FORK_CONCLUSION", strings.Repeat("结论细节。", 90) + "FINAL_FACT_42"} {
		t.Run(fmt.Sprint(len(body)), func(t *testing.T) {
			b := testBot(t, link.NameTools)
			if err := b.Append(bot.Entry{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("task")}}); err != nil {
				t.Fatal(err)
			}
			id := "bg_dedup"
			if _, err := b.AppendFork(id, 1, "task"); err != nil {
				t.Fatal(err)
			}
			msg := provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent(body)}
			if err := b.Append(bot.Entry{Branch: id, Message: msg}); err != nil {
				t.Fatal(err)
			}
			box, err := inbox.Open(b.InboxPath())
			if err != nil {
				t.Fatal(err)
			}
			k := &Kernel{Bot: b, Inbox: box, Permissive: true, WorkDir: t.TempDir(), MaxSteps: 2}
			k.finishBackground(&turn{branch: id, task: "task", replies: 3}, []provider.Message{msg}, nil)
			pending := box.Pending()
			if len(pending) != 1 {
				t.Fatalf("events=%d", len(pending))
			}
			path := filepath.Join(b.AgentDir(id), "result.txt")
			if strings.Contains(pending[0].Content, body) || !strings.Contains(pending[0].Content, "done，3 步") || !strings.Contains(pending[0].Content, path) {
				t.Fatalf("duplicate conclusion: %s", pending[0].Content)
			}
			read := reviewedTool{Tool: &tools.ReadFile{Root: k.WorkDir}, k: k}
			args, _ := json.Marshal(map[string]any{"path": path})
			out, err := read.Run(context.Background(), args)
			if err != nil || !strings.Contains(out, body) {
				t.Fatalf("unreadable conclusion: %s %v", out, err)
			}
			p := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: provider.TextContent("收到结果指针")}}}
			k.Provider = p
			if err := k.Handle(context.Background(), pending[0]); err != nil {
				t.Fatal(err)
			}
			text := messagesText(p.reqs[0].Messages)
			if strings.Count(text, body) != 1 || !strings.Contains(text, path) {
				t.Fatalf("main request should retain the branch conclusion once, with a completion pointer: copies=%d", strings.Count(text, body))
			}
			t.Logf("fork body=%d completion=%d bytes\n%s", len(body), len(pending[0].Content), pending[0].Content)
		})
	}
}

func TestForkResultWriteFailureKeepsDiagnostics(t *testing.T) {
	b := testBot(t)
	id := "bg_save_failure"
	if err := os.MkdirAll(filepath.Join(b.AgentDir(id), "result.txt"), 0700); err != nil {
		t.Fatal(err)
	}
	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	k := &Kernel{Bot: b, Inbox: box}
	task := &turn{branch: id, replies: 2, stopReason: "background_budget", ctrl: []*Control{{Kind: "steer", State: "pending", Text: "UNREAD_CONTROL"}}}
	k.finishBackground(task, []provider.Message{{Role: provider.RoleAssistant, Content: provider.TextContent("SHORT_RESULT")}}, errors.New("ORIGINAL_ERROR"))
	pending := box.Pending()
	if len(pending) != 1 {
		t.Fatalf("events=%d", len(pending))
	}
	for _, want := range []string{"failed，2 步", "保存后台结果失败", "ORIGINAL_ERROR", "background_budget", "UNREAD_CONTROL"} {
		if !strings.Contains(pending[0].Content, want) {
			t.Fatalf("lost diagnostic %s: %s", want, pending[0].Content)
		}
	}
	if strings.Contains(pending[0].Content, "SHORT_RESULT") {
		t.Fatal("failed save duplicated conclusion")
	}
}

func TestForkCompletionBoundsUnreadControls(t *testing.T) {
	b := testBot(t)
	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	id := "bg_long_control"
	control := strings.Repeat("UNREAD_CONTROL_DETAIL\n", 1000) + "CONTROL_END_42"
	task := &turn{branch: id, replies: 3, ctrl: []*Control{{Kind: "steer", State: "pending", Text: control}}}
	k := &Kernel{Bot: b, Inbox: box}
	k.finishBackground(task, []provider.Message{{Role: provider.RoleAssistant, Content: provider.TextContent("SHORT_FINAL_RESULT")}}, nil)
	pending := box.Pending()
	path := filepath.Join(b.AgentDir(id), "result.txt")
	if len(pending) != 1 || len(pending[0].Content) > 4096 || strings.Contains(pending[0].Content, "SHORT_FINAL_RESULT") || strings.Contains(pending[0].Content, control) || !strings.Contains(pending[0].Content, path) {
		t.Fatalf("unbounded completion: %+v", pending)
	}
	if raw, err := os.ReadFile(path); err != nil || !strings.Contains(string(raw), control) || !strings.Contains(string(raw), "SHORT_FINAL_RESULT") {
		t.Fatalf("lost control or conclusion: %v", err)
	}
	if task.ctrl[0].State != "dropped" {
		t.Fatal("unread control was not marked dropped")
	}
}

func TestCreateBotPersonaOccursOnlyInArgumentsAndOwnPrefix(t *testing.T) {
	root := t.TempDir()
	primary, _, err := bot.EnsurePrimary(root, bot.Seed{Links: []string{link.NamePeers}, Owner: "u1"})
	if err != nil {
		t.Fatal(err)
	}
	persona := strings.Repeat("ROLE_BODY_专门整理项目资料。", 50)
	args, _ := json.Marshal(map[string]string{"name": "dedup-child", "persona": persona})
	p := &scripted{resps: []provider.Message{toolCall("create", "create_bot", string(args)), {Role: provider.RoleAssistant, Content: provider.TextContent("已创建")}}}
	k := &Kernel{Bot: primary, Provider: p, MaxSteps: 3, Links: []link.Link{link.Peers{}}}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "create", time.Now())); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, m := range p.reqs[1].Messages {
		if strings.Contains(m.Text(), persona) {
			t.Fatal("persona echoed in receipt or environment")
		}
		for _, call := range m.Calls() {
			if call.Function.Name == "create_bot" && call.Function.Arguments == string(args) {
				n++
			}
		}
	}
	if n != 1 {
		t.Fatalf("create arguments count=%d", n)
	}
	child, err := bot.Load(root, "dedup-child")
	if err != nil {
		t.Fatal(err)
	}
	stored, err := os.ReadFile(filepath.Join(bot.Dir(root, child.Name()), "persona.md"))
	if err != nil || strings.TrimSpace(string(stored)) != persona {
		t.Fatalf("persona not persisted: %v", err)
	}
	cp := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: provider.TextContent("ready")}}}
	ck := &Kernel{Bot: child, Provider: cp, MaxSteps: 2}
	if err := ck.Handle(context.Background(), link.TerminalEvent("u1", "start", time.Now())); err != nil {
		t.Fatal(err)
	}
	if strings.Count(messagesText(cp.reqs[0].Messages), persona) != 1 || !strings.Contains(cp.reqs[0].Messages[0].Text(), persona) || environmentGet[string](testPublished(t, ck), "persona") != "persona.md "+shortHash(persona) {
		t.Fatal("persona must occur only in own prefix; environment must carry its readable label and hash")
	}
}
