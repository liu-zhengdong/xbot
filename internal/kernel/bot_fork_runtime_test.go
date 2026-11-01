package kernel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"xbot/internal/agent"
	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/compact"
	"xbot/internal/inbox"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/peer"
	"xbot/internal/person"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

// Only the provider is scripted. The production compact.LLM, Jobs, source
// preparation, HTTP handler, domain writer and memory store all execute.
type botForkProvider struct {
	chat func(context.Context, provider.Request) (provider.Message, error)
}

func (p botForkProvider) Chat(ctx context.Context, req provider.Request, _ func(string)) (provider.Message, error) {
	return p.chat(ctx, req)
}

func botForkRuntimeFixture(t *testing.T, routed bool) (*Kernel, string, string) {
	t.Helper()
	root := t.TempDir()
	owner, err := person.Identify(root, person.Account{Link: "terminal", ExternalID: "uid:1"}, "Original owner")
	if err != nil {
		t.Fatal(err)
	}
	b, err := bot.Create(root, "source", "SOURCE-PERSONA ship the notes project", []string{link.NameTerminal, link.NameTools, link.NameMemory})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{owner.ID, "shared-owner"}); err != nil {
		t.Fatal(err)
	}
	if err := b.InheritBinding(bot.Config{Provider: "original-provider", Model: "original-model", ContextLimit: 32768}); err != nil {
		t.Fatal(err)
	}
	role, budget := bot.RoleDev, 7200
	if err := b.ApplyProfile(bot.ProfileUpdate{Role: &role, BackgroundMaxTime: &budget}); err != nil {
		t.Fatal(err)
	}
	if err := b.WriteRules("SOURCE-RULE retain original decisions."); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"COVERED-RAW", "RAW-PROJECT release Friday; personOriginal said keep tests."} {
		if err := b.Append(bot.Entry{Time: time.Unix(1700000000, 0), Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent(text)}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.SaveState(bot.State{Blocks: []bot.SummaryBlock{{From: 1, To: 1, Text: "SUMMARY-PROJECT selected the notes architecture"}}}); err != nil {
		t.Fatal(err)
	}
	var k *Kernel
	if routed {
		k = botControlKernel(t, b)
	} else {
		k = &Kernel{Bot: b}
	}
	k.Memory = memory.Open(root)
	k.WorkDir = filepath.Join(root, "work", b.Name())
	if err := os.MkdirAll(k.WorkDir, 0700); err != nil {
		t.Fatal(err)
	}
	k.Now = func() time.Time { return time.Unix(1700000000, 0).UTC() }
	for _, fact := range []memory.Fact{
		{Key: "method", Kind: memory.KindWork, Scope: memory.ScopeBot, Tier: memory.TierProfile, BotName: b.Name(), WrittenBy: b.Name(), Text: "BOT-PROFILE keep review decisions concise", Time: k.now()},
		{Key: "tea", Kind: memory.KindAttribute, Scope: memory.ScopePerson, Tier: memory.TierProfile, PersonID: owner.ID, WrittenBy: b.Name(), Text: "PERSON-STORE-ONLY green tea", Time: k.now()},
	} {
		if err := k.Memory.Remember(context.Background(), fact); err != nil {
			t.Fatal(err)
		}
	}
	if err := k.StartOperations(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = k.Close() })
	return k, operationEndpoint(k), owner.ID
}

func botForkReceipt(t *testing.T, reply cliops.BotForkReply) {
	t.Helper()
	raw, err := json.Marshal(reply)
	if err != nil || len(raw)+1 > tools.DefaultShellOutputBytes || len(cliops.FormatBotFork(reply)) > tools.DefaultShellOutputBytes {
		t.Errorf("receipt over Shell budget json=%d human=%d err=%v", len(raw)+1, len(cliops.FormatBotFork(reply)), err)
	}
}

func botForkFacts(t *testing.T, k *Kernel, name string) []memory.Fact {
	t.Helper()
	facts, err := k.Memory.Recall(context.Background(), memory.Query{Scope: memory.ScopeBot, Tier: memory.TierProfile, BotName: name})
	if err != nil {
		t.Fatal(err)
	}
	return facts
}

