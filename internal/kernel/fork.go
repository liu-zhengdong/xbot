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
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/peer"
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

// ForkResult is the bot that was created, and the group id when one was requested.
type ForkResult struct {
	Name    string
	GroupID string
}

// Fork creates a bot that starts from a handoff, not a copy of this bot.
// The new bot gets the role as its persona, an empty transcript, and this bot's
// provider binding. Person-scope memory is already shared and is not copied.
// Each bot records the other so the new one asks for details with message_bot.
func (k *Kernel) Fork(ctx context.Context, req ForkRequest) (ForkResult, error) {
	if k == nil || k.Bot == nil {
		return ForkResult{}, errors.New("fork: no bot")
	}
	if k.Memory == nil {
		return ForkResult{}, errors.New("fork: no memory store")
	}
	name := strings.TrimSpace(req.Name)
	role := strings.TrimSpace(req.Role)
	group := strings.TrimSpace(req.Group)
	if !bot.ValidName(name) {
		return ForkResult{}, fmt.Errorf("fork: invalid name %q", req.Name)
	}
	if name == k.Bot.Name() {
		return ForkResult{}, errors.New("fork: a bot cannot fork onto itself")
	}
	if role == "" {
		return ForkResult{}, errors.New("fork: role is required")
	}
	if len(role) > handoffBytes {
		return ForkResult{}, errors.New("fork: role is too long")
	}
	if group != "" && (len(group) > 48 || strings.ContainsAny(group, "/\\\n")) {
		return ForkResult{}, errors.New("fork: bad group name")
	}
	owners := append([]string(nil), k.Bot.Config().Owners...)
	if len(owners) == 0 {
		return ForkResult{}, errors.New("fork: source has no owner, so the new bot could not message it")
	}
	if _, err := bot.Load(k.Bot.Root, name); err == nil {
		return ForkResult{}, fmt.Errorf("bot %q: %w", name, bot.ErrExists)
	} else if !errors.Is(err, bot.ErrNotFound) {
		return ForkResult{}, err
	}

	summary, err := k.handoff(ctx, role)
	if err != nil {
		return ForkResult{}, err
	}
	links := append([]string(nil), k.Bot.Config().Links...)
	child, err := bot.Create(k.Bot.Root, name, role, links)
	if err != nil {
		return ForkResult{}, err
	}
	if err := child.SetOwners(owners); err != nil {
		return ForkResult{}, err
	}
	if err := child.InheritBinding(k.Bot.Config()); err != nil {
		return ForkResult{}, err
	}
	if err := child.GrantLink(link.NamePeers); err != nil {
		return ForkResult{}, err
	}
	if err := k.Bot.GrantLink(link.NamePeers); err != nil {
		return ForkResult{}, err
	}
	rules, err := k.Bot.ReadRules(DefaultRules())
	if err != nil {
		return ForkResult{}, err
	}
	if err := child.WriteRules(rules); err != nil {
		return ForkResult{}, err
	}
	if err := k.storeHandoff(ctx, child.Name(), summary); err != nil {
		return ForkResult{}, err
	}
	note := fmt.Sprintf("Forked from %s for this role. Ask %s for details with message_bot. You cannot read %s's transcript.", k.Bot.Name(), k.Bot.Name(), k.Bot.Name())
	if err := k.rememberBot(ctx, child.Name(), "fork-source", note); err != nil {
		return ForkResult{}, err
	}
	srcNote := fmt.Sprintf("Forked %s for role %q. Reach them with message_bot. They cannot read your transcript.", name, clipNote(role, 240))
	if err := k.rememberBot(ctx, k.Bot.Name(), "fork-"+name, srcNote); err != nil {
		return ForkResult{}, err
	}
	out := ForkResult{Name: name}
	if group != "" {
		g, err := (&peer.Bus{Root: k.Bot.Root, Now: k.now}).CreateGroup(k.Bot.Name(), group, []string{name})
		if err != nil {
			return ForkResult{}, err
		}
		out.GroupID = g.ID
	}
	return out, nil
}

func (k *Kernel) handoff(ctx context.Context, role string) (string, error) {
	entries, err := k.Bot.Entries()
	if err != nil {
		return "", err
	}
	st, err := k.Bot.LoadState()
	if err != nil {
		return "", err
	}
	st = migrate(st)
	facts, err := k.Memory.Recall(ctx, memory.Query{Scope: memory.ScopeBot, BotName: k.Bot.Name(), Tier: memory.TierProfile})
	if err != nil {
		return "", err
	}
	turns := project(entries, st.Blocks)
	if len(turns) == 0 && len(facts) == 0 {
		return fmt.Sprintf("Handoff from %s. No transcript yet. New role: %s", k.Bot.Name(), oneLine(role)), nil
	}
	if k.Compactor == nil {
		return "", errors.New("fork: no compactor to write the handoff")
	}
	persona, err := k.Bot.ReadPersona()
	if err != nil {
		return "", err
	}
	var prior strings.Builder
	prior.WriteString("Write a handoff for a new bot. Do not paste the transcript. ")
	prior.WriteString("Cover project background, key decisions, and context the new role needs.\n")
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
	focus := fmt.Sprintf("Handoff for role %q. Project background, key decisions, and context this role needs.", role)
	var text string
	if f, ok := k.Compactor.(focusedCompactor); ok {
		text, err = f.CompactFocus(ctx, prior.String(), focus, turns)
	} else {
		text, err = k.Compactor.Compact(ctx, prior.String()+"\nFocus: "+focus, turns)
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

func (k *Kernel) storeHandoff(ctx context.Context, botName, text string) error {
	parts := splitFact(oneLine(text), factBytes)
	if len(parts) == 0 {
		return errors.New("fork: empty handoff")
	}
	for i, part := range parts {
		key := "handoff"
		if i > 0 {
			key = fmt.Sprintf("handoff-%d", i+1)
		}
		if err := k.rememberBot(ctx, botName, key, part); err != nil {
			return err
		}
	}
	return nil
}

func (k *Kernel) rememberBot(ctx context.Context, botName, key, text string) error {
	return k.Memory.Remember(ctx, memory.Fact{
		Key:       key,
		Kind:      memory.KindWork,
		Scope:     memory.ScopeBot,
		Tier:      memory.TierProfile,
		BotName:   botName,
		WrittenBy: k.Bot.Name(),
		Text:      text,
		Time:      k.now(),
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
	if s == "" {
		return nil
	}
	var out []string
	for s != "" {
		if len(s) <= n {
			out = append(out, s)
			break
		}
		cut := n
		for cut > 0 && !utf8.ValidString(s[:cut]) {
			cut--
		}
		if sp := strings.LastIndex(s[:cut], " "); sp > n/2 {
			cut = sp
		}
		part := strings.TrimSpace(s[:cut])
		if part == "" {
			return nil
		}
		out = append(out, part)
		s = strings.TrimSpace(s[cut:])
	}
	return out
}

type forkBotTool struct{ k *Kernel }

func (forkBotTool) Name() string { return "fork_bot" }
func (forkBotTool) Description() string {
	return "Create a bot with its own role and an empty transcript. You write a handoff (project background, key decisions, context for that role) into its bot-scope memory. Person memory is not copied. It cannot read your transcript and will ask you with message_bot. Pass group to also create a group containing both of you."
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
