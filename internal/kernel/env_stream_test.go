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
	"xbot/internal/code"
	"xbot/internal/exec"
	"xbot/internal/inbox"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

func TestEnvStreamBotPrefixAndPersonData(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	if err := b.SetOwners([]string{"u1", "u2"}); err != nil {
		t.Fatal(err)
	}
	store := memory.Open(b.Root)
	ctx := context.Background()
	for _, f := range []memory.Fact{
		{Scope: memory.ScopeBot, BotName: b.Name(), Tier: memory.TierProfile, Key: "project", Text: "BOT_FIXED", WrittenBy: b.Name()},
		{Scope: memory.ScopePerson, PersonID: "u1", Tier: memory.TierProfile, Key: "home", Text: "PERSON_ONE", WrittenBy: "u1"},
		{Scope: memory.ScopePerson, PersonID: "u2", Tier: memory.TierProfile, Key: "home", Text: "PERSON_TWO", WrittenBy: "u2"},
	} {
		if err := store.Remember(ctx, f); err != nil {
			t.Fatal(err)
		}
	}
	p := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: "one"}, {Role: provider.RoleAssistant, Content: "two"}, {Role: provider.RoleAssistant, Content: "report"}, {Role: provider.RoleAssistant, Content: "back"}}}
	k := &Kernel{Bot: b, Provider: p, Memory: store, MaxSteps: 2}
	events := []link.Event{link.TerminalEvent("u1", "hello1", time.Now()), link.TerminalEvent("u2", "hello2", time.Now()), link.NewEvent(link.SourceSubagent, exec.EvDone, "RESULT_77", "sa_0123456789abcdef", time.Now()), link.TerminalEvent("u1", "hello3", time.Now())}
	for _, e := range events {
		if err := k.Handle(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	prefix := p.reqs[0].Messages[1].Content
	if !strings.Contains(prefix, "BOT_FIXED") || strings.Contains(prefix, "PERSON_") {
		t.Fatalf("wrong prefix %s", prefix)
	}
	for _, req := range p.reqs {
		if req.Messages[1].Content != prefix {
			t.Fatal("person switched fixed prefix")
		}
	}
	for i, want := range []string{"PERSON_ONE", "PERSON_TWO", "RESULT_77", "PERSON_ONE"} {
		if !strings.Contains(messagesText(p.reqs[i].Messages), want) {
			t.Fatalf("request %d missing %s", i, want)
		}
	}
	st, err := b.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if st.Env.Sender != "u1" || !strings.Contains(st.Env.Person, "PERSON_ONE") {
		t.Fatalf("person environment %+v", st.Env)
	}
	// Same-person edits change the stream without rewriting the Bot prefix.
	if err := store.Remember(ctx, memory.Fact{Scope: memory.ScopePerson, PersonID: "u1", Tier: memory.TierProfile, Key: "extra", Text: "PERSON_NEW", WrittenBy: "other"}); err != nil {
		t.Fatal(err)
	}
	oldSnap := st.MemSnap
	seq, err := k.receive(link.TerminalEvent("u1", "another", time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	st, err = b.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if st.Env.SenderSeq != seq || !strings.Contains(st.Env.Person, "PERSON_NEW") || st.MemSnap != oldSnap {
		t.Fatal("missing updated person or lost bot snapshot")
	}
	data, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(data[len(data)-1].Message.Content, "PERSON_NEW") {
		t.Fatal("person edit not appended")
	}
	reopened, err := bot.Load(b.Root, b.Name())
	if err != nil {
		t.Fatal(err)
	}
	after := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: "after restart"}}}
	restarted := &Kernel{Bot: reopened, Memory: store, Provider: after, MaxSteps: 2}
	if err := restarted.Handle(ctx, link.TerminalEvent("u2", "restart and switch", time.Now())); err != nil {
		t.Fatal(err)
	}
	if after.reqs[0].Messages[1].Content != prefix || !strings.Contains(messagesText(after.reqs[0].Messages), "PERSON_TWO") {
		t.Fatal("restart changed prefix or lost sender facts")
	}

}

func TestEnvStreamInboxAfterNormalReply(t *testing.T) {
	b := testBot(t, link.NameTerminal, link.NameCoding)
	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	k := &Kernel{Bot: b, Inbox: box, MaxSteps: 3}
	ev := link.NewEvent(link.SourceSubagent, exec.EvDone, "RESULT_LAST_89", "sa_0123456789abcdef", time.Now())
	k.Provider = providerFunc(func(_ context.Context, req provider.Request, _ func(string)) (provider.Message, error) {
		calls++
		if calls == 1 {
			if err := box.Post(ev); err != nil {
				t.Fatal(err)
			}
			return provider.Message{Role: provider.RoleAssistant, Content: "owner answered"}, nil
		}
		if !strings.Contains(messagesText(req.Messages), "RESULT_LAST_89") {
			t.Fatal("normal answer boundary lost report")
		}
		seq, err := b.FindEvent(ev.ID)
		if err != nil || seq == 0 {
			t.Fatal("report acknowledged before persistence")
		}
		return provider.Message{Role: provider.RoleAssistant, Content: "result is 89"}, nil
	})
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "hello", time.Now())); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(box.Pending()) != 0 {
		t.Fatalf("calls=%d pending=%d", calls, len(box.Pending()))
	}
}

