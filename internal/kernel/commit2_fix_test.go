package kernel

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/compact"
	"xbot/internal/exec"
	"xbot/internal/inbox"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/notable"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

func TestNonPersonWakePreservesSeenPerson(t *testing.T) {
	for _, source := range []string{link.NameTimer, link.SourceSubagent, link.NameCoding, "webhook"} {
		t.Run(source, func(t *testing.T) {
			b := testBot(t, link.NameTerminal)
			if err := b.SetOwners([]string{"u1"}); err != nil {
				t.Fatal(err)
			}
			store := memory.Open(b.Root)
			ctx := context.Background()
			if err := store.Remember(ctx, memory.Fact{Scope: memory.ScopePerson, PersonID: "u1", Tier: memory.TierProfile, Key: "home", Text: "PERSON_BASELINE_52", WrittenBy: "u1"}); err != nil {
				t.Fatal(err)
			}
			p := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: provider.TextContent("one")}, {Role: provider.RoleAssistant, Content: provider.TextContent("wake")}, {Role: provider.RoleAssistant, Content: provider.TextContent("back")}}}
			now := time.Unix(1000, 0)
			k := &Kernel{Bot: b, Memory: store, Provider: p, MaxSteps: 2, Now: func() time.Time { return now }}
			if err := k.Handle(ctx, link.TerminalEvent("u1", "first", now)); err != nil {
				t.Fatal(err)
			}
			before := environmentGet[string](testPublished(t, k), "person:u1")
			count, err := b.Count()
			if err != nil {
				t.Fatal(err)
			}
			// Mirrors serve's timer: owner id used for routing, no SenderKind.
			ev := link.NewEvent(source, "wake", "wake", "u1", now)
			if err := k.Handle(ctx, ev); err != nil {
				t.Fatal(err)
			}
			_, err = b.LoadState()
			if err != nil {
				t.Fatal(err)
			}
			if environmentGet[string](testPublished(t, k), "person:u1") != before {
				t.Fatalf("%s erased seen person: before=%q after=%q", source, "PERSON_BASELINE_52", environmentGet[string](testPublished(t, k), "person:u1"))
			}
			end, err := b.Count()
			if err != nil {
				t.Fatal(err)
			}
			rows, err := b.Slice(count+1, end)
			if err != nil {
				t.Fatal(err)
			}
			var updates string
			for _, row := range rows {
				if strings.HasPrefix(row.Message.Text(), "<environment_") {
					updates += row.Message.Text()
				}
			}
			if !strings.Contains(updates, `"name":"`+source+`"`) || strings.Contains(updates, "person_data") {
				t.Fatalf("wrong source update: %s", updates)
			}
			if err := k.Handle(ctx, link.TerminalEvent("u1", "again", now)); err != nil {
				t.Fatal(err)
			}
			_, err = b.LoadState()
			if err != nil {
				t.Fatal(err)
			}
			if environmentGet[string](testPublished(t, k), "person:u1") != before {
				t.Fatal("unchanged person baseline changed on return")
			}
			end, err = b.Count()
			if err != nil {
				t.Fatal(err)
			}
			rows, err = b.Slice(count+1, end)
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range rows {
				text := row.Message.Text()
				if strings.Contains(text, "- [Person]") || strings.Contains(text, "+ [Person]") || strings.Contains(text, "PERSON_BASELINE_52") {
					t.Fatalf("unchanged profile re-emitted: %s", text)
				}
			}
			for _, req := range p.reqs {
				if req.Messages[1].Text() != p.reqs[0].Messages[1].Text() {
					t.Fatal("fixed prefix changed")
				}
			}
		})
	}
}

