package kernel

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/exec"
	"xbot/internal/inbox"
	"xbot/internal/link"
	"xbot/internal/notify"
	"xbot/internal/provider"
)

// This model repeats any result still present in its context, just as the real
// foreground did when B arrived after A had already been reported.
type repeatingReporter struct{ reqs []provider.Request }

func (p *repeatingReporter) Chat(_ context.Context, req provider.Request, _ func(string)) (provider.Message, error) {
	p.reqs = append(p.reqs, req)
	text := "新消息处理好了。"
	for _, m := range req.Messages {
		if strings.Contains(m.Content, "RESULT_ONCE_731010") {
			text += "结果是 RESULT_ONCE_731010。"
			break
		}
	}
	return provider.Message{Role: provider.RoleAssistant, Content: text}, nil
}

func TestSubagentResultIsOnlyReportedOnce(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	p := &repeatingReporter{}
	k := &Kernel{Bot: b, Provider: p, MaxSteps: 4}
	ev := link.NewEvent(link.SourceSubagent, exec.EvDone, "RESULT_ONCE_731010", "sa_a", time.Now())
	for _, event := range []link.Event{ev, link.TerminalEvent("u1", "另做一件事", time.Now()), ev} {
		if err := k.Handle(context.Background(), event); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range entries {
		if e.Message.Role == provider.RoleAssistant && strings.Contains(e.Message.Content, "RESULT_ONCE_731010") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("result fully reported %d times, want 1", n)
	}
}

type interruptedReporter struct {
	started     chan struct{}
	interrupted bool
	requests    []provider.Request
}

type boundaryReporter struct {
	started chan string
	release chan struct{}
	calls   int
}

func (p *boundaryReporter) Chat(ctx context.Context, req provider.Request, _ func(string)) (provider.Message, error) {
	p.calls++
	if p.calls == 1 {
		p.started <- "initial"
		<-ctx.Done()
		return provider.Message{}, ctx.Err()
	}
	if p.calls == 2 {
		return provider.Message{Role: provider.RoleAssistant, Content: "handled request-1"}, nil
	}
	if p.calls == 3 {
		p.started <- "retry"
		select {
		case <-p.release:
		case <-ctx.Done():
			return provider.Message{}, ctx.Err()
		}
		return provider.Message{Role: provider.RoleAssistant, Content: "SILENT"}, nil
	}
	for _, m := range req.Messages {
		if strings.Contains(m.Content, "RESULT_BOUNDARY") {
			return provider.Message{Role: provider.RoleAssistant, Content: "RESULT_BOUNDARY 完成。"}, nil
		}
	}
	return provider.Message{Role: provider.RoleAssistant, Content: "handled request-2"}, nil
}

func TestFIFONextOwnerDoesNotInheritReportIdentityAtStepBoundary(t *testing.T) {
	b := testBot(t)
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	p := &boundaryReporter{started: make(chan string, 2), release: make(chan struct{})}
	k := &Kernel{Bot: b, Provider: p, MaxSteps: 2}
	ev := link.NewEvent(link.SourceSubagent, exec.EvDone, "RESULT_BOUNDARY", "sa_a", time.Now())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- k.Handle(ctx, ev) }()
	for i, want := range []string{"initial", "retry"} {
		select {
		case got := <-p.started:
			if got != want {
				t.Fatal(got)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		if !Interject(b, link.TerminalEvent("u1", fmt.Sprintf("request-%d", i+1), time.Now()), &Sink{Done: make(chan Outcome, 1)}) {
			t.Fatal("owner refused")
		}
	}
	close(p.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if e.Message.Content == "handled request-2" {
			found = true
			if len(e.Reports) > 0 {
				t.Fatal("new owner reply inherited an old result receipt")
			}
		}
	}
	if !found {
		t.Fatal("next owner request was replaced by an old result delivery")
	}
}

func (p *interruptedReporter) Chat(ctx context.Context, req provider.Request, delta func(string)) (provider.Message, error) {
	p.requests = append(p.requests, req)
	for _, m := range req.Messages {
		if strings.Contains(m.Content, "RESULT_ONCE_731010") {
			if !p.interrupted {
				p.interrupted = true
				if delta != nil {
					delta("半截转告")
				}
				close(p.started)
				<-ctx.Done()
				return provider.Message{}, ctx.Err()
			}
			return provider.Message{Role: provider.RoleAssistant, Content: "RESULT_ONCE_731010 已完成。"}, nil
		}
	}
	return provider.Message{Role: provider.RoleAssistant, Content: "新消息处理好了。"}, nil
}

func TestInterruptedReportDoesNotGetReceiptedByOwnerReply(t *testing.T) {
	b := testBot(t)
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	p := &interruptedReporter{started: make(chan struct{})}
	k := &Kernel{Bot: b, Provider: p, Inbox: box, MaxSteps: 6}
	ev := link.NewEvent(link.SourceSubagent, exec.EvDone, "RESULT_ONCE_731010", "sa_a", time.Now())
	if err := box.Post(ev); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- k.Handle(ctx, ev) }()
	select {
	case <-p.started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if !Interject(b, link.TerminalEvent("u1", "新消息", time.Now()), &Sink{Done: make(chan Outcome, 1)}) {
		t.Fatal("owner interjection refused")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	receipts, ownerReply := 0, 0
	for _, e := range entries {
		if e.Message.Content == "新消息处理好了。" {
			ownerReply++
			if len(e.Reports) != 0 {
				t.Fatal("owner reply signed for interrupted result")
			}
		}
		if len(e.Reports) > 0 {
			receipts++
			if e.Message.Content != "RESULT_ONCE_731010 已完成。" {
				t.Fatal("receipt on incomplete report", e)
			}
		}
	}
	if receipts != 1 || ownerReply != 1 {
		t.Fatalf("receipts=%d owner replies=%d", receipts, ownerReply)
	}
	if len(box.Pending()) != 0 {
		t.Fatal("committed result not acknowledged")
	}
	// A process restart recovers receipts even when all the original lines are
	// covered by a summary. Replayed inbox events must not call the model.
	st, err := b.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	st.Blocks = []bot.SummaryBlock{{From: 1, To: len(entries), Text: "旧结果已处理。"}}
	if err := b.SaveState(st); err != nil {
		t.Fatal(err)
	}
	restarted := &Kernel{Bot: b, Provider: &scripted{}}
	if err := restarted.Handle(ctx, ev); err != nil {
		t.Fatal(err)
	}
	pending, err := restarted.pendingReports()
	if err != nil || len(pending) != 0 {
		t.Fatalf("restart pending=%v err=%v", pending, err)
	}
	projected, err := restarted.compactReportHistory(entries)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range projected {
		if strings.Contains(m.Content, "RESULT_ONCE_731010") {
			t.Fatal("compression reintroduces delivered result")
		}
	}
}

func TestPendingReportSurvivesCompactionAndRestart(t *testing.T) {
	b := testBot(t)
	ev := link.NewEvent(link.SourceSubagent, exec.EvDone, "RESULT_ONCE_731010", "sa_a", time.Now())
	if err := b.Append(bot.Entry{Time: ev.Time, EventID: ev.ID, Report: true, Origin: bot.OriginOwner, Message: provider.Message{Role: provider.RoleUser, Content: link.Render(ev, false)}}); err != nil {
		t.Fatal(err)
	}
	if err := b.SaveState(bot.State{Blocks: []bot.SummaryBlock{{From: 1, To: 1, Text: "后台结果等待转告。"}}}); err != nil {
		t.Fatal(err)
	}
	p := &repeatingReporter{}
	k := &Kernel{Bot: b, Provider: p, MaxSteps: 3}
	if err := k.Handle(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if !reportDelivered(entries, ev.ID) || !strings.Contains(entries[len(entries)-1].Message.Content, "RESULT_ONCE_731010") {
		t.Fatal("compacted pending result lost", entries)
	}
}

func TestAbsorbedResultsHaveSeparateDurableReceipts(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	a := link.NewEvent(link.SourceSubagent, exec.EvDone, "RESULT_A_ONCE", "sa_a", time.Now())
	bResult := link.NewEvent(link.SourceSubagent, exec.EvDone, "RESULT_B_ONCE", "sa_b", time.Now())
	p := &hookProvider{scripted: &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "links1", Type: "function", Function: provider.FunctionCall{Name: "links", Arguments: `{"action":"list"}`}}}},
		{Role: provider.RoleAssistant, Content: "主人的消息处理好了。"},
		{Role: provider.RoleAssistant, Content: "RESULT_A_ONCE 完成。"},
		{Role: provider.RoleAssistant, Content: "RESULT_B_ONCE 完成。"},
	}}, hook: func() {
		for _, ev := range []link.Event{a, bResult} {
			if err := box.Post(ev); err != nil {
				t.Error(err)
			}
		}
	}}
	k := &Kernel{Bot: b, Inbox: box, Provider: p, MaxSteps: 8}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "看一下链接", time.Now())); err != nil {
		t.Fatal(err)
	}
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range []link.Event{a, bResult} {
		events, receipts := 0, 0
		for _, e := range entries {
			if e.EventID == ev.ID && e.Report {
				events++
			}
			for _, id := range e.Reports {
				if id == ev.ID {
					receipts++
				}
			}
		}
		if events != 1 || receipts != 1 {
			t.Fatalf("%s events=%d receipts=%d", ev.ID, events, receipts)
		}
	}
	if len(box.Pending()) != 0 {
		t.Fatal("absorbed results not acknowledged")
	}
	// B's delivery no longer carries A's raw result or full report.
	for _, m := range p.reqs[len(p.reqs)-1].Messages {
		if strings.Contains(m.Content, "RESULT_A_ONCE") {
			t.Fatal("A reintroduced during B delivery")
		}
	}
}

