package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"xbot/internal/link"
	"xbot/internal/peer"
	"xbot/internal/remote"
)

type listBotsTool struct{ k *Kernel }

func (listBotsTool) Name() string { return "list_bots" }
func (listBotsTool) Description() string {
	return "List other same-owner bots, including bot@device on paired devices. You cannot see bots owned by someone else."
}
func (listBotsTool) Parameters() any { return schema(nil, nil) }
func (listBotsTool) Confirm(json.RawMessage) (bool, string, error) {
	return false, "", nil
}
func (t listBotsTool) Run(context.Context, json.RawMessage) (string, error) {
	cfgs, err := peer.VisibleBots(t.k.Bot.Root, t.k.Bot.Name())
	if err != nil {
		return "", err
	}
	remotes, err := remote.VisibleBots(t.k.Bot.Root, t.k.Bot.Config().Owners, t.k.now())
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, cfg := range cfgs {
		persona := strings.TrimSpace(cfg.Persona)
		if len(persona) > 80 {
			persona = persona[:80]
		}
		b.WriteString(cfg.Name)
		if persona != "" {
			b.WriteString(" — ")
			b.WriteString(persona)
		}
		b.WriteByte('\n')
	}
	for _, remote := range remotes {
		b.WriteString(remote.String(t.k.now()))
		b.WriteByte('\n')
	}
	if b.Len() == 0 {
		return "no other bots", nil
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

type messageBotTool struct{ k *Kernel }

func (messageBotTool) Name() string { return "message_bot" }
func (messageBotTool) Description() string {
	return "Post a message into another same-owner bot's inbox. bot@device reaches the owner's bot on another paired device. They see it as untrusted data, not as their own instructions, and they do not see your private memory. Chains stop after a few hops."
}
func (messageBotTool) Parameters() any {
	return schema(map[string]any{
		"bot":  stringProp("bot name"),
		"text": stringProp("message text"),
	}, []string{"bot", "text"})
}
func (messageBotTool) Confirm(json.RawMessage) (bool, string, error) {
	return false, "", nil
}
func (t messageBotTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Bot  string `json:"bot"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	kt := t.k.turnFor(ctx)
	chain := kt.turnChain
	if chain == "" {
		chain = "ch_" + t.k.Bot.Name()
	}
	addr := strings.TrimSpace(in.Bot)
	var err error
	var queued bool
	if strings.Contains(addr, "@") {
		queued, err = remote.Send(ctx, t.k.Bot.Root, t.k.Bot.Name(), addr, in.Text, chain, kt.turnHops+1, t.k.now())
	} else {
		err = (&peer.Bus{Root: t.k.Bot.Root, Now: t.k.now}).Message(t.k.Bot.Name(), addr, in.Text, chain, kt.turnHops+1, t.k.now())
	}
	if err != nil {
		return "", err
	}
	if queued {
		return "gateway unreachable; message to " + addr + " queued and retried for up to 24 hours", nil
	}
	return "sent to " + strings.TrimSpace(in.Bot), nil
}

type listGroupsTool struct{ k *Kernel }

func (listGroupsTool) Name() string { return "list_groups" }
func (listGroupsTool) Description() string {
	return "List groups you belong to."
}
func (listGroupsTool) Parameters() any { return schema(nil, nil) }
func (listGroupsTool) Confirm(json.RawMessage) (bool, string, error) {
	return false, "", nil
}
func (t listGroupsTool) Run(context.Context, json.RawMessage) (string, error) {
	groups, err := (&peer.Bus{Root: t.k.Bot.Root}).ListGroups()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, g := range groups {
		if !hasMember(g.Members, t.k.Bot.Name()) {
			continue
		}
		b.WriteString(g.ID)
		b.WriteString(" ")
		b.WriteString(g.Name)
		b.WriteString(" [")
		b.WriteString(strings.Join(g.Members, ", "))
		b.WriteString("]\n")
	}
	if b.Len() == 0 {
		return "no groups", nil
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

type createGroupTool struct{ k *Kernel }

func (createGroupTool) Name() string { return "create_group" }
func (createGroupTool) Description() string {
	return "Create a group of same-owner bots. You are included. Members see group messages, not each other's private memory."
}
func (createGroupTool) Parameters() any {
	return schema(map[string]any{
		"name":    stringProp("display name"),
		"members": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "other bot names"},
	}, []string{"name", "members"})
}
func (createGroupTool) Confirm(json.RawMessage) (bool, string, error) {
	return false, "", nil
}
func (t createGroupTool) Run(_ context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Name    string   `json:"name"`
		Members []string `json:"members"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	g, err := (&peer.Bus{Root: t.k.Bot.Root, Now: t.k.now}).CreateGroup(t.k.Bot.Name(), in.Name, in.Members)
	if err != nil {
		return "", err
	}
	return "created " + g.ID + " " + g.Name, nil
}

type postGroupTool struct{ k *Kernel }

func (postGroupTool) Name() string { return "post_group" }
func (postGroupTool) Description() string {
	return "Post into a group. Every other member gets an untrusted inbox event. Their private memory is not included."
}
func (postGroupTool) Parameters() any {
	return schema(map[string]any{
		"id":   stringProp("group id"),
		"text": stringProp("message text"),
	}, []string{"id", "text"})
}
func (postGroupTool) Confirm(json.RawMessage) (bool, string, error) {
	return false, "", nil
}
func (t postGroupTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	kt := t.k.turnFor(ctx)
	chain := kt.turnChain
	if chain == "" {
		chain = "ch_" + t.k.Bot.Name()
	}
	err := (&peer.Bus{Root: t.k.Bot.Root, Now: t.k.now}).PostGroup(link.KindBot, t.k.Bot.Name(), in.ID, in.Text, chain, kt.turnHops+1, t.k.now())
	if err != nil {
		return "", err
	}
	return "posted to " + in.ID, nil
}

func hasMember(members []string, name string) bool {
	for _, m := range members {
		if m == name {
			return true
		}
	}
	return false
}

type botStatusTool struct{ k *Kernel }

func (botStatusTool) Name() string { return "bot_status" }
func (botStatusTool) Description() string {
	return "Read the status of same-owner bots: idle, running, or awaiting a reply, plus the last tool and message. Runs no model call. Pass bot for one bot; omit for all. steps adds up to 10 recent steps of the current turn. Another owner's bot is not found."
}
func (botStatusTool) Parameters() any {
	return schema(map[string]any{
		"bot":   stringProp("optional same-owner bot; omit for every bot"),
		"steps": intProp("optional recent steps to include, at most 10"),
	}, nil)
}
func (botStatusTool) Confirm(json.RawMessage) (bool, string, error) { return false, "", nil }
func (t botStatusTool) Run(_ context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Bot   string `json:"bot"`
		Steps int    `json:"steps"`
	}
	if len(args) > 0 && string(args) != "null" {
		if err := json.Unmarshal(args, &in); err != nil {
			return "", err
		}
	}
	name := strings.TrimSpace(in.Bot)
	list := t.k.BotStatuses()
	if name != "" {
		filtered := list[:0:0]
		for _, st := range list {
			if st.Bot == name {
				filtered = append(filtered, st)
			}
		}
		if len(filtered) == 0 {
			return "", fmt.Errorf("bot_status: %s is not a same-owner bot", name)
		}
		list = filtered
	}
	n := in.Steps
	if n > 10 {
		n = 10
	}
	var steps []Step
	if n > 0 {
		if name != "" {
			steps = t.k.BotSteps(name, n)
		} else if len(list) == 1 {
			steps = t.k.BotSteps(list[0].Bot, n)
		}
	}
	return botStatusText(list, steps), nil
}

type askTool struct{ k *Kernel }

func (askTool) Name() string { return "ask_user" }
func (askTool) Description() string {
	return "Ask the owner to pick one option. recommended is required and must exactly match one of options. Stop after this call. Their choice arrives as the next user message; after 30 minutes without a reply the recommended option is chosen automatically."
}
func (askTool) Parameters() any {
	return schema(map[string]any{
		"question":    stringProp("the question"),
		"recommended": stringProp("required: exact text of one of options; used automatically if the owner does not reply"),
		"options":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "two to six short choices"},
	}, []string{"question", "options", "recommended"})
}
func (askTool) Confirm(args json.RawMessage) (bool, string, error) {
	q, opts, _, err := parseAsk(args)
	if err != nil {
		return false, "", err
	}
	return false, formatAsk(q, opts), nil
}
func (t askTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	q, opts, recommended, err := parseAsk(args)
	if err != nil {
		return "", err
	}
	if err := t.k.askOwner(ctx, q, opts, recommended); err != nil {
		return "", err
	}
	return "waiting for the owner to choose", nil
}

func parseAsk(args json.RawMessage) (string, []string, string, error) {
	var in struct {
		Question    string   `json:"question"`
		Recommended string   `json:"recommended"`
		Options     []string `json:"options"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", nil, "", err
	}
	q := strings.TrimSpace(in.Question)
	var opts []string
	for _, o := range in.Options {
		o = strings.TrimSpace(o)
		if o != "" {
			opts = append(opts, o)
		}
	}
	if q == "" || len(opts) < 2 || len(opts) > 6 {
		return "", nil, "", errAsk
	}
	recommended := strings.TrimSpace(in.Recommended)
	found := false
	for _, o := range opts {
		if o == recommended {
			found = true
		}
	}
	if !found {
		return "", nil, "", errAsk
	}
	return q, opts, recommended, nil
}

func formatAsk(question string, options []string) string {
	var b strings.Builder
	b.WriteString(question)
	for i, o := range options {
		fmt.Fprintf(&b, "\n%d. %s", i+1, o)
	}
	return b.String()
}

var errAsk = askError{}

type askError struct{}

func (askError) Error() string {
	return "ask_user: need a question, 2 to 6 options, and recommended matching one option"
}