func TestEnvStreamSilenceRules(t *testing.T) {
	for _, source := range []string{link.NameTerminal, link.SourceSubagent, link.NameTimer} {
		t.Run(source, func(t *testing.T) {
			b := testBot(t, link.NameTerminal)
			if err := b.SetOwners([]string{"u1"}); err != nil {
				t.Fatal(err)
			}
			ev := link.NewEvent(source, exec.EvDone, "input", "u1", time.Now())
			if source == link.NameTerminal {
				ev = link.TerminalEvent("u1", "do something", time.Now())
			}
			k := &Kernel{Bot: b, MaxSteps: 2, Provider: &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: "SILENT"}}}}
			err := k.Handle(context.Background(), ev)
			if source == link.NameTimer && err != nil {
				t.Fatal(err)
			}
			if source != link.NameTimer && err == nil {
				t.Fatal("SILENT swallowed required input")
			}
		})
	}
}

func TestEnvStreamNoManualCompact(t *testing.T) {
	b := testBot(t, link.NameContext)
	k := &Kernel{Bot: b, Links: []link.Link{link.Context{}}}
	list, err := k.modelTools()
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tool := range list {
		names[tool.Name()] = true
	}
	if names["compact"] || !names["recall_history"] || !names["expand"] {
		t.Fatalf("tools=%v", names)
	}
}

func TestEnvStreamCancelledCallIsCounted(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	k := &Kernel{Bot: b, MaxSteps: 2, Provider: providerFunc(func(context.Context, provider.Request, func(string)) (provider.Message, error) {
		return provider.Message{}, context.Canceled
	})}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "hi", time.Now())); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	snap := k.ContextSnapshot()
	if len(snap.Requests) != 1 || snap.Requests[0].Status != "cancelled" {
		t.Fatalf("snapshot %+v", snap.Requests)
	}
	trace, err := b.Traces()
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, tr := range trace {
		if tr.Kind == bot.TraceUsage {
			n++
			if tr.Status != "cancelled" || !tr.Estimated || tr.Prompt == 0 {
				t.Fatalf("usage %+v", tr)
			}
		}
	}
	if n != 1 {
		t.Fatalf("cancelled usage calls=%d", n)
	}
}

type streamTool struct{ run func() }

func (streamTool) Name() string                                  { return "read_file" }
func (streamTool) Description() string                           { return "read" }
func (streamTool) Parameters() any                               { return map[string]any{"type": "object"} }
func (streamTool) Confirm(json.RawMessage) (bool, string, error) { return false, "read", nil }
func (t streamTool) Run(context.Context, json.RawMessage) (string, error) {
	t.run()
	return "TOOL_RESULT_123", nil
}

type streamLink struct{ tool tools.Tool }

func (streamLink) Name() string          { return link.NameTools }
func (l streamLink) Tools() []tools.Tool { return []tools.Tool{l.tool} }

