package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"xbot/internal/bot"
	"xbot/internal/compact"
	"xbot/internal/memory"
	"xbot/internal/peer"
	"xbot/internal/provider"
)

func botForkProgressSource(t *testing.T, nonempty bool) *Kernel {
	t.Helper()
	root := t.TempDir()
	b, err := bot.Create(root, "source", "Source duties.", []string{"custom-link"})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"original-owner"}); err != nil {
		t.Fatal(err)
	}
	if nonempty {
		if err := b.Append(bot.Entry{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("RAW-PROJECT-CONTEXT")}}); err != nil {
			t.Fatal(err)
		}
	}
	return &Kernel{Bot: b, Memory: memory.Open(root), Now: func() time.Time { return time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC) }}
}

func botForkProgressDirectory(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
}

func botForkProgressFacts(t *testing.T, k *Kernel, name string) []memory.Fact {
	t.Helper()
	facts, err := k.Memory.Recall(context.Background(), memory.Query{Scope: memory.ScopeBot, Tier: memory.TierProfile, BotName: name})
	if err != nil {
		t.Fatal(err)
	}
	return facts
}

func botForkProgressConfirmed(t *testing.T, out ForkResult, expected ...string) {
	t.Helper()
	if !reflect.DeepEqual(out.Confirmed, expected) {
		t.Fatalf("confirmed %v, want %v (result %+v)", out.Confirmed, expected, out)
	}
}

// This is the production LLM compactor's actual Chat dependency, with no network.
type botForkProgressProvider struct {
	text  string
	check func(context.Context, provider.Request)
	calls int
}

func (p *botForkProgressProvider) Chat(ctx context.Context, req provider.Request, _ func(string)) (provider.Message, error) {
	p.calls++
	if p.check != nil {
		p.check(ctx, req)
	}
	return provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent(p.text)}, nil
}

func TestBotForkRuntimeProgressCapacity(t *testing.T) {
	k := botForkProgressSource(t, true)
	text := strings.Repeat("界", (8<<10)/3) + "ab"
	if len(text) != 8<<10 {
		t.Fatal("fixture must reach the real handoff limit")
	}
	p := &botForkProgressProvider{text: text, check: func(_ context.Context, req provider.Request) {
		if len(req.ToolDefinitions()) != 0 || !strings.Contains(req.Messages[1].Text(), "RAW-PROJECT-CONTEXT") {
			t.Fatal("handoff must use the original nonempty, no-tools LLM request")
		}
	}}
	k.Compactor = compact.LLM{P: p}
	out, err := k.ForkWithResult(context.Background(), ForkRequest{Name: " child ", Role: "Review the project.", Group: "later"})
	if !errors.Is(err, memory.ErrProfileFull) || p.calls != 1 || out.Name != "child" || !out.Created || out.Stage != "handoff_memory" || out.CreateStage != "" || out.GroupID != "" {
		t.Fatalf("actual 8KiB handoff / 4KiB memory cap: %+v, %v, calls %d", out, err, p.calls)
	}
	botForkProgressConfirmed(t, out, "owners", "binding", "rules")
	parts := splitFact(oneLine(text), factBytes)
	if out.HandoffTotal != len(parts) || out.HandoffParts <= 0 || out.HandoffParts >= out.HandoffTotal {
		t.Fatalf("actual split and partial acknowledgement counts: %+v, split %d", out, len(parts))
	}
	wantKey := fmt.Sprintf("handoff-%d", out.HandoffParts+1)
	if out.UncertainMemory == nil || *out.UncertainMemory != (ForkMemoryAttempt{Bot: "child", Key: wantKey}) {
		t.Fatalf("failed actual next attempt: %+v", out)
	}
	facts := botForkProgressFacts(t, k, "child")
	if len(facts) != out.HandoffParts {
		t.Fatalf("retained acknowledged facts: %d, result %+v", len(facts), out)
	}
	for i, fact := range facts {
		if fact.Text != parts[i] || !utf8.ValidString(fact.Text) || len(fact.Text) > 960 {
			t.Fatalf("unmodified 960-byte rune-safe part %d: %+v", i, fact)
		}
	}
	legacy, legacyErr := k.Fork(context.Background(), ForkRequest{Name: "legacy-child", Role: "Review the project."})
	if !errors.Is(legacyErr, memory.ErrProfileFull) || !reflect.DeepEqual(legacy, ForkResult{}) {
		t.Fatalf("legacy error result must stay empty: %+v, %v", legacy, legacyErr)
	}
}