func TestBackgroundWakeNotifiesEveryReceiptedResult(t *testing.T) {
	b := testBot(t)
	k := &Kernel{Bot: b, Desktop: func(context.Context, string, string) error { return nil }}
	k.turn = &turn{turnSource: link.NameTimer}
	// A previous explicit notification does not cover results arriving later.
	k.markNoted(context.Background(), "earlier note")
	produced := []provider.Message{
		{Role: provider.RoleAssistant, Content: "RESULT_A 已完成。", Reports: []string{"a"}},
		{Role: provider.RoleAssistant, Content: "RESULT_B 已完成。", Reports: []string{"b"}},
	}
	if err := k.pushFindings(context.Background(), produced); err != nil {
		t.Fatal(err)
	}
	notes, err := notify.Open(b.OutboxPath()).Unread()
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 || !strings.Contains(notes[0].Text, "RESULT_A") || !strings.Contains(notes[0].Text, "RESULT_B") {
		t.Fatalf("notified results=%+v", notes)
	}
}

func TestReportProjectionPreservesMessagePositionsAndOwnerText(t *testing.T) {
	input := []provider.Message{
		{Role: provider.RoleUser, Content: "<subagent_report>my own text</subagent_report>"},
		{Role: provider.RoleUser, Content: "result", ReportID: "r1"},
		{Role: provider.RoleAssistant, Content: "full report", Reports: []string{"r1"}},
	}
	got := reportProjection(input, nil, "")
	if len(got) != len(input) || got[0].Content != input[0].Content || got[1].Content == "result" || got[2].Content == "full report" {
		t.Fatal(got)
	}
	if input[1].Content != "result" {
		t.Fatal("projection changed transcript")
	}
}

func TestEmptyOrFailedReportDoesNotSignReceipt(t *testing.T) {
	for _, text := range []string{"", "SILENT"} {
		b := testBot(t)
		box, err := inbox.Open(b.InboxPath())
		if err != nil {
			t.Fatal(err)
		}
		k := &Kernel{Bot: b, Inbox: box, Provider: &scripted{resps: []provider.Message{{Content: text}}}, MaxSteps: 1}
		ev := link.NewEvent(link.SourceSubagent, exec.EvDone, "result", "sa_a", time.Now())
		if err := box.Post(ev); err != nil {
			t.Fatal(err)
		}
		if err := k.Handle(context.Background(), ev); err == nil {
			t.Fatal("unreported result treated as delivered")
		}
		entries, err := b.Entries()
		if err != nil {
			t.Fatal(err)
		}
		if reportDelivered(entries, ev.ID) || len(box.Pending()) != 1 {
			t.Fatal("empty report acknowledged")
		}
	}
}