func TestEnvStreamLiveAndReassembledToolOrder(t *testing.T) {
	b := testBot(t, link.NameTerminal, link.NameTools)
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	k := &Kernel{Bot: b, MaxSteps: 4, Now: func() time.Time { return time.Unix(10, 0).UTC() }}
	later := link.TerminalEvent("u1", "B_DURING_TOOL", time.Unix(10, 0))
	k.Links = []link.Link{link.Terminal{}, streamLink{streamTool{run: func() {
		if !Interject(b, later, &Sink{Done: make(chan Outcome, 1)}) {
			t.Fatal("interject not accepted")
		}
	}}}}
	calls := 0
	k.Provider = providerFunc(func(_ context.Context, req provider.Request, _ func(string)) (provider.Message, error) {
		calls++
		if calls == 1 {
			return toolCall("read1", "read_file", "{}"), nil
		}
		st, err := b.LoadState()
		if err != nil {
			t.Fatal(err)
		}
		replay, _, err := k.assemble(st, st.MemSnap)
		if err != nil {
			t.Fatal(err)
		}
		// Provider message bytes, not internal sequence metadata, are the cache contract.
		normalize := func(msgs []provider.Message) string {
			var out []string
			for _, m := range msgs {
				v := struct {
					Role, Content, Call string
					Calls               []provider.ToolCall
				}{m.Role, m.Content, m.ToolCallID, m.ToolCalls}
				raw, _ := json.Marshal(v)
				out = append(out, string(raw))
			}
			return strings.Join(out, "\n")
		}
		if normalize(req.Messages) != normalize(replay) {
			t.Fatalf("live/replay differ\nLIVE %s\nREPLAY %s", normalize(req.Messages), normalize(replay))
		}
		atCall, atResult, atB := -1, -1, -1
		for i, m := range req.Messages {
			if len(m.ToolCalls) > 0 {
				atCall = i
			}
			if m.Role == provider.RoleTool {
				atResult = i
			}
			if m.Content == later.Content {
				atB = i
			}
		}
		if atResult != atCall+1 || atB <= atResult {
			t.Fatalf("order call/result/B %d/%d/%d", atCall, atResult, atB)
		}
		return provider.Message{Role: provider.RoleAssistant, Content: "both handled"}, nil
	})
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "A", time.Unix(10, 0))); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestEnvStreamTaskRecoveryAndCodingState(t *testing.T) {
	b := testBot(t, link.NameTerminal, link.NameCoding)
	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	id := "sa_0123456789abcdef"
	dir := b.AgentDir(id)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(exec.Meta{ID: id, Task: "old task", Status: "queued"})
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	coding := code.Open(bot.Dir(b.Root, b.Name()), b.InboxPath())
	task, err := coding.Record().Start(code.Task{WorkDir: t.TempDir(), Goal: "coding work", MaxSteps: 2, TimeoutSec: 10})
	if err != nil {
		t.Fatal(err)
	}
	k := &Kernel{Bot: b, Inbox: box, Links: []link.Link{coding}}
	state, err := k.taskEnvironment(nil, link.Event{})
	if err != nil {
		t.Fatal(err)
	}
	if len(state) != 1 || state[0].ID != task.ID {
		t.Fatalf("coding task missing %+v", state)
	}
	if err := k.RecoverTasks(); err != nil {
		t.Fatal(err)
	}
	meta, err := exec.ReadMeta(dir)
	if err != nil || meta.Status != bot.ForkInterrupted {
		t.Fatalf("meta %+v %v", meta, err)
	}
	ca, err := coding.Record().Get(task.ID)
	if err != nil || ca.Status != code.StatusInterrupted {
		t.Fatalf("coding %+v %v", ca, err)
	}
	if err := box.Reload(); err != nil {
		t.Fatal(err)
	}
	if len(box.Pending()) != 2 {
		t.Fatalf("recovery reports %d", len(box.Pending()))
	}
	for i := 0; i < 2; i++ {
		fresh := &Kernel{Bot: b, Inbox: box, Links: []link.Link{coding}}
		if err := fresh.RecoverTasks(); err != nil {
			t.Fatal(err)
		}
	}
	if err := box.Reload(); err != nil {
		t.Fatal(err)
	}
	if len(box.Pending()) != 2 {
		t.Fatal("restart duplicated interruption report")
	}
}

func TestEnvStreamResultFileIsReadable(t *testing.T) {
	b := testBot(t, link.NameTools)
	text := strings.Repeat("detail\n", 1800) + "FINAL_NUMBER_8128"
	dir := b.AgentDir("sa_0123456789abcdef")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	report, err := exec.ResultText(dir, text, "")
	if err != nil {
		t.Fatal(err)
	}
	k := &Kernel{Bot: b, WorkDir: t.TempDir(), Permissive: true}
	read := reviewedTool{Tool: &tools.ReadFile{Root: k.WorkDir}, k: k}
	args := fmt.Sprintf(`{"path":%q,"offset":1801,"limit":5}`, filepath.Join(dir, "result.txt"))
	out, err := read.Run(context.Background(), json.RawMessage(args))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(report, filepath.Join(dir, "result.txt")) || !strings.Contains(out, "FINAL_NUMBER_8128") {
		t.Fatalf("unreadable report %s output %s", report, out)
	}
}

func TestRecoveryDoesNotInterruptAnotherLiveKernel(t *testing.T) {
	b := testBot(t, link.NameAgents)
	hold := make(chan struct{})
	loc := exec.NewLocal(exec.Limits{Timeout: time.Second * 10})
	loc.Prov = &roleModel{hold: hold}
	id := "sa_0123456789abcdef"
	if _, err := loc.Start(context.Background(), exec.StartRequest{ID: id, Mode: exec.ModeBrief, Task: "held", Dir: b.AgentDir(id)}); err != nil {
		t.Fatal(err)
	}
	defer close(hold)
	end := time.Now().Add(time.Second)
	for {
		m, err := exec.ReadMeta(b.AgentDir(id))
		if err == nil && m.PID > 0 {
			break
		}
		if time.Now().After(end) {
			t.Fatal("no live metadata")
		}
		time.Sleep(time.Millisecond)
	}
	fresh := &Kernel{Bot: b}
	if err := fresh.RecoverTasks(); err != nil {
		t.Fatal(err)
	}
	if fresh.execSnapshot() != nil {
		t.Fatal("recovery initialized an executor before web approval hooks; leaked a history observer")
	}
	m, err := exec.ReadMeta(b.AgentDir(id))
	if err != nil || m.Status != "running" {
		t.Fatalf("live task interrupted %+v %v", m, err)
	}
}

