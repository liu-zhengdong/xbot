package kernel

import (
	"context"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/envprompt"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
)

func TestRegisteredTimeRequests(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	now := time.Date(2026, 10, 31, 8, 17, 0, 0, time.UTC)
	p := &scripted{}
	for i := 0; i < 5; i++ {
		p.resps = append(p.resps, provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("ok")})
	}
	k := &Kernel{Bot: b, Provider: p, Memory: memory.NopStore{}, Now: func() time.Time { return now }}
	// An unrelated field joins by Register alone, without touching sampling or encoding.
	k.registeredEnvironment().Register(envprompt.Field{Key: "weather", Sample: func(context.Context) (envprompt.Value, error) { return "晴", nil }, Prompt: func(v envprompt.Value, _ envprompt.Change) string { return "天气：" + v.(string) }})
	handle := func() {
		t.Helper()
		if err := k.Handle(context.Background(), link.TerminalEvent("u1", "几点了", now)); err != nil {
			t.Fatal(err)
		}
	}
	handle()
	prefix := p.reqs[0].Messages[0].Text()
	now = now.Add(30 * time.Second)
	handle()
	now = now.Add(30 * time.Second)
	handle()
	for i, want := range []int{1, 1, 2} {
		text := messagesText(p.reqs[i].Messages)
		if strings.Count(text, "当前时间：") != want {
			t.Fatalf("request %d: time announcements != %d", i, want)
		}
		if strings.Contains(text, `"time":`) {
			t.Fatal("request still contains time JSON field")
		}
		if p.reqs[i].Messages[0].Text() != prefix {
			t.Fatal("stable prefix changed")
		}
	}
	if k.registeredEnvironment().Current("time") != k.localStamp() {
		t.Fatal("clock truth not sampled")
	}
	if !strings.Contains(messagesText(p.reqs[2].Messages), "天气：晴") {
		t.Fatal("registered field not delivered")
	}
	// Cover all announcements: a same-minute full snapshot must restore them.
	n, _ := b.Count()
	st, _ := b.LoadState()
	st.Blocks = []bot.SummaryBlock{{From: 1, To: n, Text: "earlier turns"}}
	if err := b.SaveState(st); err != nil {
		t.Fatal(err)
	}
	handle()
	text := messagesText(p.reqs[3].Messages)
	if strings.Count(text, "当前时间：") != 1 || !strings.Contains(text, "当前时间："+k.localStamp()) || !strings.Contains(text, "天气：晴") {
		t.Fatal("missing fresh semantic snapshot")
	}
	// The fitted view can lose publications independently of persisted blocks.
	req := p.reqs[3]
	raw := req
	var fitted []provider.Message
	for _, m := range req.Messages {
		if !strings.Contains(m.Text(), bot.EnvironmentOpen) {
			fitted = append(fitted, m)
		}
	}
	req.Messages = fitted
	r := &turnRun{k: k}
	restored, err := r.finishEnvironment(context.Background(), raw, req)
	if err != nil || !strings.Contains(messagesText(restored.Messages), "当前时间："+k.localStamp()) {
		t.Fatalf("fitted snapshot: %v", err)
	}
}

func TestEnvironmentPromptPreviewDoesNotPublish(t *testing.T) {
	k := &Kernel{Bot: testBot(t), Now: func() time.Time { return time.Unix(1200, 0) }}
	preview, _, err := k.environmentPrompts(context.Background(), "", true)
	if err != nil || len(preview) != 1 {
		t.Fatalf("%v %v", preview, err)
	}
	msgs, err := k.appendEnvironmentPrompts(context.Background(), "", false)
	if err != nil || len(msgs) != 1 || msgs[0].Text() != preview[0] {
		t.Fatal("preview consumed announcement")
	}
	msgs, err = k.appendEnvironmentPrompts(context.Background(), "", false)
	if err != nil || len(msgs) != 0 {
		t.Fatal("same minute repeated")
	}
}
