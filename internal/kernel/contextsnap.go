package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"xbot/internal/bot"
	"xbot/internal/compact"
	"xbot/internal/provider"
)

// contextSnapCap is how many requests of one wake are kept. A turn makes one
// request per step, so a long turn has many; the panel offers the last ten.
// Only the newest keeps its detail (see ContextDetail); the older ones keep
// their band totals and how far the turn had got, which is all the panel needs
// to show them, and keeps ten requests from holding ten copies of the prompt.
const contextSnapCap = 10

// contextStepCap bounds the steps of the current turn the detail keeps. A turn
// longer than this drops its oldest steps, and says how many.
const contextStepCap = 150

// contextTurnCap bounds the earlier turns listed one per row. Older ones are
// only counted.
const contextTurnCap = 40

// contextPreviewRunes clips the text kept for one row: enough to recognise a
// tool result, not enough to carry a file.
const contextPreviewRunes = 240

// contextWhatRunes clips the one-line description of a step.
const contextWhatRunes = 120

// ContextLayer is one band of the assembled context, in send order. A summary
// block is its own layer, so a block that compacts a range stays visible as one.
type ContextLayer struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Tokens int    `json:"tokens"`
	// From and To are the transcript sequences a summary block covers. Zero
	// for every other layer.
	From int `json:"from,omitempty"`
	To   int `json:"to,omitempty"`
}

// ContextRequest is one model call of the current wake.
type ContextRequest struct {
	Status string `json:"status"`
	// Step counts model calls in the wake, starting at 1.
	Step int    `json:"step"`
	At   string `json:"at"`
	// Prompt is the token estimate of everything sent, tools included.
	Prompt int `json:"prompt"`
	// Tools is the estimate for the tool definitions alone. ReportUsage keeps
	// tools out of the prompt count, so the two differ by this much.
	Tools  int            `json:"tools"`
	Layers []ContextLayer `json:"layers"`
	// TurnSteps is how many steps of the current turn this request carried.
	// The steps of one wake only grow, so an older request is shown as the
	// first TurnSteps steps of the newest request's detail.
	TurnSteps int `json:"turn_steps"`
}

// ContextStep is one step of the current turn: a tool call with its result
// folded in, or a reply text.
type ContextStep struct {
	// Tool is empty for a reply text.
	Tool string `json:"tool,omitempty"`
	// What says what the step acted on: the command, path or query, on one line.
	What   string `json:"what"`
	Tokens int    `json:"tokens"`
	// Fail is set when the result reads as an error or a non-zero exit.
	Fail    bool   `json:"fail,omitempty"`
	Preview string `json:"preview,omitempty"`
}

// ContextTurn is one earlier turn, named by what started it.
type ContextTurn struct {
	Ask    string `json:"ask"`
	Tokens int    `json:"tokens"`
}

// ContextBlock is one summary block.
type ContextBlock struct {
	From    int    `json:"from"`
	To      int    `json:"to"`
	Tokens  int    `json:"tokens"`
	Preview string `json:"preview,omitempty"`
}

// ContextTool is one tool definition's share of the tools band.
type ContextTool struct {
	Name   string `json:"name"`
	Tokens int    `json:"tokens"`
}

// ContextDetail is what the newest request carries row by row. Every text in it
// went through RedactText.
type ContextDetail struct {
	Steps []ContextStep `json:"steps"`
	// SkippedSteps is how many of the turn's first steps were dropped by
	// contextStepCap; Steps[0] is step SkippedSteps+1.
	SkippedSteps int           `json:"skipped_steps,omitempty"`
	Turns        []ContextTurn `json:"turns"`
	// EarlierTurns and EarlierTokens count the turns before Turns[0].
	EarlierTurns  int            `json:"earlier_turns,omitempty"`
	EarlierTokens int            `json:"earlier_tokens,omitempty"`
	Blocks        []ContextBlock `json:"blocks"`
	ToolDefs      []ContextTool  `json:"tool_defs"`
}

// ContextSnap is what the owner inspects: the requests of the current wake.
type ContextSnap struct {
	Bot string `json:"bot"`
	// Target is the soft line: past it the kernel compacts in the background.
	// Ceiling is the hard line for the request, tool definitions included:
	// past it the turn compacts inline. Both follow the bound model (ADR 0015).
	Target  int `json:"target"`
	Ceiling int `json:"ceiling"`
	// Window is the model's context length from providers.json; 0 when unknown.
	Window int `json:"window,omitempty"`
	// Requests is the kept model calls of this wake, oldest first.
	Requests []ContextRequest `json:"requests"`
	// Detail is the row-level view of the newest request; nil before the first.
	Detail *ContextDetail `json:"detail,omitempty"`
}

