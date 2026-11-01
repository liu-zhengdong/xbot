package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"xbot/internal/bot"
	"xbot/internal/clip"
	"xbot/internal/inbox"
	"xbot/internal/memory"
	"xbot/internal/peer"
	"xbot/internal/provider"
)

const (
	handoffBytes = 8 << 10
	factBytes    = 960
)

// ForkRequest names the new bot and the role it should take.
// Group is optional. When set, both bots are placed in a new group of that name.
type ForkRequest struct {
	Name  string
	Role  string
	Group string
}

// ForkResult retains acknowledgements from the original fork write sequence.
type ForkResult struct {
	Name            string
	GroupID         string
	Created         bool
	CreateStage     string
	Confirmed       []string
	HandoffParts    int
	HandoffTotal    int
	UncertainMemory *ForkMemoryAttempt
	Stage           string
}

// ForkMemoryAttempt identifies a failed Remember that may already have written.
type ForkMemoryAttempt struct {
	Bot string
	Key string
}

// Fork preserves the legacy empty result on error.
func (k *Kernel) Fork(ctx context.Context, req ForkRequest) (ForkResult, error) {
	out, err := k.ForkWithResult(ctx, req)
	if err != nil {
		return ForkResult{}, err
	}
	return out, nil
}

// ForkWithResult creates a bot from a handoff, retaining each original nil
// acknowledgement. Person-scope memory is shared and is not copied.
func (k *Kernel) ForkWithResult(ctx context.Context, req ForkRequest) (ForkResult, error) {
	name := strings.TrimSpace(req.Name)
	out := ForkResult{Name: name, Stage: "validation"}
	if k == nil || k.Bot == nil {
		return out, errors.New("fork: no bot")
	}
	if k.Memory == nil {
		return out, errors.New("fork: no memory store")
	}
	role := strings.TrimSpace(req.Role)
	group := strings.TrimSpace(req.Group)
	if !bot.ValidName(name) {
		return out, fmt.Errorf("fork: invalid name %q", req.Name)
	}
	if name == k.currentName() {
		return out, errors.New("fork: a bot cannot fork onto itself")
	}
	if role == "" {
		return out, errors.New("fork: role is required")
	}
	if len(role) > handoffBytes {
		return out, errors.New("fork: role is too long")
	}
	if group != "" && (len(group) > 48 || strings.ContainsAny(group, "/\\\n")) {
		return out, errors.New("fork: bad group name")
	}
	var owners []string
	var root string
	if err := k.withCurrentIO(func(b *bot.Bot, _ *inbox.Inbox) error {
		owners = append([]string(nil), b.Config().Owners...)
		root = b.Root
		return nil
	}); err != nil {
		return out, err
	}
	if len(owners) == 0 {
		return out, errors.New("fork: source has no owner, so the new bot could not message it")
	}
	if _, err := bot.Load(root, name); err == nil {
		return out, fmt.Errorf("bot %q: %w", name, bot.ErrExists)
	} else if !errors.Is(err, bot.ErrNotFound) {
		return out, err
	}

	out.Stage = "handoff"
	summary, err := k.handoff(ctx, role)
	if err != nil {
		return out, err
	}
	parts := splitFact(oneLine(summary), factBytes)
	out.HandoffTotal = len(parts)
	out.Stage = "create"
	var currentCfg bot.Config
	if err := k.withCurrentIO(func(b *bot.Bot, _ *inbox.Inbox) error {
		currentCfg = b.Config()
		root = b.Root
		return nil
	}); err != nil {
		return out, err
	}
	child, err := bot.Create(root, name, role, append([]string(nil), currentCfg.Links...))
	if err != nil {
		var committed *bot.CreateCommitError
		if errors.As(err, &committed) {
			out.Created, out.CreateStage = committed.Applied, committed.Stage
		}
		return out, err
	}
	out.Created = true
	out.Stage = "owners"
	if err := child.SetOwners(owners); err != nil {
		return out, err
	}
	out.Confirmed = append(out.Confirmed, "owners")
	out.Stage = "binding"
	if err := child.InheritBinding(currentCfg); err != nil {
		return out, err
	}
	out.Confirmed = append(out.Confirmed, "binding")
	out.Stage = "rules_read"
	rules, err := k.botRules()
	if err != nil {
		return out, err
	}
	out.Stage = "rules"
	if err := child.WriteRules(rules); err != nil {
		return out, err
	}
	out.Confirmed = append(out.Confirmed, "rules")
	// The child target is fixed. Its author and source note use the current
	// source after the handoff model call; the source write below stays routed.
	var sourceName string
	if err := k.withCurrentIO(func(b *bot.Bot, _ *inbox.Inbox) error {
		sourceName = b.Name()
		return nil
	}); err != nil {
		return out, err
	}
	mem := k.Memory
	rememberChild := func(key, text string) error {
		err := mem.Remember(ctx, memory.Fact{
			Key: key, Kind: memory.KindWork, Scope: memory.ScopeBot,
			Tier: memory.TierProfile, BotName: child.Name(), WrittenBy: sourceName,
			Text: text, Time: k.now(),
		})
		if err != nil {
			out.UncertainMemory = &ForkMemoryAttempt{Bot: child.Name(), Key: key}
		}
		return err
	}
	out.Stage = "handoff_memory"
	if len(parts) == 0 {
		return out, errors.New("fork: empty handoff")
	}
	for i, part := range parts {
		key := "handoff"
		if i > 0 {
			key = fmt.Sprintf("handoff-%d", i+1)
		}
		if err := rememberChild(key, part); err != nil {
			return out, err
		}
		out.HandoffParts++
	}
	out.Confirmed = append(out.Confirmed, "handoff")
	out.Stage = "child_note"
	note := fmt.Sprintf("Forked from %s for this role. Ask %s for details with peer-message TARGET --self --text TEXT. You cannot read %s's transcript.", sourceName, sourceName, sourceName)
	if err := rememberChild("fork-source", note); err != nil {
		return out, err
	}
	out.Confirmed = append(out.Confirmed, "child_note")
	out.Stage = "source_note"
	srcNote := fmt.Sprintf("Forked %s for role %q. Reach them with peer-message TARGET --self --text TEXT. They cannot read your transcript.", name, clipNote(role, 240))
	if err := k.withCurrentIO(func(b *bot.Bot, _ *inbox.Inbox) error {
		key := "fork-" + name
		err := mem.Remember(ctx, memory.Fact{
			Key: key, Kind: memory.KindWork, Scope: memory.ScopeBot,
			Tier: memory.TierProfile, BotName: b.Name(), WrittenBy: b.Name(),
			Text: srcNote, Time: k.now(),
		})
		if err != nil {
			out.UncertainMemory = &ForkMemoryAttempt{Bot: b.Name(), Key: key}
		}
		return err
	}); err != nil {
		return out, err
	}
	out.Confirmed = append(out.Confirmed, "source_note")
	if group != "" {
		out.Stage = "group"
		if err := k.withCurrentIO(func(b *bot.Bot, _ *inbox.Inbox) error {
			sourceName, root = b.Name(), b.Root
			return nil
		}); err != nil {
			return out, err
		}
		g, err := (&peer.Bus{Root: root, Now: k.now}).CreateGroup(sourceName, group, []string{name})
		if err != nil {
			return out, err
		}
		out.GroupID = g.ID
		out.Confirmed = append(out.Confirmed, "group")
	}
	out.Stage = "complete"
	return out, nil
}

