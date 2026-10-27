package kernel

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/exec"
	"xbot/internal/inbox"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/notify"
	"xbot/internal/provider"
)

// Two completions are ordinary messages, not two special delivery requests.
// The next owner turn still contains both full results and the normal reply.
func TestSimultaneousResultsStayInNormalConversation(t *testing.T) {
	b := testBot(t)
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	in, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	a := link.NewEvent(link.SourceSubagent, exec.EvDone, "A结论731，限制数字42", "sa_a", time.Now())
	c := link.NewEvent(link.SourceSubagent, exec.EvDone, "B结论951，限制数字63", "sa_b", time.Now())
	for _, ev := range []link.Event{a, c} {
		if err := in.Post(ev); err != nil {
			t.Fatal(err)
		}
	}
	work := t.TempDir()
	path := filepath.Join(work, "evidence.txt")
	if err := os.WriteFile(path, []byte("real tool evidence 871"), 0600); err != nil {
		t.Fatal(err)
	}
	p := &scripted{resps: []provider.Message{toolCall("read", "read_file", mustArgs(t, map[string]string{"path": path})), {Role: provider.RoleAssistant, Content: provider.TextContent("A结论731，B结论951。")}, {Role: provider.RoleAssistant, Content: provider.TextContent("两个限制仍是42和63。")}}}
	k := &Kernel{Bot: b, Inbox: in, Provider: p, Memory: memory.NopStore{}, MaxSteps: 4, Links: []link.Link{link.Terminal{}, link.NewToolsLinkAt(work, "")}, WorkDir: work}
	if err := k.Handle(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	calls := len(p.reqs)
	for _, ev := range []link.Event{a, c} {
		if err := k.Handle(context.Background(), ev); err != nil {
			t.Fatal(err)
		}
	}
	if len(p.reqs) != calls {
		t.Fatal("replayed absorbed result started another generation")
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "限制是什么？", time.Now())); err != nil {
		t.Fatal(err)
	}
	req := p.reqs[len(p.reqs)-1]
	text := messagesText(req.Messages)
	// No result-receipt projection rewrites the already-sent prefix.
	first := p.reqs[0].Messages
	if len(req.Messages) < len(first) || !reflect.DeepEqual(first, req.Messages[:len(first)]) {
		for i := range first {
			if i >= len(req.Messages) || !reflect.DeepEqual(first[i], req.Messages[i]) {
				t.Fatalf("prefix %d changed:\nwas %+v\nnow %+v", i, first[i], req.Messages[i])
			}
		}
		t.Fatal("stable conversation prefix length changed")
	}
	for _, want := range []string{a.Content, c.Content, "A结论731，B结论951。"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing original %q: %s", want, text)
		}
	}
	for i, req := range p.reqs {
		if len(req.Tools) == 0 {
			t.Fatalf("request %d lost tools", i)
		}
	}
	rows, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	toolResults := 0
	for _, row := range rows {
		if row.Message.Role == provider.RoleTool && row.Message.CallID() == "read" && strings.Contains(row.Message.Text(), "real tool evidence 871") {
			toolResults++
		}
	}
	if toolResults != 1 {
		t.Fatalf("ordinary parsed tool did not run exactly once: %d", toolResults)
	}
	for _, ev := range []link.Event{a, c} {
		n := 0
		for _, row := range rows {
			if row.EventID == ev.ID {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("%s appended %d times", ev.ID, n)
		}
	}
	raw, _ := json.Marshal(rows)
	for _, bad := range []string{`"reports"`, `"report":`, `reported_result`, `subagent_report`, `deliver_result`} {
		if strings.Contains(string(raw), bad) {
			t.Fatalf("delivery mechanism survived: %s", bad)
		}
	}
}

func TestToolProtocolTextIsAnErrorNotAReply(t *testing.T) {
	for _, body := range []string{"<｜DSML｜function_calls><｜DSML｜invoke name=\"dispatch\">{}</｜DSML｜invoke>", `{"tool_calls":[{"function":{"name":"dispatch"}}]}`, "{\n \"tool_calls\": [{\"function\":{\"name\":\"dispatch\"}}]}", "reply_to: 1\n<｜DSML｜invoke name=\"dispatch\">bad</｜DSML｜invoke>"} {
		t.Run(body, func(t *testing.T) {
			b := testBot(t)
			p := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: provider.TextContent(body)}}}
			var stream strings.Builder
			k := &Kernel{Bot: b, Provider: p, MaxSteps: 2, OnDelta: func(s string) { stream.WriteString(s) }}
			ev := link.NewEvent(link.SourceSubagent, exec.EvDone, "A的完整结果123", "sa_a", time.Now())
			if err := k.Handle(context.Background(), ev); err == nil {
				t.Fatal("tool protocol accepted as a normal answer")
			}
			if stream.Len() != 0 {
				t.Fatalf("protocol leaked into stream: %q", stream.String())
			}
			rows, err := b.Entries()
			if err != nil {
				t.Fatal(err)
			}
			input := 0
			for _, row := range rows {
				if row.EventID == ev.ID {
					input++
				}
				if row.Message.Role == provider.RoleAssistant && strings.Contains(row.Message.Text(), body) {
					t.Fatal("protocol committed")
				}
			}
			if input != 1 {
				t.Fatalf("original result lost: %d", input)
			}
		})
	}
}