// contextRing holds the recent requests of one kernel. It is memory only: a
// snapshot must never reach disk, because it carries the assembled prompt
// including memory and tool output, and the transcript already holds the
// durable copy of every line.
type contextRing struct {
	mu     sync.Mutex
	reqs   []ContextRequest
	detail *ContextDetail
	// tools is the tool-definition estimate of the last request sent, from
	// any branch. The hard ceiling takes it off, because tools ride on every
	// request but are not in the message estimate.
	tools int
	// calls counts model calls in this wake. The ring drops old entries, so
	// the number cannot be recovered from its length.
	calls int
}

// noteContext records one request about to be sent. It runs in the provider
// callback, so the rows are clipped here rather than in the web handler: the
// request itself is not kept.
func (k *Kernel) noteContext(t *turn, req provider.Request) {
	if k == nil || t == nil {
		return
	}
	tools := estimateTools(req.Tools)
	k.context.mu.Lock()
	k.context.tools = tools
	k.context.mu.Unlock()
	if t.isForked() {
		// A background branch has its own context; its requests are not the
		// foreground's and would otherwise replace what the owner is reading.
		return
	}
	k.context.mu.Lock()
	defer k.context.mu.Unlock()
	k.context.calls++
	snap, detail := buildContextRequest(req, k.context.calls, k.now().Format("2006-01-02T15:04:05Z07:00"))
	k.context.reqs = append(k.context.reqs, snap)
	if len(k.context.reqs) > contextSnapCap {
		k.context.reqs = k.context.reqs[len(k.context.reqs)-contextSnapCap:]
	}
	k.context.detail = detail
}

// resetContext drops the kept requests when a new wake starts.
func (k *Kernel) resetContext() {
	if k == nil {
		return
	}
	k.context.mu.Lock()
	k.context.reqs = nil
	k.context.detail = nil
	k.context.calls = 0
	k.context.mu.Unlock()
}

// ContextSnapshot returns the requests of the current wake, oldest first.
func (k *Kernel) ContextSnapshot() ContextSnap {
	snap := ContextSnap{}
	if k == nil || k.Bot == nil {
		return snap
	}
	snap.Bot = k.Bot.Name()
	snap.Target, snap.Ceiling = k.lines()
	snap.Window = k.contextWindow()
	k.context.mu.Lock()
	snap.Requests = append([]ContextRequest(nil), k.context.reqs...)
	snap.Detail = k.context.detail
	k.context.mu.Unlock()
	return snap
}

// buildContextRequest turns one sent request into its band totals and its
// row-level detail. step is the model call's position in the wake.
func buildContextRequest(req provider.Request, step int, at string) (ContextRequest, *ContextDetail) {
	defs := toolShares(req.Tools)
	out := ContextRequest{Step: step, At: at, Status: "running"}
	for _, d := range defs {
		out.Tools += d.Tokens
	}
	var detail *ContextDetail
	out.Layers, detail = splitLayers(req.Messages)
	detail.ToolDefs = defs
	out.TurnSteps = len(detail.Steps) + detail.SkippedSteps
	for _, l := range out.Layers {
		out.Prompt += l.Tokens
	}
	out.Prompt += out.Tools
	return out, detail
}

// estimateTools applies the kernel's rough estimate to the tool definitions,
// so the figure can be read next to the layer figures.
func estimateTools(defs []provider.ToolDef) int {
	n := 0
	for _, d := range toolShares(defs) {
		n += d.Tokens
	}
	return n
}

// toolShares estimates each tool definition, largest first.
func toolShares(defs []provider.ToolDef) []ContextTool {
	out := make([]ContextTool, 0, len(defs))
	for _, d := range defs {
		n := 4 + compact.Estimate(d.Name) + compact.Estimate(d.Description)
		if d.Parameters != nil {
			if raw, err := json.Marshal(d.Parameters); err == nil {
				n += compact.Estimate(string(raw))
			}
		}
		out = append(out, ContextTool{Name: d.Name, Tokens: n})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Tokens > out[j].Tokens })
	return out
}