func TestBotForkRuntimeProgressMemoryIndexCommitted(t *testing.T) {
	k := botForkProgressSource(t, true)
	const handoff = "MODEL-HANDOFF-ALREADY-IN-MARKDOWN"
	k.Compactor = &scriptCompactor{text: handoff}
	// The first Remember sees no markdown and reaches the real MD->index writes.
	index := filepath.Join(bot.Dir(k.Bot.Root, "child"), "memory", "memory.index.json")
	botForkProgressDirectory(t, index)
	out, err := k.ForkWithResult(context.Background(), ForkRequest{Name: "child", Role: "Review."})
	var renameErr *os.LinkError
	if err == nil || !errors.As(err, &renameErr) || renameErr.New != index || !out.Created || out.Stage != "handoff_memory" || out.HandoffParts != 0 || out.HandoffTotal != 1 || out.UncertainMemory == nil || *out.UncertainMemory != (ForkMemoryAttempt{Bot: "child", Key: "handoff"}) {
		t.Fatalf("index failure must remain uncertain: %+v, %v", out, err)
	}
	botForkProgressConfirmed(t, out, "owners", "binding", "rules")
	body, readErr := os.ReadFile(filepath.Join(filepath.Dir(index), "memory.md"))
	if readErr != nil || !strings.Contains(string(body), handoff) {
		t.Fatalf("real markdown commit before index failure: %q, %v", body, readErr)
	}
	if _, err := os.Stat(filepath.Join(bot.Dir(k.Bot.Root, "child"), "config.json")); err != nil {
		t.Fatal("created child must survive the memory failure", err)
	}
}

func TestBotForkRuntimeProgressCreateAndRulesFailures(t *testing.T) {
	for _, stage := range []string{"state", "persona", "rules_read", "rules"} {
		t.Run(stage, func(t *testing.T) {
			k := botForkProgressSource(t, false)
			childDir := bot.Dir(k.Bot.Root, "child")
			switch stage {
			case "state":
				botForkProgressDirectory(t, filepath.Join(childDir, "state.json"))
			case "persona":
				botForkProgressDirectory(t, filepath.Join(childDir, "persona.md"))
			case "rules_read":
				botForkProgressDirectory(t, k.Bot.RulesPath())
			case "rules":
				botForkProgressDirectory(t, filepath.Join(childDir, "rules.md"))
			}
			out, err := k.ForkWithResult(context.Background(), ForkRequest{Name: "child", Role: "Review."})
			if err == nil || !out.Created || out.Name != "child" || out.HandoffParts != 0 || out.HandoffTotal != 1 || out.UncertainMemory != nil || out.GroupID != "" {
				t.Fatalf("actual filesystem partial at %s: %+v, %v", stage, out, err)
			}
			if stage == "state" || stage == "persona" {
				var committed *bot.CreateCommitError
				if !errors.As(err, &committed) || !committed.Applied || committed.Stage != stage || out.Stage != "create" || out.CreateStage != stage {
					t.Fatalf("preserved typed CreateCommitError: %+v, %v", out, err)
				}
				botForkProgressConfirmed(t, out)
			} else {
				if out.Stage != stage || out.CreateStage != "" {
					t.Fatalf("exact rules failure stage: %+v", out)
				}
				botForkProgressConfirmed(t, out, "owners", "binding")
			}
			if _, err := bot.Load(k.Bot.Root, "child"); err != nil {
				t.Fatal("acknowledged child config must remain", err)
			}
		})
	}
}