func TestReplyProviderRejectsSplitProtocolButKeepsUsage(t *testing.T) {
	p := providerFunc(func(_ context.Context, _ provider.Request, delta func(string)) (provider.Message, error) {
		delta("<｜D")
		delta("SML｜invoke name=\"dispatch\">bad</｜DSML｜invoke>")
		return provider.Message{Content: provider.TextContent("<｜DSML｜invoke name=\"dispatch\">bad</｜DSML｜invoke>")}, nil
	})
	used := 0
	var stream strings.Builder
	rp := &replyProvider{inner: p, onUse: func(provider.Request, provider.Message) { used++ }}
	if _, err := rp.Chat(context.Background(), provider.Request{}, func(s string) { stream.WriteString(s) }); err == nil {
		t.Fatal("accepted split protocol")
	}
	if stream.Len() != 0 || used != 1 {
		t.Fatalf("stream=%q usage=%d", stream.String(), used)
	}
}

type providerFunc func(context.Context, provider.Request, func(string)) (provider.Message, error)

func (f providerFunc) Chat(ctx context.Context, r provider.Request, d func(string)) (provider.Message, error) {
	return f(ctx, r, d)
}

func TestTaskEnvironmentOnlyAppendsDifferences(t *testing.T) {
	a := bot.TaskEnv{ID: "sa_a", Task: "任务A", Status: "running"}
	b := bot.TaskEnv{ID: "sa_b", Task: "任务B", Status: "queued", Position: 2}
	prev := environment{Fields: map[string]json.RawMessage{}}
	environmentSet(&prev, "task:"+a.ID, a)
	environmentSet(&prev, "task:"+b.ID, b)
	if got := environmentDelta(prev, prev, "", ""); got != "" {
		t.Fatal(got)
	}
	next := replayEnvironment([]provider.Message{{Role: provider.RoleUser, Internal: true, Content: provider.TextContent(environmentDelta(environment{Fields: map[string]json.RawMessage{}}, prev, "", ""))}}, "")
	b.Position = 1
	environmentSet(&next, "task:"+b.ID, b)
	diff := environmentDelta(prev, next, "", "")
	if strings.Contains(diff, "sa_a") || !strings.Contains(diff, `"position":1`) {
		t.Fatal(diff)
	}
	prev = next
	next = environment{Fields: map[string]json.RawMessage{}}
	for key, value := range prev.Fields {
		next.Fields[key] = value
	}
	a.Status = "done"
	environmentSet(&next, "task:"+a.ID, a)
	diff = environmentDelta(prev, next, "", "")
	if !strings.Contains(diff, `"status":"done"`) || strings.Contains(diff, "sa_b") {
		t.Fatal(diff)
	}
	if environmentDelta(next, next, "", "") != "" {
		t.Fatal("completion repeated")
	}

}

func TestDSMLExplanationIsOrdinaryText(t *testing.T) {
	for _, text := range []string{"DSML 的 invoke 是调用标签，不是普通回复。", "下面解释 DSML invoke：\n```xml\n<｜DSML｜invoke>example</｜DSML｜invoke>\n```"} {
		p := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: provider.TextContent(text)}}}
		rp := &replyProvider{inner: p}
		var stream strings.Builder
		msg, err := rp.Chat(context.Background(), provider.Request{}, func(s string) { stream.WriteString(s) })
		if err != nil || msg.Text() != text || stream.String() != text {
			t.Fatalf("ordinary explanation rejected: %q %v %q", msg.Text(), err, stream.String())
		}
	}
}
func TestLaterResultStillNotifiesAfterAnEarlierExplicitNotification(t *testing.T) {
	b := testBot(t, link.NameTerminal, link.NameTools, link.NameTimer)
	in, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	a := link.NewEvent(link.SourceSubagent, exec.EvDone, "A结论731", "sa_a", time.Now())
	c := link.NewEvent(link.SourceSubagent, exec.EvDone, "B结论951", "sa_b", time.Now())
	p := &scripted{resps: []provider.Message{toolCall("notify", "send_message_to_user", `{"text":"A结论731"}`), {Role: provider.RoleAssistant, Content: provider.TextContent("B结论951。")}}}
	posted := false
	k := &Kernel{Bot: b, Links: []link.Link{&link.Timer{Dir: b.RoutineDir(), Sender: "u1"}}, Inbox: in, Provider: p, MaxSteps: 3, Desktop: func(context.Context, string, string) error {
		if !posted {
			posted = true
			if err := in.Post(c); err != nil {
				t.Fatal(err)
			}
		}
		return nil
	}}
	if err := k.Handle(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	notes, err := notify.Open(b.OutboxPath()).Unread()
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 2 || notes[0].Text != "A结论731" || notes[1].Text != "B结论951。" {
		t.Fatalf("notifications=%+v", notes)
	}
}

func TestReceivedInputKeepsItsReminderSequence(t *testing.T) {
	b := testBot(t)
	k := &Kernel{Bot: b}
	ev := link.TerminalEvent("u1", "先收到的原话", time.Now())
	seq, err := k.receive(ev)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Append(bot.Entry{Message: provider.Message{Role: provider.RoleTool, Content: provider.ToolResult("old", "tool finished later")}}); err != nil {
		t.Fatal(err)
	}
	n, _ := b.Count()
	turn := &turn{lastRec: n}
	msg := k.eventMessage(ev)
	msg.Seq = seq
	got, err := k.recordTurn(turn, msg)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := b.Count()
	if got != seq || turn.lastSeq() != seq || after != n {
		t.Fatalf("seq=%d reminder=%d count=%d, want %d/%d", got, turn.lastSeq(), after, seq, n)
	}
}
