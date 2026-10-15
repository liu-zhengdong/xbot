package kernel

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/notify"
	"xbot/internal/tools"
)

func (k *Kernel) modelTools() ([]tools.Tool, error) {
	list := append([]tools.Tool{}, link.CollectTools(k.Links)...)
	if k.hasLink(link.NameMemory) {
		list = append(list, rememberTool{k}, forgetTool{k}, recallTool{k})
	}
	if k.hasLink(link.NamePersona) {
		list = append(list, personaTool{k})
	}
	if k.hasLink(link.NameTimer) {
		list = append(list, routineCreate{k}, routineList{k}, routineUpdate{k}, routineDelete{k}, notifyTool{k})
	}
	if k.hasLink(link.NameContext) {
		list = append(list, compactTool{k}, historyTool{k}, expandTool{k})
	}
	if k.hasLink(link.NameWeb) {
		list = append(list, askTool{})
	}
	if k.hasLink(link.NamePeers) {
		list = append(list, listBotsTool{k}, messageBotTool{k}, listGroupsTool{k}, createGroupTool{k}, postGroupTool{k})
	}
	list = k.modelAgentTools(list)
	sort.Slice(list, func(i, j int) bool { return list[i].Name() < list[j].Name() })
	return list, nil
}

func (k *Kernel) hasLink(name string) bool {
	for _, l := range k.Links {
		if l != nil && l.Name() == name {
			return true
		}
	}
	return false
}

func (k *Kernel) timer() (*link.Timer, error) {
	for _, l := range k.Links {
		if t, ok := l.(*link.Timer); ok && t != nil {
			return t, nil
		}
	}
	return nil, errors.New("timer: link is not mounted")
}

func schema(props map[string]any, required []string) map[string]any {
	if props == nil {
		props = map[string]any{}
	}
	if required == nil {
		required = []string{}
	}
	return map[string]any{
		"type":                 "object",
		"properties":           props,
		"required":             required,
		"additionalProperties": false,
	}
}

func stringProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

func intProp(desc string) map[string]any {
	return map[string]any{"type": "integer", "description": desc}
}

type rememberTool struct{ k *Kernel }

func (rememberTool) Name() string { return "remember" }
func (rememberTool) Description() string {
	return "Store a fact. scope is person, bot, or bot-person. tier is profile (always loaded) or log (search only)."
}
func (rememberTool) Parameters() any {
	return schema(map[string]any{
		"fact":  stringProp("the fact, one line"),
		"scope": stringProp("person, bot, or bot-person"),
		"tier":  stringProp("profile or log; default profile"),
	}, []string{"fact", "scope"})
}
func (rememberTool) Confirm(json.RawMessage) (bool, string, error) { return false, "", nil }
func (t rememberTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Fact  string `json:"fact"`
		Scope string `json:"scope"`
		Tier  string `json:"tier"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	if t.k.Memory == nil {
		return "", errors.New("memory: no store")
	}
	fact := memory.Fact{
		Scope:     memory.Scope(in.Scope),
		Tier:      memory.Tier(in.Tier),
		Text:      in.Fact,
		PersonID:  t.k.turnSender,
		BotName:   t.k.Bot.Name(),
		WrittenBy: t.k.Bot.Name(),
		Time:      t.k.now(),
	}
	if fact.Tier == "" {
		fact.Tier = memory.TierProfile
	}
	if !t.k.turnTrusted && (fact.Scope == memory.ScopePerson || fact.Scope == memory.ScopeBotPerson) {
		if !strings.Contains(strings.ToLower(fact.Text), "untrusted") {
			fact.Text += " (untrusted, recorded by " + t.k.Bot.Name() + ")"
		}
	}
	if err := t.k.Memory.Remember(ctx, fact); err != nil {
		return "", err
	}
	return "remembered", nil
}

type forgetTool struct{ k *Kernel }

func (forgetTool) Name() string { return "forget" }
func (forgetTool) Description() string {
	return "Delete a fact whose text matches exactly, in the given scope."
}
func (forgetTool) Parameters() any {
	return schema(map[string]any{
		"fact":  stringProp("exact fact text"),
		"scope": stringProp("person, bot, or bot-person"),
	}, []string{"fact", "scope"})
}
func (forgetTool) Confirm(json.RawMessage) (bool, string, error) { return false, "", nil }
func (t forgetTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Fact  string `json:"fact"`
		Scope string `json:"scope"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	if t.k.Memory == nil {
		return "", errors.New("memory: no store")
	}
	q := memory.Query{Scope: memory.Scope(in.Scope), BotName: t.k.Bot.Name(), PersonID: t.k.turnSender}
	if err := t.k.Memory.Forget(ctx, q, in.Fact); err != nil {
		return "", err
	}
	return "forgot", nil
}

