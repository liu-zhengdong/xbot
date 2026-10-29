package kernel

import (
	"context"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/inbox"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/peer"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

func TestForkHandoffIsNotACopy(t *testing.T) {
	root := t.TempDir()
	mem := memory.Open(root)
	src, err := bot.Create(root, "ada", "Ship the notes app.", []string{link.NameTerminal, link.NameMemory, link.NamePeers})
	if err != nil {
		t.Fatal(err)
	}
	if err := src.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	if err := src.InheritBinding(bot.Config{Provider: "deepseek", Model: "deepseek-chat", ContextLimit: 32000}); err != nil {
		t.Fatal(err)
	}
	if err := src.GrantWebhook("gh"); err != nil {
		t.Fatal(err)
	}
	if err := src.GrantWatch("notes"); err != nil {
		t.Fatal(err)
	}
	if err := src.Append(bot.Entry{Time: time.Now(), Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("hello")}}); err != nil {
		t.Fatal(err)
	}
	if err := src.Append(bot.Entry{Time: time.Now(), Message: provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("SECRET-DECISION ship friday")}}); err != nil {
		t.Fatal(err)
	}
	if err := src.SaveState(bot.State{Blocks: []bot.SummaryBlock{{From: 1, To: 1, Text: "greeting"}}}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := mem.Remember(ctx, memory.Fact{
		Key: "drink", Kind: memory.KindAttribute, Scope: memory.ScopePerson, Tier: memory.TierProfile,
		PersonID: "u1", WrittenBy: "ada", Text: "PERSON-TEA", Time: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := mem.Remember(ctx, memory.Fact{
		Key: "method", Kind: memory.KindWork, Scope: memory.ScopeBot, Tier: memory.TierProfile,
		BotName: "ada", WrittenBy: "ada", Text: "SOURCE-METHOD keep logs terse", Time: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	comp := &scriptCompactor{text: strings.Repeat("handoff-word ", 180) + "HANDOFF-END"}
	k := &Kernel{Bot: src, Memory: mem, Compactor: comp, Links: []link.Link{link.Peers{}}}
	tools, err := k.modelTools()
	if err != nil {
		t.Fatal(err)
	}
	if !hasTool(tools, "fork_bot") {
		t.Fatal("fork_bot missing")
	}
	bare := &Kernel{Bot: src, Links: []link.Link{link.Terminal{}}}
	bareTools, err := bare.modelTools()
	if err != nil {
		t.Fatal(err)
	}
	if !hasTool(bareTools, "fork_bot") {
		t.Fatal("native fork_bot missing")
	}

	out, err := (forkBotTool{k}).Run(ctx, []byte(`{"name":"reviewer","role":"Review pull requests."}`))
	if err != nil {
		t.Fatal(err)
	}
	if out != "forked reviewer" {
		t.Fatalf("tool %q", out)
	}
	if comp.calls != 1 || !strings.Contains(comp.prior, "Review pull requests.") {
		t.Fatalf("compactor calls=%d prior=%q", comp.calls, comp.prior)
	}
	sawSecret := false
	for _, m := range comp.turns {
		if strings.Contains(m.Text(), "SECRET-DECISION") {
			sawSecret = true
		}
	}
	if !sawSecret {
		t.Fatal("handoff did not see the source transcript")
	}

	child, err := bot.Load(root, "reviewer")
	if err != nil {
		t.Fatal(err)
	}
	persona, err := child.ReadPersona()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(persona) != "Review pull requests." {
		t.Fatalf("persona %q", persona)
	}
	entries, err := child.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("child transcript %d", len(entries))
	}
	st, err := child.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Blocks) != 0 {
		t.Fatalf("child blocks %+v", st.Blocks)
	}
	srcEntries, err := src.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(srcEntries) != 2 || !strings.Contains(srcEntries[1].Message.Text(), "SECRET-DECISION") {
		t.Fatalf("source transcript changed: %+v", srcEntries)
	}
	srcState, err := src.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if len(srcState.Blocks) != 1 || srcState.Blocks[0].Text != "greeting" {
		t.Fatalf("source blocks %+v", srcState.Blocks)
	}
	cfg := child.Config()
	if cfg.Provider != "deepseek" || cfg.Model != "deepseek-chat" || cfg.ContextLimit != 32000 {
		t.Fatalf("binding %+v", cfg)
	}
	if len(cfg.Webhooks) != 0 || len(cfg.Watches) != 0 {
		t.Fatalf("copied hooks %+v watches %+v", cfg.Webhooks, cfg.Watches)
	}
	if len(src.Config().Webhooks) != 1 || len(src.Config().Watches) != 1 {
		t.Fatal("source hooks dropped")
	}

	childFacts, err := mem.Recall(ctx, memory.Query{Scope: memory.ScopeBot, BotName: "reviewer", Tier: memory.TierProfile})
	if err != nil {
		t.Fatal(err)
	}
	joined := joinFacts(childFacts)
	if !strings.Contains(joined, "HANDOFF-END") || !strings.Contains(joined, "handoff-word") {
		t.Fatalf("handoff missing: %s", joined)
	}
	if !strings.Contains(joined, "message_bot") || !strings.Contains(joined, "ada") {
		t.Fatalf("peer note missing: %s", joined)
	}
	if strings.Contains(joined, "SECRET-DECISION") || strings.Contains(joined, "PERSON-TEA") || strings.Contains(joined, "SOURCE-METHOD") {
		t.Fatalf("copied private text into child memory: %s", joined)
	}
	people, err := mem.Recall(ctx, memory.Query{Scope: memory.ScopePerson, PersonID: "u1", Tier: memory.TierProfile})
	if err != nil {
		t.Fatal(err)
	}
	if len(people) != 1 || people[0].Text != "PERSON-TEA" {
		t.Fatalf("person memory %+v", people)
	}
	srcFacts, err := mem.Recall(ctx, memory.Query{Scope: memory.ScopeBot, BotName: "ada", Tier: memory.TierProfile})
	if err != nil {
		t.Fatal(err)
	}
	srcJoined := joinFacts(srcFacts)
	if !strings.Contains(srcJoined, "message_bot") || !strings.Contains(srcJoined, "reviewer") {
		t.Fatalf("source note missing: %s", srcJoined)
	}
	if strings.Contains(srcJoined, "HANDOFF-END") {
		t.Fatal("handoff was written onto the source")
	}

	ck := &Kernel{Bot: child, Links: []link.Link{link.Peers{}}}
	sent, err := (messageBotTool{ck}).Run(ctx, []byte(`{"bot":"ada","text":"what was the decision?"}`))
	if err != nil {
		t.Fatal(err)
	}
	if sent != "sent to ada" {
		t.Fatalf("sent %q", sent)
	}
	box, err := inbox.Open(src.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	ev, ok := box.Head()
	if !ok || !strings.Contains(ev.Content, "what was the decision?") || ev.Sender != "reviewer" {
		t.Fatalf("inbox %+v ok %v", ev, ok)
	}
	if err := (&peer.Bus{Root: root}).Message("ada", "reviewer", "friday", "ch_fork", 1, time.Now()); err != nil {
		t.Fatal(err)
	}
	childBox, err := inbox.Open(child.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	reply, ok := childBox.Head()
	if !ok || reply.Content != "friday" || reply.Sender != "ada" {
		t.Fatalf("child inbox %+v", reply)
	}

	groups, err := (&peer.Bus{Root: root}).ListGroups()
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 0 {
		t.Fatalf("group created without a name: %+v", groups)
	}

	if _, err := k.Fork(ctx, ForkRequest{Name: "reviewer", Role: "again"}); err == nil || comp.calls != 1 {
		t.Fatalf("second fork err=%v calls=%d", err, comp.calls)
	}
}

func TestForkGroupOptional(t *testing.T) {
	root := t.TempDir()
	mem := memory.Open(root)
	src, err := bot.Create(root, "ada", "persona", []string{link.NamePeers})
	if err != nil {
		t.Fatal(err)
	}
	if err := src.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	k := &Kernel{Bot: src, Memory: mem, Compactor: &scriptCompactor{text: "handoff for the group"}}
	res, err := k.Fork(context.Background(), ForkRequest{Name: "qa", Role: "Find regressions.", Group: "ship"})
	if err != nil {
		t.Fatal(err)
	}
	if res.GroupID == "" {
		t.Fatal("missing group id")
	}
	groups, err := (&peer.Bus{Root: root}).ListGroups()
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0].ID != res.GroupID || groups[0].Name != "ship" {
		t.Fatalf("%+v", groups)
	}
	if !hasMember(groups[0].Members, "ada") || !hasMember(groups[0].Members, "qa") {
		t.Fatalf("members %v", groups[0].Members)
	}
	child, err := bot.Load(root, "qa")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := child.Entries()
	if err != nil || len(entries) != 0 {
		t.Fatalf("entries %d %v", len(entries), err)
	}
}

func TestSplitFactKeepsParagraphs(t *testing.T) {
	alpha := strings.TrimSpace(strings.Repeat("alpha ", 40))
	beta := strings.TrimSpace(strings.Repeat("beta ", 200))
	text := alpha + "\n\n" + beta
	parts := splitFact(text, 700)
	if len(parts) < 2 {
		t.Fatalf("parts %d", len(parts))
	}
	if parts[0] != alpha {
		t.Fatalf("first chunk %q", parts[0])
	}
	for _, p := range parts {
		if strings.Contains(p, "bet a") || strings.HasSuffix(strings.TrimSpace(p), "be") {
			t.Fatalf("split a word: %q", p)
		}
	}
	if strings.Contains(parts[0], "beta") {
		t.Fatal("paragraphs were merged")
	}
}

func TestForkEmptyTranscriptSkipsModel(t *testing.T) {
	root := t.TempDir()
	src, err := bot.Create(root, "ada", "persona", []string{link.NamePeers})
	if err != nil {
		t.Fatal(err)
	}
	if err := src.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	comp := &scriptCompactor{text: "should not run"}
	k := &Kernel{Bot: src, Memory: memory.Open(root), Compactor: comp}
	res, err := k.Fork(context.Background(), ForkRequest{Name: "neo", Role: "Take notes."})
	if err != nil {
		t.Fatal(err)
	}
	if res.Name != "neo" || comp.calls != 0 {
		t.Fatalf("%+v calls %d", res, comp.calls)
	}
	facts, err := k.Memory.Recall(context.Background(), memory.Query{Scope: memory.ScopeBot, BotName: "neo", Tier: memory.TierProfile})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(joinFacts(facts), "No transcript yet") {
		t.Fatalf("%s", joinFacts(facts))
	}
}

type scriptCompactor struct {
	text  string
	prior string
	turns []provider.Message
	calls int
}

func (s *scriptCompactor) Compact(_ context.Context, prior string, turns []provider.Message) (string, error) {
	s.calls++
	s.prior = prior
	s.turns = append([]provider.Message(nil), turns...)
	return s.text, nil
}

func hasTool(list []tools.Tool, name string) bool {
	for _, tool := range list {
		if tool.Name() == name {
			return true
		}
	}
	return false
}

func joinFacts(facts []memory.Fact) string {
	var b strings.Builder
	for _, f := range facts {
		b.WriteString(f.Text)
		b.WriteByte('\n')
	}
	return b.String()
}
