package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"xbot/internal/link"
	"xbot/internal/review"
	"xbot/internal/tools"
)

// The tool registry is the one place a Bot's callable tools are listed (ADR
// 0035). Every tool registers once with its source and group. Only the hot
// ones and the tools door are declared in a request's tools[]; the rest are
// named in the environment's tool_index and reached through the door, the way
// Pi's mcp proxy does. Disclosure happens in tool results, never by changing
// tools[], so the declared set and its bytes stay put across a turn.

// toolExpose says how a registered tool reaches the model.
type toolExpose int

const (
	// exposeListed tools are named in tool_index and called through the door.
	exposeListed toolExpose = iota
	// exposeHot tools are declared in every request beside the door.
	exposeHot
)

// hotTools is the short fixed set declared beside the door; the shells are
// hot too. Everything else is listed.
var hotTools = map[string]bool{
	"send_message_to_user": true,
	"todo":                 true,
	"read_file":            true,
	"write_file":           true,
	"edit_file":            true,
	"web_fetch":            true,
}

type toolEntry struct {
	tool   tools.Tool
	source string // kernel | organ | link | mcp:<server>
	group  string
	expose toolExpose
	// deferred tools belong to a lazy Link not loaded this turn. The main
	// line reaches them through the door; a subagent, which has no door,
	// gets them only after links load.
	deferred bool
}

// pendingGroup is an installed MCP server with no tools listed this turn. The
// door starts it when the model describes the group or calls a tool of it.
type pendingGroup struct {
	id, summary string
	mcp         *link.MCP
}

type toolRegistry struct {
	entries []toolEntry
	pending []pendingGroup
}

// Register adds one tool under its name. A duplicate name is left for the
// agent to reject, as before.
func (r *toolRegistry) Register(t tools.Tool, source, group string, deferred bool) {
	expose := exposeListed
	if hotTools[t.Name()] || review.IsShellTool(t.Name()) {
		expose = exposeHot
	}
	r.entries = append(r.entries, toolEntry{tool: t, source: source, group: group, expose: expose, deferred: deferred})
}

func (r *toolRegistry) find(id string) (toolEntry, bool) {
	for _, e := range r.entries {
		if e.tool.Name() == id {
			return e, true
		}
	}
	return toolEntry{}, false
}

// loaded is every tool not deferred: what a subagent may declare.
func (r *toolRegistry) loaded() []tools.Tool {
	out := make([]tools.Tool, 0, len(r.entries))
	for _, e := range r.entries {
		if !e.deferred {
			out = append(out, e.tool)
		}
	}
	return out
}

// index is the short list for the environment: listed ids by group, and
// servers not started yet.
func (r *toolRegistry) index() map[string]any {
	groups := map[string][]string{}
	for _, e := range r.entries {
		if e.expose == exposeListed {
			groups[e.group] = append(groups[e.group], e.tool.Name())
		}
	}
	out := make(map[string]any, len(groups)+len(r.pending))
	for g, ids := range groups {
		sort.Strings(ids)
		out[g] = ids
	}
	for _, p := range r.pending {
		out[p.id] = "not started; describe it to list its tools"
	}
	return out
}