type recallTool struct{ k *Kernel }

func (recallTool) Name() string { return "recall" }
func (recallTool) Description() string {
	return "Search memory. scope is optional; empty searches the three scopes visible to this sender."
}
func (recallTool) Parameters() any {
	return schema(map[string]any{
		"query": stringProp("keywords"),
		"scope": stringProp("optional person, bot, or bot-person"),
	}, []string{"query"})
}
func (recallTool) Confirm(json.RawMessage) (bool, string, error) { return false, "", nil }
func (t recallTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Query string `json:"query"`
		Scope string `json:"scope"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	if t.k.Memory == nil {
		return "", errors.New("memory: no store")
	}
	scopes := []memory.Scope{memory.ScopePerson, memory.ScopeBot, memory.ScopeBotPerson}
	if in.Scope != "" {
		scopes = []memory.Scope{memory.Scope(in.Scope)}
	}
	var facts []memory.Fact
	for _, scope := range scopes {
		got, err := t.k.Memory.Recall(ctx, memory.Query{
			Scope: scope, BotName: t.k.Bot.Name(), PersonID: t.k.turnSender, Text: in.Query,
		})
		if err != nil {
			return "", err
		}
		facts = append(facts, got...)
	}
	facts = memory.Resolve(memory.Visible(facts, t.k.Bot.Name(), t.k.turnSender))
	if len(facts) == 0 {
		return "no matching facts", nil
	}
	var b strings.Builder
	for _, f := range facts {
		fmt.Fprintf(&b, "- [%s/%s] %s\n", f.Scope, f.Tier, f.Text)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

type personaTool struct{ k *Kernel }

func (personaTool) Name() string { return "propose_persona" }
func (personaTool) Description() string {
	return "Propose a new persona. It is saved only after the owner confirms, and applies on the next wake."
}
func (personaTool) Parameters() any {
	return schema(map[string]any{"text": stringProp("the full persona")}, []string{"text"})
}
func (personaTool) Confirm(json.RawMessage) (bool, string, error) {
	return true, "update persona", nil
}
func (t personaTool) Run(_ context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	if err := t.k.Bot.WritePersona(in.Text); err != nil {
		return "", err
	}
	return "persona saved; it applies on the next wake", nil
}

type notifyTool struct{ k *Kernel }

func (notifyTool) Name() string { return "notify_owner" }
func (notifyTool) Description() string {
	return "Leave a notification for the owner. Use this on a timer wake when something is worth their attention."
}
func (notifyTool) Parameters() any {
	return schema(map[string]any{"text": stringProp("what the owner should see")}, []string{"text"})
}
func (notifyTool) Confirm(json.RawMessage) (bool, string, error) { return false, "", nil }
func (t notifyTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	text := strings.TrimSpace(in.Text)
	if text == "" {
		return "", errors.New("notify: empty text")
	}
	if _, err := notify.Open(t.k.Bot.OutboxPath()).Append(text, t.k.now()); err != nil {
		return "", err
	}
	desk := t.k.Desktop
	if desk == nil {
		desk = notify.Desktop
	}
	_ = desk(ctx, t.k.Bot.Name(), text)
	return "notified the owner", nil
}

type routineCreate struct{ k *Kernel }

func (routineCreate) Name() string { return "routine_create" }
func (routineCreate) Description() string {
	return "Create a routine: a prompt plus a 5-field cron expression. The minimum gap is 5 minutes."
}
func (routineCreate) Parameters() any {
	return schema(map[string]any{
		"id":     stringProp("optional id"),
		"name":   stringProp("short name"),
		"prompt": stringProp("what to consider when the routine fires"),
		"cron":   stringProp("5-field cron, minimum gap 5 minutes"),
	}, []string{"name", "prompt", "cron"})
}
func (routineCreate) Confirm(json.RawMessage) (bool, string, error) {
	return true, "create routine", nil
}
func (t routineCreate) Run(_ context.Context, args json.RawMessage) (string, error) {
	var in struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Prompt string `json:"prompt"`
		Cron   string `json:"cron"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	tm, err := t.k.timer()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(in.ID) == "" {
		var b [4]byte
		if _, err := rand.Read(b[:]); err != nil {
			return "", err
		}
		in.ID = "r" + hex.EncodeToString(b[:])
	}
	r := link.Routine{ID: in.ID, Name: in.Name, Prompt: in.Prompt, Cron: in.Cron, Enabled: true}
	if err := tm.Save(r); err != nil {
		return "", err
	}
	return "created routine " + r.ID, nil
}

type routineList struct{ k *Kernel }

