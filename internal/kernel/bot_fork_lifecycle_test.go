package kernel

import (
	"context"
	"encoding/json"
	"errors"
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
	"xbot/internal/exec"
	"xbot/internal/inbox"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/peer"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

func TestBotForkRuntimeLLMCurrentRenameFrozenInputAndTurn(t *testing.T) {
	k, endpoint, _ := botForkRuntimeFixture(t, true)
	original := k.Bot
	preLinks := append([]string(nil), original.Config().Links...)
	var postLinks []string
	owner := k.ensureTurn()
	owner.fromSeq = 23
	const duties = "FROZEN-DUTIES review the original project"
	const handoff = "RENAME-HANDOFF carry the original project decision into review."
	input := cliops.BotFork{Name: "reviewer", Role: duties, Group: "original-group"}
	calls := 0
	k.Compactor = compact.LLM{P: botForkProvider{chat: func(ctx context.Context, req provider.Request) (provider.Message, error) {
		calls++
		if turnSeqOf(ctx) != 23 || ctx.Err() != nil || len(req.Tools) != 0 || len(req.Messages) != 2 {
			t.Error("model lost original prepared context/no-tools shape")
		}
		body := req.Messages[1].Text()
		for _, want := range []string{duties, "SOURCE-PERSONA", "SUMMARY-PROJECT", "RAW-PROJECT", "BOT-PROFILE"} {
			if !strings.Contains(body, want) {
				t.Errorf("frozen model body missing %q", want)
			}
		}
		// A legitimate HTTP profile write while the actual model callback is
		// running proves neither operations mutex nor storage route spans Chat.
		profileCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		next, description := "renamed", "profile changed during handoff model"
		profile, err := cliops.Call(profileCtx, endpoint, original.Root, &cliops.Update{Name: &next, Description: &description})
		if err != nil || !profile.Moved || profile.Name != next {
			t.Errorf("profile blocked during actual LLM: %+v %v", profile, err)
			return provider.Message{}, errors.New("profile could not run during handoff")
		}
		if err := k.withCurrentIO(func(current *bot.Bot, _ *inbox.Inbox) error {
			if err := current.WritePersona("CHANGED-PERSONA after handoff snapshot"); err != nil {
				return err
			}
			if err := current.WriteRules("CURRENT-RULE written during model call"); err != nil {
				return err
			}
			if err := current.SetOwners([]string{"shared-owner", "late-owner"}); err != nil {
				return err
			}
			if err := current.GrantLink(link.NameTelegram); err != nil {
				return err
			}
			if err := current.InheritBinding(bot.Config{Provider: "current-provider", Model: "current-model", ContextLimit: 45678}); err != nil {
				return err
			}
			postLinks = append([]string(nil), current.Config().Links...)
			return nil
		}); err != nil {
			return provider.Message{}, err
		}
		// Changing a caller's DTO and live turns cannot change the decoded request
		// or the prepared source sequence already executing in the listener.
		input.Name, input.Role, input.Group = "changed-target", "CHANGED-DUTIES", "changed-group"
		owner.mu.Lock()
		owner.fromSeq = 333
		owner.mu.Unlock()
		k.turnMu.Lock()
		k.turn = &turn{fromSeq: 444}
		k.turnMu.Unlock()
		if strings.Contains(body, "CHANGED-PERSONA") || strings.Contains(body, "CHANGED-DUTIES") || turnSeqOf(ctx) != 23 {
			t.Error("in-flight model input or context was rebound")
		}
		return provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent(handoff), Usage: provider.TokenUsage{PromptTokens: 61, CompletionTokens: 9, Reported: true}}, nil
	}}}
	var reply cliops.BotForkReply
	runRoutineSource(t, k, nil, `{"command":"bot-fork reviewer --self --role-file duties.txt --group original-group"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
		var err error
		reply, err = cliops.CallBotFork(ctx, endpoint, original.Root, tools.CallID(ctx), input)
		if err != nil || !reply.Created || reply.Name != "reviewer" || reply.Stage != "complete" || reply.GroupID == "" {
			t.Errorf("rename fork receipt %+v err=%v", reply, err)
		}
		botForkReceipt(t, reply)
		return "completed", nil
	})
	if calls != 1 || k.Bot != original {
		t.Fatalf("original kernel/LLM was replaced calls=%d", calls)
	}
	child, err := bot.Load(original.Root, "reviewer")
	if err != nil {
		t.Fatal(err)
	}
	if persona, err := child.ReadPersona(); err != nil || persona != duties {
		t.Fatalf("decoded role changed: %q %v", persona, err)
	}
	if rules, err := child.ReadRules(""); err != nil || !strings.Contains(rules, "CURRENT-RULE") || strings.Contains(rules, "SOURCE-RULE") {
		t.Fatalf("post-model rules were stale: %q %v", rules, err)
	}
	cfg := child.Config()
	if !reflect.DeepEqual(cfg.Owners, []string{"u1", "shared-owner"}) || cfg.Provider != "current-provider" || cfg.Model != "current-model" || cfg.ContextLimit != 45678 || reflect.DeepEqual(preLinks, postLinks) || !reflect.DeepEqual(cfg.Links, postLinks) {
		t.Fatalf("pre-model owners/post-model links+binding timing changed %+v", cfg)
	}
	for _, fact := range botForkFacts(t, k, "renamed") {
		if fact.Key == "fork-reviewer" && (fact.BotName != "renamed" || fact.WrittenBy != "renamed") {
			t.Fatalf("source note used old identity: %+v", fact)
		}
	}
	childFacts := botForkFacts(t, k, "reviewer")
	if !strings.Contains(joinFacts(childFacts), "Forked from renamed") || !strings.Contains(joinFacts(childFacts), handoff) {
		t.Fatalf("child note not current: %s", joinFacts(childFacts))
	}
	for _, fact := range childFacts {
		if fact.WrittenBy != "renamed" {
			t.Fatalf("child memory author stale: %+v", fact)
		}
	}
	group, err := (&peer.Bus{Root: original.Root}).LoadGroup(reply.GroupID)
	if err != nil || group.Name != "original-group" || !reflect.DeepEqual(group.Members, []string{"renamed", "reviewer"}) {
		t.Fatalf("group did not use frozen input/current source: %+v %v", group, err)
	}
	current, err := k.Resources.CurrentBot()
	if err != nil {
		t.Fatal(err)
	}
	traces, err := current.Traces()
	if err != nil || len(traces) != 1 || traces[0].Turn != 23 || traces[0].Call != bot.TraceCallCompact {
		t.Fatalf("usage rebound to latest or stale path: %+v %v", traces, err)
	}
	for _, path := range []string{bot.Dir(original.Root, "source"), filepath.Join(original.Root, "work", "source")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("old Bot/work revived %s: %v", path, err)
		}
	}
	if _, err := bot.Load(original.Root, "changed-target"); !errors.Is(err, bot.ErrNotFound) {
		t.Fatalf("request input was not frozen: %v", err)
	}
}

func TestBotForkRuntimeHTTPPartialSourceNoteTruth(t *testing.T) {
	k, endpoint, _ := botForkRuntimeFixture(t, false)
	// Fill the original source profile using the real bounded writer. The short
	// generated child handoff still fits; only the later source note fails.
	for i := 0; i < 10; i++ {
		err := k.Memory.Remember(context.Background(), memory.Fact{Key: "fill", Kind: memory.KindWork, Scope: memory.ScopeBot, Tier: memory.TierProfile, BotName: "source", WrittenBy: "source", Text: strings.Repeat("z", 900), Time: k.now()})
		if errors.Is(err, memory.ErrProfileFull) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	// The remaining space may accept another short fact; use a real profile
	// entry until no one-byte entry fits, without hard-coding an nth failure.
	for i := 0; i < memory.ProfileBytes; i++ {
		err := k.Memory.Remember(context.Background(), memory.Fact{Key: "pad", Kind: memory.KindWork, Scope: memory.ScopeBot, Tier: memory.TierProfile, BotName: "source", WrittenBy: "source", Text: "p", Time: k.now()})
		if errors.Is(err, memory.ErrProfileFull) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	model := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: provider.TextContent("PARTIAL-HANDOFF review the Friday release.")}}}
	k.Compactor = compact.LLM{P: model}
	runRoutineSource(t, k, nil, `{"command":"bot-fork reviewer --self --role review --group project"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
		reply, err := cliops.CallBotFork(ctx, endpoint, k.Bot.Root, tools.CallID(ctx), cliops.BotFork{Name: "reviewer", Role: "Review changes", Group: "project"})
		if err == nil || !reply.Created || reply.Stage != "source_note" || reply.HandoffParts != 1 || reply.HandoffTotal != 1 || reply.GroupID != "" || !reflect.DeepEqual(reply.Confirmed, []string{"owners", "binding", "rules", "handoff", "child_note"}) || reply.UncertainMemory == nil || reply.UncertainMemory.Bot != "source" || reply.UncertainMemory.Key != "fork-reviewer" || !strings.Contains(reply.Error, memory.ErrProfileFull.Error()) {
			t.Errorf("HTTP lost late partial truth: %+v %v", reply, err)
		}
		botForkReceipt(t, reply)
		return "completed", nil
	})
	if len(model.reqs) != 1 {
		t.Fatalf("late failure retried model %d times", len(model.reqs))
	}
	if facts := joinFacts(botForkFacts(t, k, "reviewer")); !strings.Contains(facts, "PARTIAL-HANDOFF") || !strings.Contains(facts, "Forked from source") {
		t.Fatalf("late partial rolled back acknowledged child facts: %q", facts)
	}
	if groups, err := (&peer.Bus{Root: k.Bot.Root}).ListGroups(); err != nil || len(groups) != 0 {
		t.Fatalf("group ran after source-note failure: %+v %v", groups, err)
	}
}