// kernelTools declares the tools the kernel implements. A row registers when
// on holds; nothing else appends kernel tools.
var kernelTools = []struct {
	group string
	on    func(k *Kernel, links []link.Link) bool
	tools func(k *Kernel) []tools.Tool
	// deferred, when set, says the row waits for links load (see toolEntry).
	deferred func(k *Kernel) bool
}{
	{"memory", granted(link.NameMemory), func(k *Kernel) []tools.Tool {
		return []tools.Tool{recallTool{k}, experienceRecordTool{k}, experienceSearchTool{k}}
	}, nil},
	{"bot_profile", granted(link.NamePersona), func(k *Kernel) []tools.Tool {
		return []tools.Tool{personaTool{k}, profileTool{k}}
	}, nil},
	{"routine", granted(link.NameTimer), func(k *Kernel) []tools.Tool {
		return []tools.Tool{routineCreate{k}, routineList{k}, routineUpdate{k}, routineDelete{k}}
	}, nil},
	// The owner sees only what the bot sends, so every bot can send.
	{"chat", always, func(k *Kernel) []tools.Tool { return []tools.Tool{sendMessageTool{k}} }, nil},
	{"chat", granted(link.NameWeb), func(k *Kernel) []tools.Tool { return []tools.Tool{askTool{k}} }, nil},
	{"context", granted(link.NameContext), func(k *Kernel) []tools.Tool {
		return []tools.Tool{compactTool{k}, historyTool{k}, expandTool{k}, detailTool{k}}
	}, nil},
	{"peers", granted(link.NamePeers), func(k *Kernel) []tools.Tool {
		return []tools.Tool{listBotsTool{k}, messageBotTool{k}, listGroupsTool{k}, groupHistoryTool{k}, createGroupTool{k}, postGroupTool{k}, forkBotTool{k}, botStatusTool{k}}
	}, nil},
	{"bots", func(k *Kernel, _ []link.Link) bool { return k.isPrimary() }, func(k *Kernel) []tools.Tool {
		return []tools.Tool{createBotTool{k}, deleteBotTool{k}, pinBotTool{k}, setPrimaryTool{k}}
	}, nil},
	{"links", always, func(k *Kernel) []tools.Tool { return []tools.Tool{linksTool{k}, kernelManualTool{}} }, nil},
	{"system", func(k *Kernel, _ []link.Link) bool { return k.Restart != nil }, func(k *Kernel) []tools.Tool {
		return []tools.Tool{restartTool{k}}
	}, nil},
	{"todo", func(k *Kernel, _ []link.Link) bool { return k.todoToolOn() }, func(k *Kernel) []tools.Tool { return []tools.Tool{todoTool{k}} }, nil},
	{"secrets", func(k *Kernel, links []link.Link) bool { return k.Vault != nil && namedLink(links, link.NameTools) }, func(k *Kernel) []tools.Tool {
		return []tools.Tool{&tools.HTTPRequest{Store: k.Vault}, &tools.SecretList{Store: k.Vault}}
	}, nil},
	{"shell_jobs", func(k *Kernel, links []link.Link) bool {
		return shellBackgroundOn(links) || k.liveShell() != nil && len(k.liveShell().List()) > 0
	}, func(k *Kernel) []tools.Tool {
		return []tools.Tool{shellOutputTool{k}, shellStatusTool{k}, shellCancelTool{k}}
	}, nil},
	{"agents", granted(link.NameAgents), func(k *Kernel) []tools.Tool {
		var start tools.Tool = spawnTool{k}
		if k.Dispatch {
			start = dispatchTool{k}
		}
		return []tools.Tool{start, steerTool{k}, queueTool{k}, interruptTool{k}, cancelTool{k}, readSubTool{k}, expandSubTool{k}}
	}, nil},
	// A skill listed in the prompt can be read without loading skills.
	{"skills", func(k *Kernel, links []link.Link) bool { return namedLink(links, link.NameSkills) || k.listedSkill() }, func(k *Kernel) []tools.Tool {
		return []tools.Tool{skillTool{k}}
	}, func(k *Kernel) bool { return !k.isLoaded(link.NameSkills) && !k.listedSkill() }},
	{"skills", granted(link.NameSkills), func(k *Kernel) []tools.Tool { return []tools.Tool{writeSkillTool{k}} }, func(k *Kernel) bool { return !k.isLoaded(link.NameSkills) }},
}

func granted(name string) func(*Kernel, []link.Link) bool {
	return func(_ *Kernel, links []link.Link) bool { return namedLink(links, name) }
}

func always(*Kernel, []link.Link) bool { return true }

// toolRegistry registers what the mounted links offer and the kernel rows.
// An MCP server registers its tools once it has been started this turn.
func (k *Kernel) toolRegistry() *toolRegistry {
	k.attachShell()
	links := k.snapshotLinks()
	search := k.webSearchOn()
	r := &toolRegistry{}
	for _, l := range links {
		if l == nil {
			continue
		}
		if m, ok := l.(*link.MCP); ok && !k.isLoaded(l.Name()) {
			r.pending = append(r.pending, pendingGroup{id: l.Name(), summary: serverSummary(m.Spec.Name), mcp: m})
			continue
		}
		source := linkSource(l)
		deferred := lazyUntilLoad(l.Name()) && !k.isLoaded(l.Name())
		for _, t := range l.Tools() {
			if t.Name() == "web_search" && !search {
				continue
			}
			r.Register(t, source, l.Name(), deferred)
		}
	}
	for _, row := range kernelTools {
		if !row.on(k, links) {
			continue
		}
		deferred := row.deferred != nil && row.deferred(k)
		for _, t := range row.tools(k) {
			r.Register(t, "kernel", row.group, deferred)
		}
	}
	return r
}