func (routineList) Name() string        { return "routine_list" }
func (routineList) Description() string { return "List this bot's routines." }
func (routineList) Parameters() any {
	return schema(map[string]any{}, nil)
}
func (routineList) Confirm(json.RawMessage) (bool, string, error) { return false, "", nil }
func (t routineList) Run(context.Context, json.RawMessage) (string, error) {
	tm, err := t.k.timer()
	if err != nil {
		return "", err
	}
	list, err := tm.List()
	if err != nil {
		return "", err
	}
	if len(list) == 0 {
		return "no routines", nil
	}
	var b strings.Builder
	for _, r := range list {
		fmt.Fprintf(&b, "- %s %q cron %s enabled %v\n", r.ID, r.Name, r.Cron, r.Enabled)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

type routineUpdate struct{ k *Kernel }

func (routineUpdate) Name() string { return "routine_update" }
func (routineUpdate) Description() string {
	return "Update a routine's name, prompt, cron, or enabled flag."
}
func (routineUpdate) Parameters() any {
	return schema(map[string]any{
		"id":      stringProp("routine id"),
		"name":    stringProp("optional name"),
		"prompt":  stringProp("optional prompt"),
		"cron":    stringProp("optional cron"),
		"enabled": map[string]any{"type": "boolean", "description": "optional enabled flag"},
	}, []string{"id"})
}
func (routineUpdate) Confirm(json.RawMessage) (bool, string, error) {
	return true, "update routine", nil
}
func (t routineUpdate) Run(_ context.Context, args json.RawMessage) (string, error) {
	var in struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Prompt  string `json:"prompt"`
		Cron    string `json:"cron"`
		Enabled *bool  `json:"enabled"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	tm, err := t.k.timer()
	if err != nil {
		return "", err
	}
	r, err := tm.Load(in.ID)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(in.Name) != "" {
		r.Name = in.Name
	}
	if strings.TrimSpace(in.Prompt) != "" {
		r.Prompt = in.Prompt
	}
	if strings.TrimSpace(in.Cron) != "" {
		r.Cron = in.Cron
	}
	if in.Enabled != nil {
		r.Enabled = *in.Enabled
	}
	if err := tm.Save(r); err != nil {
		return "", err
	}
	return "updated routine " + r.ID, nil
}

type routineDelete struct{ k *Kernel }

func (routineDelete) Name() string        { return "routine_delete" }
func (routineDelete) Description() string { return "Delete a routine." }
func (routineDelete) Parameters() any {
	return schema(map[string]any{"id": stringProp("routine id")}, []string{"id"})
}
func (routineDelete) Confirm(json.RawMessage) (bool, string, error) {
	return true, "delete routine", nil
}
func (t routineDelete) Run(_ context.Context, args json.RawMessage) (string, error) {
	var in struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	tm, err := t.k.timer()
	if err != nil {
		return "", err
	}
	if err := tm.Delete(in.ID); err != nil {
		return "", err
	}
	return "deleted routine " + in.ID, nil
}

type compactTool struct{ k *Kernel }

func (compactTool) Name() string { return "compact" }
func (compactTool) Description() string {
	return "Summarize a finished transcript range. from and to are inclusive sequence numbers. The last turns stay raw, and a tool-call pair cannot be split."
}
func (compactTool) Parameters() any {
	return schema(map[string]any{
		"from":  intProp("first sequence number"),
		"to":    intProp("last sequence number"),
		"focus": stringProp("what the summary must preserve"),
	}, []string{"from", "to"})
}
func (compactTool) Confirm(json.RawMessage) (bool, string, error) { return false, "", nil }
func (t compactTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		From  int    `json:"from"`
		To    int    `json:"to"`
		Focus string `json:"focus"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	return t.k.CompactRange(ctx, in.From, in.To, in.Focus)
}

type historyTool struct{ k *Kernel }

func (historyTool) Name() string { return "recall_history" }
func (historyTool) Description() string {
	return "Search the raw transcript. Results include stable sequence numbers."
}
func (historyTool) Parameters() any {
	return schema(map[string]any{"query": stringProp("keywords")}, []string{"query"})
}
func (historyTool) Confirm(json.RawMessage) (bool, string, error) { return false, "", nil }
func (t historyTool) Run(_ context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	return t.k.RecallHistory(in.Query)
}

type expandTool struct{ k *Kernel }

func (expandTool) Name() string { return "expand" }
func (expandTool) Description() string {
	return "Return the raw transcript text for an inclusive sequence range, including the range a summary block covers."
}
func (expandTool) Parameters() any {
	return schema(map[string]any{
		"from": intProp("first sequence number"),
		"to":   intProp("last sequence number"),
	}, []string{"from", "to"})
}
func (expandTool) Confirm(json.RawMessage) (bool, string, error) { return false, "", nil }
func (t expandTool) Run(_ context.Context, args json.RawMessage) (string, error) {
	var in struct {
		From int `json:"from"`
		To   int `json:"to"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	return t.k.Expand(in.From, in.To)
}