func TestBotForkRuntimeOriginalZeroSequenceAndCancellation(t *testing.T) {
	for _, cancelSource := range []bool{false, true} {
		t.Run(map[bool]string{false: "zero_is_original", true: "original_cancel_reaches_model"}[cancelSource], func(t *testing.T) {
			k, endpoint, _ := botForkRuntimeFixture(t, false)
			k.ensureTurn().fromSeq = 0
			k.Compactor = compact.LLM{P: botForkProvider{chat: func(ctx context.Context, req provider.Request) (provider.Message, error) {
				if turnSeqOf(ctx) != 0 {
					t.Error("actual zero sequence replaced")
				}
				k.turnMu.Lock()
				k.turn = &turn{fromSeq: 987}
				k.turnMu.Unlock()
				if cancelSource {
					if _, err := k.liveShell().Cancel(tools.CallID(ctx)); err != nil {
						return provider.Message{}, err
					}
					select {
					case <-ctx.Done():
						return provider.Message{}, ctx.Err()
					case <-time.After(time.Second):
						return provider.Message{}, errors.New("original cancellation did not reach model")
					}
				}
				return provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("ZERO-HANDOFF original zero turn")}, nil
			}}}
			runRoutineSource(t, k, nil, `{"command":"bot-fork reviewer --self --role review"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
				reply, err := cliops.CallBotFork(context.Background(), endpoint, k.Bot.Root, tools.CallID(ctx), cliops.BotFork{Name: "reviewer", Role: "Review changes"})
				if cancelSource {
					if err == nil || reply.Created || reply.Stage != "handoff" || !strings.Contains(reply.Error, context.Canceled.Error()) {
						t.Errorf("original cancel lost: %+v %v", reply, err)
					}
				} else if err != nil || !reply.Created {
					t.Errorf("actual zero source rejected: %+v %v", reply, err)
				}
				return "completed", nil
			})
			if !cancelSource {
				traces, err := k.Bot.Traces()
				if err != nil || len(traces) != 1 || traces[0].Turn != 0 {
					t.Fatalf("zero usage borrowed latest: %+v %v", traces, err)
				}
			}
		})
	}
}

func TestBotForkRuntimeActualChildNonprimary(t *testing.T) {
	p := &scripted{resps: []provider.Message{
		toolCall("child-fork", "sh", `{"command":"bot-fork child-created --self --role review"}`),
		{Role: provider.RoleAssistant, Content: provider.TextContent("child complete")},
	}}
	k, source, box := dispatchKernel(t, p)
	k.Memory = memory.Open(source.Root)
	k.Compactor = compact.LLM{P: &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: provider.TextContent("CHILD-HANDOFF review this project")}}}}
	if err := source.Append(bot.Entry{Time: time.Now(), Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("child fork project history")}}); err != nil {
		t.Fatal(err)
	}
	primary := botControlMake(t, source.Root, "primary", "u1")
	if err := bot.SetPrimary(source.Root, primary.Name()); err != nil {
		t.Fatal(err)
	}
	if err := k.StartOperations(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = k.Close() })
	endpoint := operationEndpoint(k)
	_ = k.ensureExec()
	local, ok := k.execSnapshot().(*exec.Local)
	if !ok {
		t.Fatalf("actual executor %T", k.execSnapshot())
	}
	originalTools := local.ToolsFor
	called := make(chan cliops.BotForkReply, 1)
	local.ToolsFor = func(req exec.StartRequest) []tools.Tool {
		list := originalTools(req)
		for i, tool := range list {
			if tool.Name() == "sh" {
				list[i] = routineSourceShell{run: func(ctx context.Context, _ json.RawMessage) (string, error) {
					identity, _ := ctx.Value(routineCallSourceKey{}).(routineCallSource)
					if !identity.child || identity.owner != nil || identity.trusted || identity.person != "" {
						t.Error("actual child gained main approval identity")
					}
					reply, err := cliops.CallBotFork(ctx, endpoint, source.Root, tools.CallID(ctx), cliops.BotFork{Name: "child-created", Role: "Review changes"})
					called <- reply
					return "completed", err
				}}
			}
		}
		return list
	}
	task, err := k.StartSubagent(context.Background(), SubagentRequest{Type: "dev", Task: "create the agreed review bot"})
	if err != nil {
		t.Fatal(err)
	}
	if done := waitEvent(t, box, exec.EvDone); done.Sender != task.ID {
		t.Fatalf("completion belongs to %s, wanted %s", done.Sender, task.ID)
	}
	select {
	case reply := <-called:
		if !reply.Created || reply.Stage != "complete" || reply.Error != "" {
			t.Fatalf("nonprimary actual child fork: %+v", reply)
		}
	default:
		t.Fatal("actual child did not call source HTTP")
	}
}

func TestBotForkRuntimeMissingFrozenTurn(t *testing.T) {
	k, endpoint, _ := botForkRuntimeFixture(t, false)
	owner := k.ensureTurn()
	tool := routineSourceShell{run: func(ctx context.Context, _ json.RawMessage) (string, error) {
		reply, err := cliops.CallBotFork(ctx, endpoint, k.Bot.Root, tools.CallID(ctx), cliops.BotFork{Name: "rejected", Role: "Review changes"})
		if err == nil || reply.Created || !strings.Contains(reply.Error, "source frozen turn unavailable") {
			t.Errorf("missing frozen context borrowed latest: %+v %v", reply, err)
		}
		return "completed", nil
	}}
	ctx := context.WithValue(context.Background(), turnKey{}, owner)
	p := &scripted{resps: []provider.Message{toolCall("malformed-prepared", "sh", `{}`)}}
	_, err := agent.Run(ctx, p, []tools.Tool{tool}, nil, agent.Options{OneRound: true, RunTool: func(ctx context.Context, tool tools.Tool, _ json.RawMessage, run func(context.Context) tools.CallResult) tools.CallResult {
		return k.shellJobs().RunCall(ctx, tools.CallRequest{Name: tool.Name(), Run: run, Prepare: func(ctx context.Context, _ string) (context.Context, func()) {
			server, release := k.holdSourceOperations()
			ctx = context.WithValue(ctx, routineCallSourceKey{}, routineCallSource{listener: server, owner: owner})
			return context.WithValue(ctx, turnKey{}, (*turn)(nil)), release
		}})
	}})
	if err != nil {
		t.Fatal(err)
	}
}
