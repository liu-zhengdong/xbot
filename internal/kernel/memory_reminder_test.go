package kernel

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
)

func TestRegisteredMemoryCapacity(t *testing.T) {
	for _, scope := range []string{"bot", "person"} {
		for _, size := range []int{100, memory.ProfileBytes * 85 / 100, memory.ProfileBytes*85/100 + 1, memory.ProfileBytes, memory.ProfileBytes + 100} {
			t.Run(fmt.Sprintf("%s/%d", scope, size), func(t *testing.T) {
				b := testBot(t, link.NameTerminal)
				k := &Kernel{Bot: b, Memory: memory.Open(b.Root)}
				dir := filepath.Join(b.Root, "bots", b.Name(), "memory")
				if scope == "person" {
					dir = filepath.Join(b.Root, "people", "u1", "memory")
				}
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(dir, "memory.md")
				if err := os.WriteFile(path, []byte(strings.Repeat("x", size)), 0600); err != nil {
					t.Fatal(err)
				}
				now := time.Unix(5000, 0)
				ev := link.TerminalEvent("u1", "hello", now)
				bind := func(at time.Time) string {
					t.Helper()
					msgs, err := k.reminderKinds(ev).Bind([]provider.Message{{Role: provider.RoleUser, Content: provider.TextContent("hello")}}, 1, at)
					if err != nil {
						t.Fatal(err)
					}
					return msgs[len(msgs)-1].Text()
				}
				text := bind(now)
				want := size*100 >= memory.ProfileBytes*85
				if strings.Contains(text, "先整理或删旧再写") != want {
					t.Fatalf("capacity reminder: %q", text)
				}
				if !want {
					return
				}
				state := "将满"
				if size >= memory.ProfileBytes {
					state = "已满"
				}
				for _, part := range []string{"<system-reminder>", "记忆" + state, path, fmt.Sprintf("当前 %d / 上限 %d 字节", size, memory.ProfileBytes)} {
					if !strings.Contains(text, part) {
						t.Fatalf("missing %q: %s", part, text)
					}
				}
				if strings.Contains(bind(now.Add(time.Minute)), "先整理或删旧再写") {
					t.Fatal("capacity cooldown ignored")
				}
				if !strings.Contains(bind(now.Add(memoryReminderCooldown)), "先整理或删旧再写") {
					t.Fatal("capacity reminder did not return")
				}
				if err := os.WriteFile(path, []byte("small"), 0600); err != nil {
					t.Fatal(err)
				}
				if strings.Contains(bind(now.Add(2*memoryReminderCooldown)), "先整理或删旧再写") {
					t.Fatal("stale capacity after consolidation")
				}
			})
		}
	}
}

func TestRegisteredMemoryReflection(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	k := &Kernel{Bot: b, Memory: memory.Open(b.Root)}
	now := time.Unix(5000, 0)
	ev := link.TerminalEvent("u1", "hello", now)
	const note = "本轮有值得沉淀的就记进记忆，没有就跳过。"
	bind := func(ev link.Event, at time.Time) string {
		t.Helper()
		msgs, err := k.reminderKinds(ev).Bind([]provider.Message{{Role: provider.RoleUser, Content: provider.TextContent("hello")}}, 1, at)
		if err != nil {
			t.Fatal(err)
		}
		return msgs[len(msgs)-1].Text()
	}
	if !strings.Contains(bind(ev, now), note) {
		t.Fatal("missing reflection")
	}
	if strings.Contains(bind(ev, now.Add(time.Minute)), note) {
		t.Fatal("reflection cooldown ignored")
	}
	// A fresh channel/kernel uses the same sidecar cooldown, not turn-local state.
	k = &Kernel{Bot: b, Memory: memory.Open(b.Root)}
	if strings.Contains(bind(ev, now.Add(2*time.Minute)), note) {
		t.Fatal("cooldown lost on reopen")
	}
	if !strings.Contains(bind(ev, now.Add(memoryReminderCooldown)), note) {
		t.Fatal("reflection did not return")
	}
	timer := ev
	timer.Source = link.NameTimer
	timer.SenderKind = link.KindBot
	if strings.Contains(bind(timer, now.Add(2*memoryReminderCooldown)), note) {
		t.Fatal("timer reflection")
	}
	k.Memory = nil
	if strings.Contains(bind(ev, now.Add(3*memoryReminderCooldown)), note) {
		t.Fatal("reflection without memory")
	}
}

func TestMemoryReminderPreservesProfileFull(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	store := memory.Open(b.Root)
	path := filepath.Join(b.Root, "bots", b.Name(), "memory", "memory.md")
	ctx := context.Background()
	for i := 0; i < 80; i++ {
		before, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		err = store.Remember(ctx, memory.Fact{Scope: memory.ScopeBot, BotName: b.Name(), Tier: memory.TierProfile, Text: fmt.Sprintf("事实 %d %s", i, strings.Repeat("内容", 100))})
		if err == nil {
			continue
		}
		if !errors.Is(err, memory.ErrProfileFull) {
			t.Fatal(err)
		}
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(before) != string(after) {
			t.Fatal("rejected write changed/truncated profile")
		}
		k := &Kernel{Bot: b, Memory: store}
		msgs, err := k.reminderKinds(link.TerminalEvent("u1", "hello", time.Now())).Bind([]provider.Message{{Role: provider.RoleUser, Content: provider.TextContent("hello")}}, 1, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(msgs[0].Text(), "先整理或删旧再写") {
			t.Fatal("full store did not receive capacity advice")
		}
		return
	}
	t.Fatal("profile cap not enforced")
}