func TestForkLongReportKeepsNotableDigest(t *testing.T) {
	b := testBot(t)
	if err := b.Append(bot.Entry{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("start")}}); err != nil {
		t.Fatal(err)
	}
	id := "bg_notable_result"
	if _, err := b.AppendFork(id, 1, "long report"); err != nil {
		t.Fatal(err)
	}
	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	turn := &turn{branch: id, task: "long report", started: time.Now()}
	ctx := notable.With(context.Background(), "/work", &turn.notable)
	notable.Wrote(ctx, "/outside/notable-file.txt")
	want := notable.Line(turn.notable.Items())
	k := &Kernel{Bot: b, Inbox: box}
	k.finishBackground(turn, []provider.Message{{Role: provider.RoleAssistant, Content: provider.TextContent(strings.Repeat("long fact ", 1000) + "FULL_RESULT_TAIL_98")}}, nil)
	pending := box.Pending()
	if len(pending) != 1 {
		t.Fatalf("reports=%d", len(pending))
	}
	report := pending[0].Content
	if len(report) > 4096 || !strings.HasSuffix(report, want) || !strings.Contains(report, filepath.Join(b.AgentDir(id), "result.txt")) {
		t.Fatalf("lost digest or path: %s", report)
	}
	full, err := os.ReadFile(filepath.Join(b.AgentDir(id), "result.txt"))
	if err != nil || !strings.Contains(string(full), want) || !strings.Contains(string(full), "FULL_RESULT_TAIL_98") {
		t.Fatalf("full result lost: %v", err)
	}
}

func TestRecoveryUnreadableMetaDoesNotBlockBot(t *testing.T) {
	for _, kind := range []string{"empty", "invalid-json", "meta-is-directory", "read-only-dir"} {
		t.Run(kind, func(t *testing.T) {
			b := testBot(t, link.NameTerminal)
			box, err := inbox.Open(b.InboxPath())
			if err != nil {
				t.Fatal(err)
			}
			id := "sa_0123456789abcdef"
			dir := b.AgentDir(id)
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "meta.json")
			if kind == "invalid-json" {
				if err := os.WriteFile(path, []byte("{"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "meta-is-directory" {
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "read-only-dir" {
				if os.Geteuid() == 0 {
					t.Skip("root bypasses directory write permissions")
				}
				if err := os.WriteFile(path, []byte(`{"id":"sa_0123456789abcdef","status":"running"}`), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(dir, 0500); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(dir, 0700) })
			}

			st, err := b.LoadState()
			if err != nil {
				t.Fatal(err)
			}
			tasks := []bot.TaskEnv{{ID: id, Status: "running"}}
			if err := b.SaveState(st); err != nil {
				t.Fatal(err)
			}
			k := &Kernel{Bot: b, Inbox: box}
			if err := k.RecoverTasks(); err != nil {
				t.Fatalf("startup blocked on %s: %v", kind, err)
			}
			if kind != "meta-is-directory" && kind != "read-only-dir" {
				meta, err := exec.ReadMeta(dir)
				if err != nil || meta.Status != bot.ForkInterrupted || meta.ID != id {
					t.Fatalf("not interrupted: %+v %v", meta, err)
				}
			}
			pending := box.Pending()
			if len(pending) != 1 || pending[0].ID != "ev_"+id+"_interrupted" || !strings.Contains(pending[0].Content, "中断") {
				t.Fatalf("missing interruption: %+v", pending)
			}
			sampled, err := k.taskEnvironment(tasks, link.Event{})
			if err != nil {
				t.Fatal(err)
			}
			if len(sampled) != 1 || sampled[0].Status != bot.ForkInterrupted {
				t.Fatalf("environment kept stale task state: %+v", sampled)
			}
			if kind == "read-only-dir" {
				original, err := exec.ReadMeta(dir)
				if err != nil || original.Status != "running" {
					t.Fatalf("fixture did not retain the unwritable running meta: %+v %v", original, err)
				}
			}

			again := &Kernel{Bot: b, Inbox: box}
			if err := again.RecoverTasks(); err != nil {
				t.Fatal(err)
			}
			if len(box.Pending()) != 1 || inboxPostCount(t, b.InboxPath(), "ev_"+id+"_interrupted") != 1 {
				t.Fatal("restart duplicated interruption")
			}
			// The bot remains usable, even if the broken old directory cannot be saved.
			k.Provider = &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: provider.TextContent("still available")}, {Role: provider.RoleAssistant, Content: provider.TextContent("interruption noted")}}}
			k.MaxSteps = 3
			if err := k.Handle(context.Background(), link.TerminalEvent("u1", "hello", time.Now())); err != nil {
				t.Fatal(err)
			}
			rows, err := b.Entries()
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range rows {
				if strings.HasPrefix(row.Message.Text(), "<environment_") && strings.Contains(row.Message.Text(), id+" [running]") {
					t.Fatal("environment revived interrupted task from stale metadata")
				}
			}

		})
	}
}