// The compactor must receive original task facts even when fit had already
// omitted a large result from a previous provider request.
type factCompactor struct{ saw bool }

func (c *factCompactor) Compact(_ context.Context, prior string, msgs []provider.Message) (string, error) {
	if !strings.Contains(prior, summaryFacts) {
		return "", errors.New("missing shared fact preservation rule")
	}
	text := messagesText(msgs)
	if !strings.Contains(text, "TASK_A=42") || !strings.Contains(text, "TASK_B=91") || !strings.Contains(text, "result.txt") {
		return "", errors.New("summary lost original task facts")
	}
	c.saw = true
	return "TASK_A=42; TASK_B=91; result.txt; owner already told A", nil
}
func TestEnvStreamSummaryKeepsOriginalTaskFacts(t *testing.T) {
	b := testBot(t)
	c := &factCompactor{}
	k := &Kernel{Bot: b, Compactor: c}
	msgs := []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "fact", Type: "function", Function: provider.FunctionCall{Name: "read_file", Arguments: "{}"}}}},
		{Role: provider.RoleTool, ToolCallID: "fact", Content: strings.Repeat("detail ", 600) + " TASK_A=42 TASK_B=91 /work/result.txt"},
		{Role: provider.RoleAssistant, Content: "owner already told A"},
	}
	f := newTurnFit(0, nil)
	f.omitted[1] = 0
	if err := k.foldTurn(context.Background(), f, msgs, 3); err != nil {
		t.Fatal(err)
	}
	if !c.saw || !strings.Contains(messagesText(f.view(msgs)), "TASK_B=91") {
		t.Fatal("fold did not keep facts")
	}
	c.saw = false
	entries := []bot.Entry{{Message: msgs[1]}, {Message: msgs[2]}}
	text, err := k.summarize(context.Background(), entries, nil, 1, 2, "")
	if err != nil || !c.saw || !strings.Contains(text, "TASK_A=42") {
		t.Fatalf("cross-turn summary %q %v", text, err)
	}
}

func TestForkLongResultWriteFailureIsFailed(t *testing.T) {
	b := testBot(t)
	if err := b.Append(bot.Entry{Message: provider.Message{Role: provider.RoleUser, Content: "start"}}); err != nil {
		t.Fatal(err)
	}
	id := "bg_result_failure"
	if _, err := b.AppendFork(id, 1, "long report"); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(b.AgentDir(id), "result.txt"), 0700); err != nil {
		t.Fatal(err)
	}
	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	k := &Kernel{Bot: b, Inbox: box}
	k.finishBackground(&turn{branch: id, task: "long report", started: time.Now()}, []provider.Message{{Role: provider.RoleAssistant, Content: strings.Repeat("result", 1000)}}, nil)
	status, _ := readMeta(t, b, id)
	if status != "failed" {
		t.Fatalf("saved result failed but task says %s", status)
	}
	pending := box.Pending()
	if len(pending) != 1 || !strings.Contains(pending[0].Content, "保存后台结果失败") {
		t.Fatalf("report %+v", pending)
	}
}

type boundedFactCompactor struct {
	calls   int
	sawTail bool
}

func (c *boundedFactCompactor) Compact(_ context.Context, prior string, msgs []provider.Message) (string, error) {
	text := messagesText(msgs)
	if len(prior)+len(text) > 20000 {
		return "", errors.New("summary request exceeded bounded batch")
	}
	c.calls++
	c.sawTail = c.sawTail || strings.Contains(text, "LAST_FACT_77")
	return "summary of preserved facts", nil
}
func TestTurnFactSummaryUsesBoundedBatches(t *testing.T) {
	b := testBot(t)
	c := &boundedFactCompactor{}
	k := &Kernel{Bot: b, Compactor: c}
	k.Budget.Ceiling = 10000
	var msgs []provider.Message
	for i := 0; i < 12; i++ {
		msgs = append(msgs, provider.Message{Role: provider.RoleTool, Content: strings.Repeat("detail ", 9000)})
	}
	msgs[len(msgs)-1].Content += "LAST_FACT_77"
	f := newTurnFit(0, nil)
	if err := k.foldTurn(context.Background(), f, msgs, len(msgs)); err != nil {
		t.Fatal(err)
	}
	if c.calls < 2 || !c.sawTail {
		t.Fatalf("batch calls %d tail %v", c.calls, c.sawTail)
	}
}