// linkSource follows the native-or-Link cut: an MCP server and a store grant
// depend on something outside this process; an organ and the rest are ours.
func linkSource(l link.Link) string {
	if _, ok := l.(*link.MCP); ok {
		return l.Name()
	}
	if _, ok := link.LookupOrgan(l.Name()); ok {
		return "organ"
	}
	if link.StoreGrant(l.Name()) {
		return "link"
	}
	return "kernel"
}

func serverSummary(name string) string {
	if a, ok := link.LookupApp(name); ok && a.Kind == link.AppServer {
		return a.Summary
	}
	return link.Blurb("mcp:" + name)
}

// startGroup starts one pending server for the rest of this turn.
func (k *Kernel) startGroup(ctx context.Context, id string) error {
	if _, err := k.toolsForLoaded(ctx, id); err != nil {
		return errors.New(k.redactor()(err.Error()))
	}
	k.markLoaded(id)
	return nil
}

// lookupTool finds id, starting the pending server whose naming it follows,
// so a tool seen in an earlier turn is callable without describing again.
func (k *Kernel) lookupTool(ctx context.Context, id string) (toolEntry, bool, error) {
	r := k.toolRegistry()
	if e, ok := r.find(id); ok {
		return e, true, nil
	}
	for _, p := range r.pending {
		if !p.mcp.Owns(id) {
			continue
		}
		if err := k.startGroup(ctx, p.id); err != nil {
			return toolEntry{}, false, err
		}
		e, ok := k.toolRegistry().find(id)
		return e, ok, nil
	}
	return toolEntry{}, false, nil
}

// toolSummary is the first sentence of a description, for search and lists.
func toolSummary(desc string) string {
	s := strings.Join(strings.Fields(desc), " ")
	for _, stop := range []string{"。", ". ", "；"} {
		if i := strings.Index(s, stop); i > 0 {
			s = s[:i+len(strings.TrimSpace(stop))]
		}
	}
	if r := []rune(s); len(r) > 120 {
		s = string(r[:119]) + "…"
	}
	return s
}

// toolDoor is the one proxy to every registered tool: search, describe, call.
type toolDoor struct{ k *Kernel }

type doorArgs struct {
	Action string          `json:"action"`
	Query  string          `json:"query"`
	Group  string          `json:"group"`
	Limit  int             `json:"limit"`
	ID     string          `json:"id"`
	Args   json.RawMessage `json:"args"`
}

func (toolDoor) Name() string { return "tools" }
func (toolDoor) Description() string {
	return "Reach every tool this Bot has that is not in your tool list. The environment's tool_index names them by group. action=search with query (and optional group, limit) returns matching ids with one-line summaries. action=describe with a tool id returns its full description and JSON schema; with a group id it lists the group's tools, starting an MCP server that is not running yet. action=call with id and args (an object) runs that tool, with the same review and confirmation as a direct call. None of these change your tool list."
}
func (toolDoor) Parameters() any {
	return schema(map[string]any{
		"action": map[string]any{"type": "string", "enum": []string{"search", "describe", "call"}, "description": "search, describe, or call"},
		"query":  stringProp("search: keywords"),
		"group":  stringProp("search: only this group"),
		"limit":  intProp("search: at most this many results; default 10"),
		"id":     stringProp("describe or call: tool id; describe also takes a group id"),
		"args":   map[string]any{"type": "object", "description": "call: the tool's arguments"},
	}, []string{"action"})
}

func parseDoor(raw json.RawMessage) (doorArgs, error) {
	var in doorArgs
	if err := json.Unmarshal(raw, &in); err != nil {
		return in, err
	}
	in.Action = strings.ToLower(strings.TrimSpace(in.Action))
	in.ID = strings.TrimSpace(in.ID)
	// A weak schema may send args as a JSON string.
	var text string
	if json.Unmarshal(in.Args, &text) == nil {
		in.Args = json.RawMessage(text)
	}
	if len(in.Args) == 0 || string(in.Args) == "null" {
		in.Args = json.RawMessage("{}")
	}
	return in, nil
}

func (toolDoor) Confirm(raw json.RawMessage) (bool, string, error) {
	in, err := parseDoor(raw)
	if err != nil {
		return false, "", err
	}
	if in.Action == "search" {
		return false, "search " + in.Query, nil
	}
	return false, in.Action + " " + in.ID, nil
}