func TestBotForkRuntimeActualLLMNonprimaryAndNextWake(t *testing.T) {
	k, endpoint, ownerID := botForkRuntimeFixture(t, false)
	primary := botControlMake(t, k.Bot.Root, "primary", ownerID)
	if err := bot.SetPrimary(k.Bot.Root, primary.Name()); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XBOT_BOT", "stale-diagnostic-name")
	// A nil-resource fork must not open the source Inbox during a snapshot.
	if err := os.WriteFile(k.Bot.InboxPath(), []byte("invalid inbox JSON\n"), 0600); err != nil {
		t.Fatal(err)
	}
	personPath := filepath.Join(k.Bot.Root, "people", ownerID, "memory", "memory.md")
	personBefore, err := os.ReadFile(personPath)
	if err != nil {
		t.Fatal(err)
	}
	const role = "Review notes-project changes, retaining the chosen architecture."
	const handoff = "MODEL-HANDOFF the notes project ships Friday; review changes against the selected architecture."
	model := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: provider.TextContent(handoff), Usage: provider.TokenUsage{PromptTokens: 71, CompletionTokens: 13, Reported: true}}}}
	k.Compactor = compact.LLM{P: model}
	original := k.ensureTurn()
	original.fromSeq = 17
	var reply cliops.BotForkReply
	runRoutineSource(t, k, nil, `{"command":"\"$XBOT_BIN\" bot-fork reviewer --self --role-file duties.txt --group project --json"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
		frozen, _ := ctx.Value(turnKey{}).(*turn)
		if frozen == nil || frozen == original || turnSeqOf(ctx) != 17 {
			t.Error("not the original prepared frozen turn")
		}
		original.mu.Lock()
		original.fromSeq = 88
		original.mu.Unlock()
		k.turnMu.Lock()
		k.turn = &turn{fromSeq: 99}
		k.turnMu.Unlock()
		var err error
		reply, err = cliops.CallBotFork(ctx, endpoint, k.Bot.Root, tools.CallID(ctx), cliops.BotFork{Name: " reviewer ", Role: " \n" + role + "\t", Group: " project "})
		if err != nil || !reply.Created || reply.Name != "reviewer" || reply.Stage != "complete" || reply.GroupID == "" || reply.UncertainMemory != nil {
			t.Errorf("actual fork receipt %+v err=%v", reply, err)
		}
		botForkReceipt(t, reply)
		return "completed", nil
	})
	if len(model.reqs) != 1 {
		t.Fatalf("actual compact.LLM calls=%d", len(model.reqs))
	}
	req := model.reqs[0]
	if len(req.Tools) != 0 || len(req.Messages) != 2 || req.Messages[0].Role != provider.RoleSystem || req.Messages[1].Role != provider.RoleUser {
		t.Fatalf("handoff did not use no-tools System/Data Chat: %+v", req)
	}
	body := req.Messages[1].Text()
	for _, want := range []string{"<prior_summary>", "Focus: Handoff for role", role, "SOURCE-PERSONA", "SUMMARY-PROJECT", "RAW-PROJECT", "BOT-PROFILE"} {
		if !strings.Contains(body, want) {
			t.Errorf("actual model input missing %q", want)
		}
	}
	if strings.Contains(body, "PERSON-STORE-ONLY") || strings.Contains(body, "COVERED-RAW") {
		t.Error("direct person store or covered raw row was copied into handoff input")
	}
	child, err := bot.Load(k.Bot.Root, "reviewer")
	if err != nil {
		t.Fatal(err)
	}
	if persona, err := child.ReadPersona(); err != nil || persona != role {
		t.Fatalf("child full persona %q err=%v", persona, err)
	}
	cfg, sourceCfg := child.Config(), k.Bot.Config()
	if !reflect.DeepEqual(cfg.Owners, sourceCfg.Owners) || !reflect.DeepEqual(cfg.Links, sourceCfg.Links) || cfg.Provider != sourceCfg.Provider || cfg.Model != sourceCfg.Model || cfg.ContextLimit != sourceCfg.ContextLimit {
		t.Fatalf("child inheritance %+v source=%+v", cfg, sourceCfg)
	}
	if cfg.Role == bot.RoleDev || cfg.Working().BackgroundMaxTime != 45*time.Minute || cfg.BackgroundMaxTime != 0 {
		t.Fatalf("inherited source dev budget: %+v", cfg)
	}
	if entries, err := child.Entries(); err != nil || len(entries) != 0 {
		t.Fatalf("child transcript copied: %d %v", len(entries), err)
	}
	if state, err := child.LoadState(); err != nil || len(state.Blocks) != 0 {
		t.Fatalf("child summary blocks copied: %+v %v", state, err)
	}
	if rules, err := child.ReadRules(""); err != nil || !strings.Contains(rules, "SOURCE-RULE") {
		t.Fatalf("child rules %q %v", rules, err)
	}
	wantConfirmed := []string{"owners", "binding", "rules", "handoff", "child_note", "source_note", "group"}
	if !reflect.DeepEqual(reply.Confirmed, wantConfirmed) || reply.HandoffParts != 1 || reply.HandoffTotal != 1 {
		t.Fatalf("acknowledgements %+v", reply)
	}
	childFacts := joinFacts(botForkFacts(t, k, child.Name()))
	if !strings.Contains(childFacts, handoff) || !strings.Contains(childFacts, "Forked from source") || strings.Contains(childFacts, "PERSON-STORE-ONLY") || strings.Contains(childFacts, "BOT-PROFILE") {
		t.Fatalf("handoff and child note facts %q", childFacts)
	}
	if facts := joinFacts(botForkFacts(t, k, "source")); !strings.Contains(facts, "Forked reviewer") || strings.Contains(facts, handoff) {
		t.Fatalf("source note facts %q", facts)
	}
	personAfter, err := os.ReadFile(personPath)
	if err != nil || !bytes.Equal(personBefore, personAfter) {
		t.Fatalf("shared person file changed: %v", err)
	}
	g, err := (&peer.Bus{Root: k.Bot.Root}).LoadGroup(reply.GroupID)
	if err != nil || !reflect.DeepEqual(g.Members, []string{"reviewer", "source"}) {
		t.Fatalf("optional group %+v err=%v", g, err)
	}
	if box, err := inbox.Open(child.InboxPath()); err != nil || len(box.Pending()) != 0 {
		t.Fatalf("fork posted/woke child: %v", err)
	}
	traces, err := k.Bot.Traces()
	if err != nil || len(traces) != 1 || traces[0].Call != bot.TraceCallCompact || traces[0].Turn != 17 || traces[0].Prompt != 71 {
		t.Fatalf("usage lost original turn: %+v err=%v", traces, err)
	}
	consumer := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: provider.TextContent("SILENT")}}}
	ck := &Kernel{Bot: child, Memory: k.Memory, Provider: consumer, Now: k.Now}
	if err := ck.Handle(context.Background(), link.NewEvent("timer", "tick", "Review the inherited project handoff.", "timer", k.now())); err != nil {
		t.Fatal(err)
	}
	if len(consumer.reqs) != 1 {
		t.Fatalf("child next wake requests=%d", len(consumer.reqs))
	}
	var composed strings.Builder
	for _, m := range consumer.reqs[0].Messages {
		composed.WriteString(m.Text())
	}
	if !strings.Contains(composed.String(), handoff) || !strings.Contains(composed.String(), role) || !strings.Contains(composed.String(), "SOURCE-RULE") {
		t.Fatal("actual child provider compose did not consume handoff/persona/rules")
	}
}

func TestBotForkRuntimeSourceMissingFinishedForeignCanceledNonShell(t *testing.T) {
	k, endpoint, _ := botForkRuntimeFixture(t, false)
	foreign := &Kernel{Bot: k.Bot, shell: k.shellJobs(), WorkDir: k.WorkDir}
	if err := foreign.StartOperations(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(foreign.CloseOperations)
	check := func(endpoint, id string) {
		t.Helper()
		reply, err := cliops.CallBotFork(context.Background(), endpoint, k.Bot.Root, id, cliops.BotFork{Name: "rejected", Role: "review changes"})
		if err == nil || reply.Created || reply.Stage != "validation" || reply.HandoffParts != 0 {
			t.Errorf("invalid source accepted: %+v %v", reply, err)
		}
		botForkReceipt(t, reply)
	}
	check(endpoint, "")
	check(endpoint, "not-an-actual-job")
	var completed string
	runRoutineSource(t, k, nil, `{"command":"bot-fork rejected --self --role review"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
		completed = tools.CallID(ctx)
		check(operationEndpoint(foreign), completed)
		return "completed", nil
	})
	check(endpoint, completed)
	runRoutineSource(t, k, nil, `{"command":"bot-fork rejected --self --role review"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
		if _, err := k.liveShell().Cancel(tools.CallID(ctx)); err != nil {
			t.Error(err)
		}
		check(endpoint, tools.CallID(ctx))
		return "completed", nil
	})
	shell := chatGroupNonShell{routineSourceShell{run: func(ctx context.Context, _ json.RawMessage) (string, error) {
		check(endpoint, tools.CallID(ctx))
		return "completed", nil
	}}}
	p := &scripted{resps: []provider.Message{toolCall("non-shell", shell.Name(), `{}`)}}
	ctx := context.WithValue(context.Background(), turnKey{}, k.ensureTurn())
	if _, err := agent.Run(ctx, p, []tools.Tool{shell}, nil, agent.Options{OneRound: true, RunTool: k.runToolCall}); err != nil {
		t.Fatal(err)
	}
	if _, err := bot.Load(k.Bot.Root, "rejected"); !errors.Is(err, bot.ErrNotFound) {
		t.Fatalf("invalid source reached domain creation: %v", err)
	}
}

func TestBotForkRuntimeHTTPValidationAndPrivateShortReceipt(t *testing.T) {
	k, endpoint, _ := botForkRuntimeFixture(t, false)
	const duties = "PRIVATE-DUTIES never echo this body in a receipt"
	k.Compactor = compact.LLM{P: botForkProvider{chat: func(context.Context, provider.Request) (provider.Message, error) {
		return provider.Message{}, errors.New("CAUSE-MARKER source-redaction staged-secret-123")
	}}}
	k.Redact = func(s string) string { return strings.ReplaceAll(s, "source-redaction", "[source]") }
	k.secrets = map[string]map[string]string{"fixture": {"token": "staged-secret-123"}}
	runRoutineSource(t, k, nil, `{"command":"bot-fork reviewer --self --role-file private.txt"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
		for _, tc := range []struct{ path, body string }{
			{"/v1/bot/fork", `[]`}, {"/v1/bot/fork", `null`},
			{"/v1/bot/fork", `{"name":"reviewer","role":"review","unknown":true}`},
			{"/v1/bot/fork", `{"name":"reviewer","role":"review"}{}`},
			{"/v1/bot/fork?name=other", `{"name":"reviewer","role":"review"}`},
		} {
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+tc.path, strings.NewReader(tc.body))
			if err != nil {
				return "", err
			}
			req.Header.Set("X-XBot-Root", k.Bot.Root)
			req.Header.Set("X-XBot-Call-ID", tools.CallID(ctx))
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return "", err
			}
			_, readErr := io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if readErr != nil || resp.StatusCode < 400 {
				t.Errorf("invalid wire body accepted path=%s status=%d err=%v", tc.path, resp.StatusCode, readErr)
			}
		}
		reply, err := cliops.CallBotFork(ctx, endpoint, k.Bot.Root, tools.CallID(ctx), cliops.BotFork{Name: "reviewer", Role: duties})
		if err == nil || reply.Created || !strings.Contains(reply.Error, "CAUSE-MARKER") || !strings.Contains(reply.Error, "[source]") || strings.Contains(reply.Error, "staged-secret-123") || strings.Contains(reply.Error, "source-redaction") {
			t.Errorf("original redaction/cause lost %+v %v", reply, err)
		}
		raw, _ := json.Marshal(reply)
		if strings.Contains(string(raw), duties) || strings.Contains(cliops.FormatBotFork(reply), duties) {
			t.Error("private role was echoed")
		}
		botForkReceipt(t, reply)
		reply, err = cliops.CallBotFork(ctx, endpoint, k.Bot.Root, tools.CallID(ctx), cliops.BotFork{Name: strings.Repeat("\x00", 40000), Role: duties})
		if err == nil || reply.Created || reply.Name != "" || !strings.Contains(reply.Error, "invalid name") {
			t.Errorf("oversized invalid-name receipt lost cause: %+v %v", reply, err)
		}
		botForkReceipt(t, reply)
		return "completed", nil
	})
}