// splitLayers sums the sent messages into the bands the panel shows and builds
// the rows behind them. The current turn starts at the last message the owner
// (or whoever woke the bot) sent; history before it is 早先对话.
func splitLayers(msgs []provider.Message) ([]ContextLayer, *ContextDetail) {
	start := turnStart(msgs)
	sums := map[string]int{}
	at := map[string]int{}
	var layers []ContextLayer
	detail := &ContextDetail{Steps: []ContextStep{}, Turns: []ContextTurn{}, Blocks: []ContextBlock{}}
	var turns []ContextTurn
	callAt := map[string]int{}
	for i, m := range msgs {
		kind := classifyMessage(m, i)
		est := compact.EstimateMessage(m)
		if kind == kindBlock {
			from, to := summaryRange(m.Content)
			layers = append(layers, ContextLayer{Key: keyBlock, Label: "摘要块", Tokens: est, From: from, To: to})
			detail.Blocks = append(detail.Blocks, ContextBlock{From: from, To: to, Tokens: est, Preview: clipText(blockBody(m.Content), contextPreviewRunes)})
			continue
		}
		if kind == kindHist && start >= 0 && i >= start {
			kind = kindTurn
		}
		key, label := layerOf(kind)
		sums[key] += est
		if at[key] == 0 {
			layers = append(layers, ContextLayer{Key: key, Label: label, Tokens: est})
			at[key] = len(layers)
		} else {
			layers[at[key]-1].Tokens = sums[key]
		}
		switch kind {
		case kindHist:
			if m.Role == provider.RoleUser || len(turns) == 0 {
				turns = append(turns, ContextTurn{Ask: askOf(m)})
			}
			turns[len(turns)-1].Tokens += est
		case kindTurn:
			if i == start {
				continue
			}
			detail.Steps = addStep(detail.Steps, callAt, m, est)
		}
	}
	if n := len(turns) - contextTurnCap; n > 0 {
		for _, t := range turns[:n] {
			detail.EarlierTokens += t.Tokens
		}
		detail.EarlierTurns = n
		turns = turns[n:]
	}
	detail.Turns = append(detail.Turns, turns...)
	if n := len(detail.Steps) - contextStepCap; n > 0 {
		detail.SkippedSteps = n
		detail.Steps = append([]ContextStep(nil), detail.Steps[n:]...)
	}
	return layers, detail
}

// turnStart is the index of the message that opened the current turn: the last
// user message that is history rather than a memory, environment or summary
// wrapper. -1 when there is none, and then every history message is earlier.
func turnStart(msgs []provider.Message) int {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == provider.RoleUser && classifyMessage(msgs[i], i) == kindHist {
			return i
		}
	}
	return -1
}

// addStep folds one message of the current turn into the step list. A call
// opens a step per tool call; its result joins that step by call id, so the
// panel shows a call and what came back as one row.
func addStep(steps []ContextStep, callAt map[string]int, m provider.Message, est int) []ContextStep {
	switch {
	case m.Role == provider.RoleAssistant && len(m.ToolCalls) > 0:
		share := est / len(m.ToolCalls)
		for _, tc := range m.ToolCalls {
			callAt[tc.ID] = len(steps)
			steps = append(steps, ContextStep{Tool: tc.Function.Name, What: whatOf(tc.Function.Arguments), Tokens: share})
		}
		if text := strings.TrimSpace(m.Content); text != "" {
			steps = append(steps, ContextStep{What: firstLine(text), Preview: clipText(text, contextPreviewRunes)})
		}
	case m.Role == provider.RoleTool:
		i, ok := callAt[m.ToolCallID]
		if !ok {
			steps = append(steps, ContextStep{Tool: m.Name})
			i = len(steps) - 1
		}
		steps[i].Tokens += est
		steps[i].Fail = failed(m.Content)
		steps[i].Preview = clipText(m.Content, contextPreviewRunes)
		if steps[i].What == "" {
			steps[i].What = firstLine(m.Content)
		}
	default:
		text := strings.TrimSpace(m.Content)
		steps = append(steps, ContextStep{What: firstLine(text), Tokens: est, Preview: clipText(text, contextPreviewRunes)})
	}
	return steps
}

// whatKeys are the arguments that say what a call acted on, in the order they
// are looked for. A call with none of them shows its first string argument.
var whatKeys = []string{"command", "path", "file", "query", "url", "to", "name", "id"}

// whatOf names what a tool call acted on, on one line.
func whatOf(args string) string {
	var m map[string]any
	if json.Unmarshal([]byte(args), &m) == nil {
		for _, k := range whatKeys {
			if v, ok := m[k].(string); ok && strings.TrimSpace(v) != "" {
				return clipLine(v)
			}
		}
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if v, ok := m[k].(string); ok && strings.TrimSpace(v) != "" {
				return clipLine(v)
			}
		}
	}
	return clipLine(args)
}

// failed reads a tool result as a failure: the kernel's "error:" prefix, or a
// shell result whose first line is a non-zero exit.
func failed(content string) bool {
	head := strings.TrimSpace(content)
	if strings.HasPrefix(strings.ToLower(head), "error") {
		return true
	}
	line := firstLine(head)
	if code, ok := strings.CutPrefix(line, "exit "); ok {
		n, err := strconv.Atoi(strings.TrimSpace(code))
		return err == nil && n != 0
	}
	return false
}

// askOf names a turn by the message that opened it.
func askOf(m provider.Message) string {
	if m.Role != provider.RoleUser {
		return ""
	}
	return firstLine(m.Content)
}