func TestBotForkRuntimeProgressLateSourceAndGroupFailures(t *testing.T) {
	for _, stage := range []string{"source_note", "group"} {
		t.Run(stage, func(t *testing.T) {
			k := botForkProgressSource(t, true)
			k.Compactor = compact.LLM{P: &botForkProgressProvider{text: "A short handoff.", check: func(_ context.Context, _ provider.Request) {
				if stage == "source_note" {
					// Recall has already frozen the input; only the later source write fails.
					botForkProgressDirectory(t, filepath.Join(bot.Dir(k.Bot.Root, "source"), "memory", "memory.md"))
				} else {
					if err := os.WriteFile(filepath.Join(k.Bot.Root, "groups"), []byte("actual group directory conflict"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}}}
			out, err := k.ForkWithResult(context.Background(), ForkRequest{Name: "child", Role: "Review.", Group: "project"})
			if err == nil || !out.Created || out.Stage != stage || out.GroupID != "" || out.HandoffParts != 1 || out.HandoffTotal != 1 {
				t.Fatalf("late %s must retain prior acknowledgements: %+v, %v", stage, out, err)
			}
			if stage == "source_note" {
				botForkProgressConfirmed(t, out, "owners", "binding", "rules", "handoff", "child_note")
				if out.UncertainMemory == nil || *out.UncertainMemory != (ForkMemoryAttempt{Bot: "source", Key: "fork-child"}) {
					t.Fatalf("actual source attempt: %+v", out)
				}
			} else {
				botForkProgressConfirmed(t, out, "owners", "binding", "rules", "handoff", "child_note", "source_note")
				if out.UncertainMemory != nil {
					t.Fatalf("group error must not invent a failed memory attempt: %+v", out)
				}
			}
			if facts := botForkProgressFacts(t, k, "child"); len(facts) != 2 {
				t.Fatalf("child handoff and source recognition survived: %+v", facts)
			}
		})
	}
}

func TestBotForkRuntimeProgressSuccessAndOriginalTiming(t *testing.T) {
	k := botForkProgressSource(t, true)
	cfg := k.Bot.Config()
	cfg.Role, cfg.BackgroundMaxTime = bot.RoleDev, 7200
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bot.Dir(k.Bot.Root, "source"), "config.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	k.Bot, err = bot.Load(k.Bot.Root, "source")
	if err != nil {
		t.Fatal(err)
	}
	if err := k.Bot.WriteRules("Keep review findings actionable."); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := k.Memory.Remember(ctx, memory.Fact{Scope: memory.ScopePerson, Tier: memory.TierProfile, PersonID: "original-owner", Key: "shared", Text: "PERSON-SHARED", WrittenBy: "source", Time: k.now()}); err != nil {
		t.Fatal(err)
	}
	personPath := filepath.Join(k.Bot.Root, "people", "original-owner", "memory", "memory.md")
	personBefore, err := os.ReadFile(personPath)
	if err != nil {
		t.Fatal(err)
	}
	role := strings.Repeat("界", 2730) + "ab"
	p := &botForkProgressProvider{text: "ONLY-HANDOFF", check: func(_ context.Context, req provider.Request) {
		if len(req.ToolDefinitions()) != 0 || !strings.Contains(req.Messages[1].Text(), "Source duties.") || !strings.Contains(req.Messages[1].Text(), "Focus: Handoff for role") || !strings.Contains(req.Messages[1].Text(), role) {
			t.Fatal("original focused LLM input lost persona or complete role")
		}
		if err := k.Bot.SetOwners([]string{"new-owner"}); err != nil {
			t.Fatal(err)
		}
		if err := k.Bot.GrantLink("post-model-link"); err != nil {
			t.Fatal(err)
		}
		if err := k.Bot.InheritBinding(bot.Config{Provider: "post-provider", Model: "post-model", ContextLimit: 12345}); err != nil {
			t.Fatal(err)
		}
	}}
	k.Compactor = compact.LLM{P: p}
	out, err := k.ForkWithResult(ctx, ForkRequest{Name: " child ", Role: " \n" + role + "\n "})
	if err != nil || out.Name != "child" || !out.Created || out.Stage != "complete" || out.CreateStage != "" || out.HandoffParts != 1 || out.HandoffTotal != 1 || out.UncertainMemory != nil || out.GroupID != "" || p.calls != 1 {
		t.Fatalf("complete fork: %+v, %v", out, err)
	}
	botForkProgressConfirmed(t, out, "owners", "binding", "rules", "handoff", "child_note", "source_note")
	child, err := bot.Load(k.Bot.Root, "child")
	if err != nil {
		t.Fatal(err)
	}
	childCfg := child.Config()
	if !reflect.DeepEqual(childCfg.Owners, []string{"original-owner"}) || !reflect.DeepEqual(childCfg.Links, []string{"custom-link", "post-model-link"}) || childCfg.Provider != "post-provider" || childCfg.Model != "post-model" || childCfg.ContextLimit != 12345 {
		t.Fatalf("pre-model owners / post-model links and only three binding fields: %+v", childCfg)
	}
	if childCfg.Role != "" || childCfg.BackgroundMaxTime != 0 || childCfg.Working().BackgroundMaxTime != 45*time.Minute {
		t.Fatalf("duties must not copy source dev budget: %+v", childCfg)
	}
	if persona, err := child.ReadPersona(); err != nil || persona != role || len(persona) != 8<<10 {
		t.Fatalf("full 8KiB duties: length %d, %v", len(persona), err)
	}
	if rules, err := child.ReadRules(""); err != nil || rules != "Keep review findings actionable." {
		t.Fatalf("original source rules: %q, %v", rules, err)
	}
	if entries, err := child.Entries(); err != nil || len(entries) != 0 {
		t.Fatalf("empty child transcript: %+v, %v", entries, err)
	}
	if state, err := child.LoadState(); err != nil || len(state.Blocks) != 0 {
		t.Fatalf("empty child blocks: %+v, %v", state, err)
	}
	facts := botForkProgressFacts(t, k, "child")
	if len(facts) != 2 || !strings.Contains(joinFacts(facts), "ONLY-HANDOFF") || strings.Contains(joinFacts(facts), "PERSON-SHARED") {
		t.Fatalf("handoff and note without direct person copy: %+v", facts)
	}
	personAfter, err := os.ReadFile(personPath)
	if err != nil || !reflect.DeepEqual(personBefore, personAfter) {
		t.Fatalf("shared person store changed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(bot.Dir(k.Bot.Root, "child"), "people")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invented child person store: %v", err)
	}
}

type botForkProgressFallback struct {
	prior string
	turns []provider.Message
}

func (c *botForkProgressFallback) Compact(_ context.Context, prior string, turns []provider.Message) (string, error) {
	c.prior, c.turns = prior, turns
	return " FALLBACK-COMPACTOR-HANDOFF ", nil
}

func TestBotForkRuntimeProgressFallbackAndLegacySuccess(t *testing.T) {
	k := botForkProgressSource(t, true)
	c := &botForkProgressFallback{}
	k.Compactor = c
	// This compactor deliberately has no CompactFocus; the original fallback
	// receives the same projected input and focus appended to its prior.
	out, err := k.Fork(context.Background(), ForkRequest{Name: " child ", Role: "Review.", Group: "project"})
	if err != nil || out.Name != "child" || out.GroupID == "" || !out.Created || out.Stage != "complete" || !strings.Contains(c.prior, "Focus: Handoff for role") || len(c.turns) == 0 {
		t.Fatalf("legacy successful result and compact fallback: %+v, %v, prior %q", out, err, c.prior)
	}
	botForkProgressConfirmed(t, out, "owners", "binding", "rules", "handoff", "child_note", "source_note", "group")
	groups, err := (&peer.Bus{Root: k.Bot.Root}).ListGroups()
	if err != nil || len(groups) != 1 || groups[0].ID != out.GroupID || !reflect.DeepEqual(groups[0].Members, []string{"child", "source"}) {
		t.Fatalf("only actual group metadata acknowledged: %+v, %v", groups, err)
	}
	empty := botForkProgressSource(t, false)
	// Empty sources need no compactor. The original fallback includes the
	// entire role and has no final 8KiB clipping step.
	longRole := strings.Repeat("r", 8<<10)
	text, err := empty.handoff(context.Background(), longRole)
	if err != nil || len(text) <= 8<<10 || !strings.HasSuffix(text, longRole) || !strings.Contains(text, "No transcript yet") {
		t.Fatalf("original un-clipped empty fallback: bytes %d, %v", len(text), err)
	}
}
