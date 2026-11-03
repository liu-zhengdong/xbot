package kernel

import (
	"context"
	"encoding/json"
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
	k := &Kernel{Bot: testBot(t, inbox.NameTerminal), Provider: p, MaxSteps: 5}
	if err := k.Handle(context.Background(), inbox.TerminalEvent("u1", "work", time.Now())); err != nil {
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

func TestCreateBotPersonaOccursOnlyInArgumentsAndOwnPrefix(t *testing.T) {
	root := t.TempDir()
	primary, _, err := bot.EnsurePrimary(root, bot.Seed{Links: []string{link.NamePeers}, Owner: "u1"})
	if err != nil {
		t.Fatal(err)
	}
	persona := strings.Repeat("ROLE_BODY_专门整理项目资料。", 50)
	args, _ := json.Marshal(map[string]string{"command": `"$XBOT_BIN" new dedup-child --self --persona '` + persona + `' --json`, "description": "创建资料助手"})
	p := &scripted{resps: []provider.Message{toolCall("create", "sh", string(args)), {Role: provider.RoleAssistant, Content: provider.TextContent("已创建")}}}
	k := &Kernel{Bot: primary, Provider: p, MaxSteps: 3, Links: []link.Link{link.Peers{}}}
	mountBotCreateSourceShell(t, k)
	if err := k.Handle(context.Background(), inbox.TerminalEvent("u1", "create", time.Now())); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, m := range p.reqs[1].Messages {
		if strings.Contains(m.Text(), persona) {
			t.Fatal("persona echoed in receipt or environment")
		}
		for _, call := range m.Calls() {
			if call.Function.Name == "sh" && call.Function.Arguments == string(args) {
				n++
			}
		}
	}
	if n != 1 {
		t.Fatalf("create arguments count=%d", n)
	}
	entries, err := primary.Entries()
	if err != nil {
		t.Fatal(err)
	}
	calls, results := 0, 0
	for _, entry := range entries {
		for _, call := range entry.Message.Calls() {
			if call.ID == "create" && call.Function.Name == "sh" && call.Function.Arguments == string(args) {
				calls++
			}
		}
		if entry.Message.Role == provider.RoleTool && entry.Message.CallID() == "create" && entry.Message.Name == "sh" {
			results++
			if strings.Contains(entry.Message.Text(), persona) || !strings.Contains(entry.Message.Text(), `"applied":true`) {
				t.Fatalf("create receipt repeated persona or lost effect: %s", entry.Message.Text())
			}
		}
	}
	if calls != 1 || results != 1 {
		t.Fatalf("raw history lost original Shell pairing: calls=%d results=%d", calls, results)
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
	if err := ck.Handle(context.Background(), inbox.TerminalEvent("u1", "start", time.Now())); err != nil {
		t.Fatal(err)
	}
	if strings.Count(messagesText(cp.reqs[0].Messages), persona) != 1 || !strings.Contains(cp.reqs[0].Messages[0].Text(), persona) || !strings.HasPrefix(environmentGet[string](testPublished(t, ck), "persona"), "职责 "+shortHash(persona)) {
		t.Fatal("persona must occur only in own prefix; environment must carry its readable label and hash")
	}
}