func (k *Kernel) handoff(ctx context.Context, role string) (string, error) {
	var prior strings.Builder
	var turns []provider.Message
	var fallback string
	focus := fmt.Sprintf("Handoff for role %q. Project background, key decisions, and context this role needs.", role)
	err := k.withCurrentIO(func(b *bot.Bot, _ *inbox.Inbox) error {
		st, err := b.LoadState()
		if err != nil {
			return err
		}
		st = migrate(st)
		lines, err := b.UncoveredLines(st.Blocks)
		if err != nil {
			return err
		}
		entries := make([]bot.Entry, 0, len(lines))
		seqs := make([]int, 0, len(lines))
		for _, ln := range lines {
			entries = append(entries, ln.Entry)
			seqs = append(seqs, ln.Seq)
		}
		facts, err := k.Memory.Recall(ctx, memory.Query{Scope: memory.ScopeBot, BotName: b.Name(), Tier: memory.TierProfile})
		if err != nil {
			return err
		}
		turns = projected(st.Blocks, entries, seqs)
		if len(turns) == 0 && len(facts) == 0 {
			fallback = fmt.Sprintf("Handoff from %s. No transcript yet. New role: %s", b.Name(), oneLine(role))
			return nil
		}
		if k.Compactor == nil {
			return errors.New("fork: no compactor to write the handoff")
		}
		persona, err := b.ReadPersona()
		if err != nil {
			return err
		}
		prior.WriteString("Write a handoff for a new bot whose only job is the new role. Do not paste the transcript. ")
		prior.WriteString("Keep the project background, decisions, and context that role needs, and leave out anything it will not use.\n")
		prior.WriteString("New role: ")
		prior.WriteString(oneLine(role))
		prior.WriteByte('\n')
		if strings.TrimSpace(persona) != "" {
			prior.WriteString("Source persona: ")
			prior.WriteString(oneLine(persona))
			prior.WriteByte('\n')
		}
		if len(facts) > 0 {
			prior.WriteString("Source bot memory:\n")
			for _, f := range facts {
				prior.WriteString(oneLine(f.Text))
				prior.WriteByte('\n')
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if fallback != "" {
		return fallback, nil
	}
	var text string
	if f, ok := k.compactor(turnSeqOf(ctx)).(focusedCompactor); ok {
		text, err = f.CompactFocus(ctx, prior.String(), focus, turns)
	} else {
		text, err = k.compactor(turnSeqOf(ctx)).Compact(ctx, prior.String()+"\nFocus: "+focus, turns)
	}
	if err != nil {
		return "", err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", errors.New("fork: empty handoff")
	}
	text, _ = clip.Bytes(text, handoffBytes)
	return text, nil
}

func (k *Kernel) rememberBot(ctx context.Context, botName, key, text string) error {
	return k.Memory.Remember(ctx, memory.Fact{
		Key: key, Kind: memory.KindWork, Scope: memory.ScopeBot,
		Tier: memory.TierProfile, BotName: botName, WrittenBy: k.currentName(),
		Text: text, Time: k.now(),
	})
}

func clipNote(s string, n int) string {
	s = oneLine(s)
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for !utf8.ValidString(s) && len(s) > 0 {
		s = s[:len(s)-1]
	}
	return strings.TrimSpace(s)
}

func splitFact(s string, n int) []string {
	s = strings.TrimSpace(s)
	if s == "" || n <= 0 {
		return nil
	}
	var out []string
	for s != "" {
		if len(s) <= n {
			out = append(out, s)
			break
		}
		cut := splitBoundary(s, n)
		part := strings.TrimSpace(s[:cut])
		if part == "" {
			return nil
		}
		out = append(out, part)
		s = strings.TrimSpace(s[cut:])
	}
	return out
}

// splitBoundary prefers a paragraph, then a line, then a word, and never a partial rune.
func splitBoundary(s string, n int) int {
	end := n
	if end > len(s) {
		end = len(s)
	}
	for end > 0 && !utf8.ValidString(s[:end]) {
		end--
	}
	if end == 0 {
		return 1
	}
	window := s[:end]
	if i := strings.LastIndex(window, "\n\n"); i > 0 {
		return i
	}
	if i := strings.LastIndex(window, "\n"); i > 0 {
		return i
	}
	if i := strings.LastIndex(window, " "); i > 0 {
		return i
	}
	return end
}

type forkBotTool struct{ k *Kernel }

func (forkBotTool) Name() string { return "fork_bot" }
func (forkBotTool) Description() string {
	return "Create a bot with its own role and an empty transcript. You write a handoff (project background, key decisions, context for that role) into its bot-scope memory. Person memory is not copied. It cannot read your transcript and will ask you with peer-message TARGET --self --text TEXT. Pass group to also create a group containing both of you."
}
func (forkBotTool) Parameters() any {
	return schema(map[string]any{
		"name":  stringProp("new bot name"),
		"role":  stringProp("the new bot's role and persona"),
		"group": stringProp("optional group name for both bots"),
	}, []string{"name", "role"})
}
func (forkBotTool) Confirm(json.RawMessage) (bool, string, error) { return false, "", nil }
func (t forkBotTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Name  string `json:"name"`
		Role  string `json:"role"`
		Group string `json:"group"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	res, err := t.k.Fork(ctx, ForkRequest{Name: in.Name, Role: in.Role, Group: in.Group})
	if err != nil {
		return "", err
	}
	if res.GroupID != "" {
		return fmt.Sprintf("forked %s group %s", res.Name, res.GroupID), nil
	}
	return "forked " + res.Name, nil
}