func TestBotCompactToolMountedAndGuarded(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "atlas", "security.md"))
	if err != nil {
		t.Fatal(err)
	}
	documented := false
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "| `normal` |") && strings.Contains(line, "`compact`") {
			documented = true
		}
	}
	if !documented {
		t.Fatal("compact missing from the normal tool row")
	}
	b := testBot(t, link.NameContext)
	msgs := []bot.Entry{
		{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("the secret code is purple finch")}},
		{Message: provider.Message{Role: provider.RoleAssistant, Content: provider.ToolContent([]provider.ToolCall{{
			ID: "c1", Type: "function", Function: provider.FunctionCall{Name: "sh", Arguments: `{"command":"ls"}`},
		}})}},
		{Message: provider.Message{Role: provider.RoleTool, Content: provider.ToolResult("c1", "a.txt")}},
		{Message: provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("listed")}},
		{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("RECENT tail")}},
	}
	for _, e := range msgs {
		if err := b.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	mem := &factMem{}
	k := &Kernel{
		Bot: b, Links: []link.Link{link.Context{}}, Compactor: &recCompactor{}, Budget: compact.Budget{KeepTail: 1, Ceiling: 10},
		Memory: mem, Extractor: stubExtract{facts: []memory.Fact{{Scope: memory.ScopeBot, Tier: memory.TierLog, Text: "from the range"}}},
	}
	list, err := k.modelTools()
	if err != nil {
		t.Fatal(err)
	}
	var tool tools.Tool
	for _, x := range list {
		if x.Name() == "compact" {
			tool = x
		}
	}
	if tool == nil {
		t.Fatal("compact not offered with the Context grant")
	}
	if _, err := tool.Run(context.Background(), json.RawMessage(`{"from":2,"to":2}`)); err == nil {
		t.Fatal("compact split a tool-call pair")
	}
	if _, err := tool.Run(context.Background(), json.RawMessage(`{"from":5,"to":5}`)); err == nil {
		t.Fatal("compact took the raw tail")
	}
	if _, err := tool.Run(context.Background(), json.RawMessage(`{"from":1,"to":4,"focus":"keep the bird"}`)); err != nil {
		t.Fatal(err)
	}
	st, err := b.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Blocks) != 1 || st.Blocks[0].From != 1 || st.Blocks[0].To != 4 || st.Blocks[0].Focus != "keep the bird" {
		t.Fatalf("blocks=%+v", st.Blocks)
	}
	if mem.count() != 0 {
		t.Fatalf("compact wrote %d facts to memory", mem.count())
	}
}

func TestPersonBaselineReappearsAfterNonPersonCompaction(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	store := memory.Open(b.Root)
	ctx := context.Background()
	if err := store.Remember(ctx, memory.Fact{Scope: memory.ScopePerson, PersonID: "u1", Tier: memory.TierProfile, Key: "home", Text: "PERSON_AFTER_COMPACTION_18", WrittenBy: "u1"}); err != nil {
		t.Fatal(err)
	}
	p := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: provider.TextContent("one")}, {Role: provider.RoleAssistant, Content: provider.TextContent("wake")}, {Role: provider.RoleAssistant, Content: provider.TextContent("back")}}}
	k := &Kernel{Bot: b, Memory: store, Provider: p, Compactor: &recCompactor{}, MaxSteps: 2}
	k.Budget.KeepTail = 1
	if err := k.Handle(ctx, link.TerminalEvent("u1", "first", time.Now())); err != nil {
		t.Fatal(err)
	}
	if err := k.Handle(ctx, link.NewEvent(link.NameTimer, "wake", "wake", "u1", time.Now())); err != nil {
		t.Fatal(err)
	}
	n, err := b.Count()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := k.CompactRange(ctx, 1, n-1, "test summary does not retain profile"); err != nil {
		t.Fatal(err)
	}
	if err := k.Handle(ctx, link.TerminalEvent("u1", "again", time.Now())); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(messagesText(p.reqs[len(p.reqs)-1].Messages), "PERSON_AFTER_COMPACTION_18") {
		t.Fatal("person facts disappeared permanently after non-person compaction")
	}
}
