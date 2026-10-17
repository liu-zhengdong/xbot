package kernel

import (
	"context"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
)

func TestReminderCooldownAndReplay(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	t0 := time.Unix(5000, 0).UTC()
	appendCard(t, b, "card-1", "shell", "mkdir a", bot.ApprovalPending, t0.Add(-time.Minute))
	now := t0
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, Content: "first"},
		{Role: provider.RoleAssistant, Content: "second"},
	}}
	k := &Kernel{
		Bot: b, Provider: p, Memory: memory.NopStore{}, MaxSteps: 4,
		Links: []link.Link{link.Terminal{}},
		Now:   func() time.Time { return now },
	}
	ev1 := link.TerminalEvent("u1", "hello", t0.Add(time.Second))
	if err := k.Handle(context.Background(), ev1); err != nil {
		t.Fatal(err)
	}
	ev2 := link.TerminalEvent("u1", "again", t0.Add(2*time.Second))
	if err := k.Handle(context.Background(), ev2); err != nil {
		t.Fatal(err)
	}
	if len(p.reqs) != 2 {
		t.Fatalf("requests %d", len(p.reqs))
	}
	second := p.reqs[1].Messages
	if second[0].Content != p.reqs[0].Messages[0].Content {
		t.Fatal("system prefix changed between turns")
	}
	var replayed, fresh int
	for _, m := range second {
		if strings.Contains(m.Content, "hello") && strings.Contains(m.Content, "<pending_approvals>") {
			replayed++
		}
		if strings.Contains(m.Content, "again") && strings.Contains(m.Content, "<pending_approvals>") {
			fresh++
		}
	}
	if replayed != 1 {
		t.Fatalf("replayed reminder on the old message %d times", replayed)
	}
	if fresh != 0 {
		t.Fatal("cooldown did not suppress a second pending reminder")
	}
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Message.Content, "<pending_approvals>") {
			t.Fatal("sidecar reminder was written into the transcript")
		}
	}
	// Past the cooldown the same open card may fire again, on the newest message only.
	now = t0.Add(pendingCooldown + time.Second)
	p.resps = append(p.resps, provider.Message{Role: provider.RoleAssistant, Content: "third"})
	ev3 := link.TerminalEvent("u1", "later", now)
	if err := k.Handle(context.Background(), ev3); err != nil {
		t.Fatal(err)
	}
	last := p.reqs[2].Messages[len(p.reqs[2].Messages)-1]
	if !strings.Contains(last.Content, "later") || !strings.Contains(last.Content, "<pending_approvals>") {
		t.Fatalf("reminder after cooldown not on the newest message: %q", last.Content)
	}
}

func TestReminderPluginKinds(t *testing.T) {
	ch := newChannel("")
	now := time.Unix(10, 0).UTC()
	ch.Register(Kind{
		Name:     KindTodo,
		Cooldown: time.Minute,
		Trigger: func(time.Time, []Reminder) string {
			return "<todo>buy milk</todo>"
		},
	})
	ch.Register(Kind{
		Name:     KindFileChange,
		Cooldown: 0,
		Trigger: func(_ time.Time, prev []Reminder) string {
			if len(prev) > 0 {
				return ""
			}
			return "<file_change>notes.md</file_change>"
		},
	})
	due, err := ch.Due(now)
	if err != nil || len(due) != 2 {
		t.Fatalf("due %v %v", due, err)
	}
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "prefix"},
		{Role: provider.RoleUser, Content: "older"},
		{Role: provider.RoleUser, Content: "newest"},
	}
	out, err := ch.Bind(msgs, 3, now)
	if err != nil {
		t.Fatal(err)
	}
	if out[0].Content != "prefix" || out[1].Content != "older" {
		t.Fatal("attach rewrote history")
	}
	if !strings.Contains(out[2].Content, "newest") || !strings.Contains(out[2].Content, "<todo>") || !strings.Contains(out[2].Content, "<file_change>") {
		t.Fatalf("newest %q", out[2].Content)
	}
	again, err := ch.Due(now.Add(time.Second))
	if err != nil || len(again) != 0 {
		t.Fatalf("cooldown and one-shot trigger failed: %v %v", again, err)
	}
	later, err := ch.Due(now.Add(time.Minute + time.Second))
	if err != nil || len(later) != 1 || later[0].Kind != KindTodo {
		t.Fatalf("todo should fire again after cooldown: %v %v", later, err)
	}
}

func TestBackgroundDoneReminder(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(8000, 0).UTC()
	ended := now.Add(-time.Second)
	p := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: "told"}}}
	k := &Kernel{
		Bot: b, Provider: p, Memory: memory.NopStore{}, MaxSteps: 2,
		Links: []link.Link{link.Terminal{}},
		Now:   func() time.Time { return now },
		finished: []BackgroundInfo{{
			ID: "bg_abcd", Task: "rename the notes", Steps: 2, Status: "done", Ended: &ended,
		}},
	}
	ev := link.TerminalEvent("u1", "status?", now)
	if err := k.Handle(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	last := p.reqs[0].Messages[len(p.reqs[0].Messages)-1]
	if !strings.Contains(last.Content, "status?") || !strings.Contains(last.Content, "<background_done>") || !strings.Contains(last.Content, "bg_abcd") {
		t.Fatalf("completion not on the newest message: %q", last.Content)
	}
	p.resps = append(p.resps, provider.Message{Role: provider.RoleAssistant, Content: "again"})
	ev2 := link.TerminalEvent("u1", "and now?", now.Add(time.Second))
	if err := k.Handle(context.Background(), ev2); err != nil {
		t.Fatal(err)
	}
	var fresh int
	for _, m := range p.reqs[1].Messages {
		if strings.Contains(m.Content, "and now?") && strings.Contains(m.Content, "<background_done>") {
			fresh++
		}
	}
	if fresh != 0 {
		t.Fatal("background completion was reminded twice")
	}
}

func TestRetryGuidanceIsConditional(t *testing.T) {
	if strings.Contains(DefaultRules(), "try it once more") {
		t.Fatal("retry-once is still an always-on rule")
	}
	hint := loopHint(StopPollingFailure)
	if !strings.Contains(hint, "exact error") || !strings.Contains(hint, "Do not retry") {
		t.Fatalf("polling reminder lost the guidance: %q", hint)
	}
}

func BenchmarkReminderBind(b *testing.B) {
	ch := newChannel("")
	ch.Register(Kind{
		Name: KindTodo, Cooldown: time.Hour,
		Trigger: func(time.Time, []Reminder) string { return "<todo>one item</todo>" },
	})
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: strings.Repeat("s", 4096)},
		{Role: provider.RoleUser, Content: "hello"},
	}
	now := time.Unix(1, 0).UTC()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ch.log.notes = nil
		if _, err := ch.Bind(msgs, 2, now); err != nil {
			b.Fatal(err)
		}
	}
}
