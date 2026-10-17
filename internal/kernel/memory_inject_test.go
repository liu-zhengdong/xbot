package kernel

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
)

func TestProfileCapErrorAsksToConsolidate(t *testing.T) {
	// The store test owns the message. This one checks the tool returns it
	// to the model instead of storing a truncated fact.
	b := testBot(t, link.NameTerminal)
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	store := memory.Open(b.Root)
	k := &Kernel{Bot: b, Memory: store, Now: func() time.Time {
		return time.Date(2026, 10, 17, 4, 0, 0, 0, time.UTC)
	}}
	tool := rememberTool{k}
	var hit error
	for i := 0; i < 80; i++ {
		args := fmt.Sprintf(`{"fact":%q,"scope":"bot","tier":"profile"}`, strings.Repeat("word ", 80)+fmt.Sprintf("%d", i))
		_, err := tool.Run(context.Background(), []byte(args))
		if err != nil {
			hit = err
			break
		}
	}
	if hit == nil {
		t.Fatal("expected the profile tier to hit its cap")
	}
	msg := hit.Error()
	for _, part := range []string{"Consolidate", "merge facts", "forget stale", "log tier"} {
		if !strings.Contains(msg, part) {
			t.Fatalf("tool error %q missing %q", msg, part)
		}
	}
}

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
		if m.Role == provider.RoleUser && strings.HasPrefix(m.Content, "<memory>") {
			mem = m.Content
		}
	}
	if mem == "" || !strings.Contains(mem, marker) || strings.Contains(mem, "memory clipped") {
		t.Fatalf("injected memory dropped a fact\n%s", mem)
	}
}

func TestRememberOmitsScopeWritesBot(t *testing.T) {
	b := testBot(t, link.NameTerminal, link.NameMemory)
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	store := memory.Open(b.Root)
	k := &Kernel{Bot: b, Memory: store, Now: func() time.Time {
		return time.Date(2026, 10, 17, 4, 0, 0, 0, time.UTC)
	}}
	k.ensureTurn().turnSender = "u1"
	k.ensureTurn().turnTrusted = true
	tool := rememberTool{k}
	if _, err := tool.Run(context.Background(), []byte(`{"fact":"always run tests before done","tier":"profile"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Run(context.Background(), []byte(`{"fact":"owner timezone is Asia/Shanghai","scope":"person","tier":"profile"}`)); err != nil {
		t.Fatal(err)
	}
	botFacts, err := store.Recall(context.Background(), memory.Query{Scope: memory.ScopeBot, BotName: b.Name(), Tier: memory.TierProfile})
	if err != nil {
		t.Fatal(err)
	}
	if len(botFacts) != 1 || botFacts[0].Text != "always run tests before done" || botFacts[0].Scope != memory.ScopeBot {
		t.Fatalf("bot %+v", botFacts)
	}
	person, err := store.Recall(context.Background(), memory.Query{Scope: memory.ScopePerson, PersonID: "u1", Tier: memory.TierProfile})
	if err != nil {
		t.Fatal(err)
	}
	if len(person) != 1 || !strings.Contains(person[0].Text, "Asia/Shanghai") {
		t.Fatalf("person %+v", person)
	}
	for _, f := range person {
		if strings.Contains(f.Text, "always run tests") {
			t.Fatalf("work rule landed in person: %+v", f)
		}
	}
	if _, err := tool.Run(context.Background(), []byte(`{"fact":"nope","scope":"bot-person"}`)); err == nil {
		t.Fatal("bot-person scope was accepted")
	}
}

func TestExtractOmittedScopeWritesBot(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	store := memory.Open(b.Root)
	k := &Kernel{
		Bot: b, Memory: store,
		Extractor: stubExtract{facts: []memory.Fact{
			{Tier: memory.TierLog, Text: "omitted lands in bot"},
			{Scope: memory.ScopeBotPerson, Tier: memory.TierLog, Text: "legacy label lands in bot"},
			{Scope: memory.ScopePerson, Tier: memory.TierProfile, Text: "owner timezone is Asia/Shanghai"},
		}},
	}
	k.ensureTurn().turnTrusted = true
	k.ensureTurn().turnSender = "u1"
	k.extractInto(context.Background(), []bot.Entry{{
		Message: provider.Message{Role: provider.RoleUser, Content: "remember the rule and my timezone"},
	}}, "")
	botFacts, err := store.Recall(context.Background(), memory.Query{Scope: memory.ScopeBot, BotName: b.Name()})
	if err != nil {
		t.Fatal(err)
	}
	joined := ""
	for _, f := range botFacts {
		joined += f.Text + "\n"
		if f.Scope != memory.ScopeBot {
			t.Fatalf("scope %+v", f)
		}
	}
	if !strings.Contains(joined, "omitted lands in bot") || !strings.Contains(joined, "legacy label lands in bot") {
		t.Fatalf("bot facts:\n%s", joined)
	}
	if strings.Contains(joined, "Asia/Shanghai") {
		t.Fatalf("timezone stored in bot:\n%s", joined)
	}
	person, err := store.Recall(context.Background(), memory.Query{Scope: memory.ScopePerson, PersonID: "u1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(person) != 1 || !strings.Contains(person[0].Text, "Asia/Shanghai") {
		t.Fatalf("person %+v", person)
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
