package kernel

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
)

func TestNearFullProfileInjectsEveryScope(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	store := memory.Open(b.Root)
	k := &Kernel{Bot: b, Memory: store}
	ctx := context.Background()
	when := time.Date(2026, 10, 17, 3, 0, 0, 0, time.UTC)
	const marker = "bot-fact-must-survive"

	fill := func(scope memory.Scope, person, anchor string) {
		t.Helper()
		wrote := 0
		for i := 0; i < 200; i++ {
			text := anchor
			if i > 0 {
				text = fmt.Sprintf("%s-%03d %s", scope, i, strings.Repeat("n", 360))
			}
			err := store.Remember(ctx, memory.Fact{
				Key:       fmt.Sprintf("%s-%03d", scope, i),
				Scope:     scope,
				Tier:      memory.TierProfile,
				BotName:   b.Name(),
				PersonID:  person,
				WrittenBy: b.Name(),
				Text:      text,
				Time:      when.Add(time.Duration(i) * time.Second),
			})
			if err != nil {
				if wrote == 0 {
					t.Fatalf("%s: first remember: %v", scope, err)
				}
				return
			}
			wrote++
		}
		t.Fatalf("%s: profile cap never hit", scope)
	}

	fill(memory.ScopePerson, "u1", "person-anchor "+strings.Repeat("p", 360))
	if err := store.Remember(ctx, memory.Fact{
		Key: "zzz-bot", Scope: memory.ScopeBot, Tier: memory.TierProfile,
		BotName: b.Name(), WrittenBy: b.Name(), Text: marker, Time: when,
	}); err != nil {
		t.Fatal(err)
	}
	fill(memory.ScopeBot, "", "bot-anchor "+strings.Repeat("b", 360))

	body, err := k.memoryText(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{marker, "### Bot", "bot-anchor", "person-anchor"} {
		if !strings.Contains(body, want) {
			tail := body
			if len(tail) > 500 {
				tail = tail[len(tail)-500:]
			}
			t.Fatalf("missing %q in %d-byte memory\n...%s", want, len(body), tail)
		}
	}
	if strings.Contains(body, "memory clipped") {
		t.Fatalf("standing memory was clipped (%d bytes)", len(body))
	}
	if len(body) <= 4096 {
		t.Fatalf("rendered %d bytes; a 4096-byte head clip would hide the bot fact", len(body))
	}
	msgs, _ := Assemble(Input{Persona: "Be brief.", Rules: DefaultRules(), Memory: body})
	var mem string
	for _, m := range msgs {
		if m.Role == provider.RoleUser && strings.HasPrefix(m.Text(), "<memory>") {
			mem = m.Text()
		}
	}
	if mem == "" || !strings.Contains(mem, marker) || strings.Contains(mem, "memory clipped") {
		t.Fatalf("injected memory dropped a fact\n%s", mem)
	}
}

func TestClipMemoryReportsConsolidation(t *testing.T) {
	body := strings.Repeat("a", memory.ProfileInjectBytes+recentLogBytes+80)
	got := clipMemory(body)
	if !strings.Contains(got, "memory clipped") || !strings.Contains(got, "consolidate") {
		end := got
		if len(end) > 160 {
			end = end[len(end)-160:]
		}
		t.Fatalf("clip hid the cut: %q", end)
	}
	if !strings.HasPrefix(got, "aaa") {
		t.Fatal("clip did not keep the head")
	}
	if len(got) > memory.ProfileInjectBytes+recentLogBytes {
		t.Fatalf("clipped memory is %d bytes, over the budget", len(got))
	}
}