// blockBody drops the summary wrapper's opening tag, so the preview starts at
// the summary text.
func blockBody(content string) string {
	if i := strings.IndexByte(content, '>'); i >= 0 && strings.HasPrefix(content, "<summary") {
		return content[i+1:]
	}
	return content
}

// firstLine is the first non-empty line, redacted and clipped.
func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			return clipLine(l)
		}
	}
	return ""
}

// clipLine collapses whitespace, shortens absolute paths and clips to one row.
func clipLine(s string) string {
	return clipText(shortPaths(strings.Join(strings.Fields(s), " ")), contextWhatRunes)
}

// longPath matches an absolute path of three or more segments that starts a
// word, so a relative path such as ./internal/web is left alone.
var longPath = regexp.MustCompile(`(^|[\s'"=])(/(?:[^/\s'"]+/){2,}[^/\s'"]*)`)

// shortPaths keeps the last two segments of each long absolute path. In a row
// of 320px the working directory prefix is the same on every step and pushes
// the part that differs, the file name, out of view.
func shortPaths(s string) string {
	return longPath.ReplaceAllStringFunc(s, func(m string) string {
		sub := longPath.FindStringSubmatch(m)
		parts := strings.Split(strings.TrimSuffix(sub[2], "/"), "/")
		if len(parts) <= 3 {
			return m
		}
		return sub[1] + "…/" + strings.Join(parts[len(parts)-2:], "/")
	})
}

// clipText redacts and clips. The panel is the only place a tool result is
// shown outside the transcript, and the vault rule applies to it the same way.
func clipText(s string, n int) string {
	r := []rune(bot.RedactText(strings.TrimSpace(s)))
	if len(r) > n {
		return strings.TrimSpace(string(r[:n])) + "…"
	}
	return string(r)
}

// Layer keys. They are also the CSS hook the panel colours by.
const (
	keySys   = "sys"
	keyTools = "tools"
	keyMem   = "mem"
	keyBlock = "block"
	keyEnv   = "env"
	keyHist  = "hist"
	keyTurn  = "turn"

	kindSys    = "sys"
	kindMemory = "memory"
	kindBlock  = "block"
	kindEnv    = "env"
	kindHist   = "hist"
	kindTurn   = "turn"
)

// layerOf maps a message kind to the band it is summed into.
func layerOf(kind string) (string, string) {
	switch kind {
	case kindSys:
		return keySys, "系统提示词"
	case kindMemory:
		return keyMem, "记忆"
	case kindEnv:
		return keyEnv, "环境更新"
	case kindTurn:
		return keyTurn, "本轮"
	default:
		return keyHist, "早先对话"
	}
}

// classifyMessage names the band a sent message belongs to. A summary block
// and the memory and environment blocks are recognised by their wrappers; the
// system prompt is the leading system message.
func classifyMessage(m provider.Message, i int) string {
	if m.Role == provider.RoleSystem {
		return kindSys
	}
	switch {
	case strings.HasPrefix(m.Content, "<summary from="):
		return kindBlock
	case strings.HasPrefix(m.Content, "<environment_"):
		return kindEnv
	case strings.HasPrefix(m.Content, "<memory>"):
		return kindMemory
	}
	return kindHist
}

// summaryRange reads the sequence range out of a summary block wrapper. The
// wrapper is written by summaryMessage, so a malformed one means the text was
// produced elsewhere; zero is returned rather than a guess. Every digit in
// the wrapper is read here, never estimated from the text.
func summaryRange(content string) (int, int) {
	const head = `<summary from="`
	if !strings.HasPrefix(content, head) {
		return 0, 0
	}
	from, rest, ok := cutNumber(content[len(head):])
	if !ok {
		return 0, 0
	}
	const mid = `to="`
	rest = strings.TrimPrefix(rest, " ")
	if !strings.HasPrefix(rest, mid) {
		return from, 0
	}
	to, _, ok := cutNumber(rest[len(mid):])
	if !ok {
		return from, 0
	}
	return from, to
}

// cutNumber reads the leading digits and returns the remainder after the
// closing quote, so the caller matches the next field from a fresh token.
func cutNumber(s string) (int, string, bool) {
	q := strings.IndexByte(s, '"')
	if q <= 0 {
		return 0, "", false
	}
	n, err := strconv.Atoi(s[:q])
	if err != nil {
		return 0, "", false
	}
	return n, s[q+1:], true
}

// endContext annotates the already-recorded request, including cancellations.
func (k *Kernel) endContext(t *turn, err error) {
	if t == nil || t.isForked() {
		return
	}
	k.context.mu.Lock()
	defer k.context.mu.Unlock()
	if len(k.context.reqs) == 0 {
		return
	}
	status := "completed"
	if errors.Is(err, context.Canceled) {
		status = "cancelled"
	} else if err != nil {
		status = "failed"
	}
	k.context.reqs[len(k.context.reqs)-1].Status = status
}
