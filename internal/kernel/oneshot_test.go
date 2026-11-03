package kernel

import (
	"context"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/inbox"
	"xbot/internal/link"
	"xbot/internal/provider"
)

func TestOneShotAndLocalTime(t *testing.T) {
	b := testBot(t, inbox.NameTerminal, inbox.NameTimer)
	tz := "Asia/Shanghai"
	if err := b.ApplyProfile(bot.ProfileUpdate{Timezone: &tz}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 15, 12, 7, 40, 0, time.UTC)
	tm := &link.Timer{Dir: b.RoutineDir(), Sender: "u1"}
	p := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: provider.TextContent("ok")}}}
	k := &Kernel{
		Bot: b, Provider: p, Links: []link.Link{link.Terminal{}, tm},
		Now: func() time.Time { return now }, MaxSteps: 2,
	}
	at := now.Add(2 * time.Minute)
	out, err := k.RoutineHandlers().Create(context.Background(), cliops.RoutineCreate{
		Name: "water", Prompt: "drink", At: at.Format(time.RFC3339),
	})
	if err != nil || !out.Applied || out.Stage != "complete" || out.ID == "" || out.Error != "" {
		t.Fatalf("create %+v: %v", out, err)
	}
	id := out.ID
	if err := k.Handle(context.Background(), inbox.TerminalEvent("u1", "hi", now)); err != nil {
		t.Fatal(err)
	}
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	var env string
	for _, e := range entries {
		if strings.Contains(e.Message.Text(), "<environment_") {
			env += e.Message.Text() + "\n"
		}
	}
	want := "当前时间：2026-10-15T20:07:00+08:00 (Asia/Shanghai)"
	if !strings.Contains(env, want) {
		t.Fatalf("env missing local time:\n%s", env)
	}
	if len(p.reqs) == 0 || strings.Contains(p.reqs[0].Messages[0].Text(), "Asia/Shanghai") {
		t.Fatal("local time leaked into the stable prefix")
	}
	var posted []inbox.Event
	fire := func(when time.Time) {
		t.Helper()
		posted = nil
		if err := tm.Fire(when, func(ev inbox.Event) error {
			posted = append(posted, ev)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	fire(at.Add(-20 * time.Second))
	if len(posted) != 0 {
		t.Fatalf("early %+v", posted)
	}
	fire(at.Add(10 * time.Second))
	if len(posted) != 1 || posted[0].Time.Sub(at) > 15*time.Second {
		t.Fatalf("slot %+v", posted)
	}
	got, err := tm.Load(id)
	if err != nil || got.Enabled {
		t.Fatalf("enabled after fire %+v %v", got, err)
	}
	fire(at.Add(10 * time.Second))
	if len(posted) != 0 {
		t.Fatalf("fired again %+v", posted)
	}
}