// Forward hands a call to the agent as the tool it names, wrapped in review:
// calling through the door is the same as calling by name.
func (t toolDoor) Forward(ctx context.Context, raw json.RawMessage) (tools.Tool, json.RawMessage, error) {
	in, err := parseDoor(raw)
	if err != nil || in.Action != "call" {
		return nil, nil, err
	}
	if in.ID == "" {
		return nil, nil, errors.New("tools: call needs id")
	}
	if !json.Valid(in.Args) {
		return nil, nil, errors.New("tools: args are not valid JSON")
	}
	e, ok, err := t.k.lookupTool(ctx, in.ID)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return nil, nil, fmt.Errorf("tools: no tool with id %q; use action=search to find one", in.ID)
	}
	return reviewedTool{Tool: e.tool, k: t.k, undeclared: e.expose != exposeHot}, in.Args, nil
}

func (t toolDoor) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	in, err := parseDoor(raw)
	if err != nil {
		return "", err
	}
	switch in.Action {
	case "search":
		return t.k.searchTools(in.Query, in.Group, in.Limit), nil
	case "describe":
		return t.k.describeTool(ctx, in.ID)
	case "call":
		return "", errors.New("tools: call was not forwarded")
	}
	return "", fmt.Errorf("tools: unknown action %q; use search, describe, or call", in.Action)
}

func (k *Kernel) searchTools(query, group string, limit int) string {
	if limit <= 0 {
		limit = 10
	}
	limit = min(limit, 50)
	words := strings.Fields(strings.ToLower(query))
	r := k.toolRegistry()
	type hit struct {
		line  string
		score int
	}
	var hits []hit
	score := func(text string) int {
		if len(words) == 0 {
			return 1
		}
		n := 0
		for _, w := range words {
			if strings.Contains(text, w) {
				n++
			}
		}
		return n
	}
	for _, e := range r.entries {
		if group != "" && e.group != group {
			continue
		}
		name := e.tool.Name()
		if s := score(strings.ToLower(name + " " + e.group + " " + e.tool.Description())); s > 0 {
			line := fmt.Sprintf("%s [%s] %s", name, e.group, toolSummary(e.tool.Description()))
			if e.expose == exposeHot {
				line += " (in your tool list)"
			}
			hits = append(hits, hit{line, s})
		}
	}
	for _, p := range r.pending {
		if group != "" && p.id != group {
			continue
		}
		if s := score(strings.ToLower(p.id + " " + p.summary)); s > 0 {
			hits = append(hits, hit{fmt.Sprintf("%s [group, not started] %s Describe it to list its tools.", p.id, toolSummary(p.summary)), s})
		}
	}
	if len(hits) == 0 {
		return "no tool matches; try other keywords, or read tool_index in the environment"
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].line < hits[j].line
	})
	lines := make([]string, 0, limit)
	for i, h := range hits {
		if i == limit {
			lines = append(lines, fmt.Sprintf("(%d more; narrow the query)", len(hits)-limit))
			break
		}
		lines = append(lines, h.line)
	}
	return strings.Join(lines, "\n")
}

// describeTool puts a tool's full schema in the result. The declared tools[]
// is not touched: that is the whole point of the door.
func (k *Kernel) describeTool(ctx context.Context, id string) (string, error) {
	if id == "" {
		return "", errors.New("tools: describe needs id")
	}
	e, ok, err := k.lookupTool(ctx, id)
	if err != nil {
		return "", err
	}
	if ok {
		out, err := json.Marshal(struct {
			ID          string `json:"id"`
			Group       string `json:"group"`
			Source      string `json:"source"`
			Direct      bool   `json:"direct,omitempty"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{e.tool.Name(), e.group, e.source, e.expose == exposeHot, e.tool.Description(), e.tool.Parameters()})
		return string(out), err
	}
	// A group goes by its id, its server name, or its store title.
	names := []string{id, "mcp:" + id, link.ResolveApp(id)}
	r := k.toolRegistry()
	for _, p := range r.pending {
		if slices.Contains(names, p.id) {
			if err := k.startGroup(ctx, p.id); err != nil {
				return "", err
			}
			r = k.toolRegistry()
			break
		}
	}
	type member struct {
		ID      string `json:"id"`
		Summary string `json:"summary"`
	}
	var group string
	var members []member
	for _, e := range r.entries {
		if slices.Contains(names, e.group) && (group == "" || group == e.group) {
			group = e.group
			members = append(members, member{e.tool.Name(), toolSummary(e.tool.Description())})
		}
	}
	if len(members) == 0 {
		return "", fmt.Errorf("tools: no tool or group %q; use action=search", id)
	}
	sort.Slice(members, func(i, j int) bool { return members[i].ID < members[j].ID })
	out, err := json.Marshal(struct {
		Group string   `json:"group"`
		Tools []member `json:"tools"`
	}{group, members})
	return string(out), err
}
