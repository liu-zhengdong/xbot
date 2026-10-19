package kernel

import (
	"encoding/json"
	"strconv"
	"strings"
	"sync"

	"xbot/internal/bot"
	"xbot/internal/compact"
	"xbot/internal/provider"
)

// contextSnapCap is how many requests of one wake are kept. A turn makes one
// request per step, and the tail grows with each, so the inspector shows the
// recent steps rather than the whole turn. Three is enough to see what a
// compaction or a tool call changed between steps without holding a large
// transcript copy in memory.
const contextSnapCap = 3

// contextEntryCap bounds one snapshot's entry list. The first few dozen rows
// stay legible; the rest would only make a large JSON body.
const contextEntryCap = 120

// contextPreviewRunes clips one entry's preview: enough to recognise a tool
// result, not enough to carry a file.
const contextPreviewRunes = 400

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

// ContextEntry is one message as it was sent, clipped for display.
type ContextEntry struct {
	Role    string `json:"role"`
	Kind    string `json:"kind"`
	Preview string `json:"preview"`
	// Chars is the full content length, so a clipped preview still shows size.
	Chars int `json:"chars,omitempty"`
	// Tokens is the same rough estimate the bands use, so a row and its band
	// agree. It is computed from the full content, not from the preview.
	Tokens int  `json:"tokens,omitempty"`
	Cut    bool `json:"cut,omitempty"`
	// Tool is the tool a call or a result belongs to, when there is one.
	Tool string `json:"tool,omitempty"`
	// From and To are set on a summary block: the transcript range it covers.
	From int `json:"from,omitempty"`
	To   int `json:"to,omitempty"`
}

// ContextRequest is one model call of the current turn.
type ContextRequest struct {
	// Step counts model calls in the turn, starting at 1.
	Step int    `json:"step"`
	At   string `json:"at"`
	// Prompt is the token estimate of everything sent, tools included.
	Prompt int `json:"prompt"`
	// Tools is the estimate for the tool definitions alone. ReportUsage keeps
	// tools out of the prompt count, so the two differ by this much.
	Tools  int            `json:"tools"`
	Layers []ContextLayer `json:"layers"`
	// Entries is the sent message list in order. Capped at contextEntryCap.
	Entries []ContextEntry `json:"entries"`
	// More is how many entries were left out of Entries.
	More int `json:"more,omitempty"`
}

// ContextSnap is what the owner inspects: the requests of the current wake.
type ContextSnap struct {
	Bot string `json:"bot"`
	// WorkingSet is the size the session is held near, and the denominator the
	// usage reminder uses. Zero means the kernel has no working set.
	WorkingSet int `json:"working_set"`
	// Window is the model's context length from providers.json; 0 when unknown.
	Window int `json:"window,omitempty"`
	// Requests is the kept model calls of this wake, oldest first.
	Requests []ContextRequest `json:"requests"`
}

// contextRing holds the recent requests of one kernel. It is memory only: a
// snapshot must never reach disk, because it carries the assembled prompt
// including memory and tool output, and the transcript already holds the
// durable copy of every line.
type contextRing struct {
	mu   sync.Mutex
	reqs []ContextRequest
	// calls counts model calls in this wake. The ring drops old entries, so
	// the number cannot be recovered from its length.
	calls int
}

// noteContext records one request about to be sent. It runs in the provider
// callback, so the entry previews are clipped here rather than in the web
// handler: the request itself is not kept.
func (k *Kernel) noteContext(t *turn, req provider.Request) {
	if k == nil || t == nil {
		return
	}
	if t.isForked() {
		// A background branch has its own context; its requests are not the
		// foreground's and would otherwise replace what the owner is reading.
		return
	}
	k.context.mu.Lock()
	defer k.context.mu.Unlock()
	k.context.calls++
	snap := buildContextRequest(req, k.context.calls, k.now().Format("2006-01-02T15:04:05Z07:00"))
	k.context.reqs = append(k.context.reqs, snap)
	if len(k.context.reqs) > contextSnapCap {
		k.context.reqs = k.context.reqs[len(k.context.reqs)-contextSnapCap:]
	}
}

// resetContext drops the kept requests when a new wake starts.
func (k *Kernel) resetContext() {
	if k == nil {
		return
	}
	k.context.mu.Lock()
	k.context.reqs = nil
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
	snap.WorkingSet = k.compactAt()
	snap.Window = k.contextWindow()
	k.context.mu.Lock()
	snap.Requests = append([]ContextRequest(nil), k.context.reqs...)
	k.context.mu.Unlock()
	return snap
}

// buildContextRequest turns one sent request into an inspectable record. step
// is the model call's position in the turn.
func buildContextRequest(req provider.Request, step int, at string) ContextRequest {
	out := ContextRequest{Step: step, At: at, Tools: estimateTools(req.Tools)}
	out.Layers, out.Entries, out.More = splitLayers(req.Messages)
	for _, l := range out.Layers {
		out.Prompt += l.Tokens
	}
	out.Prompt += out.Tools
	return out
}

// estimateTools applies the kernel's rough estimate to the tool definitions,
// so the figure can be read next to the layer figures.
func estimateTools(defs []provider.ToolDef) int {
	if len(defs) == 0 {
		return 0
	}
	n := 0
	for _, d := range defs {
		n += 4 + compact.Estimate(d.Name) + compact.Estimate(d.Description)
		if d.Parameters != nil {
			if raw, err := json.Marshal(d.Parameters); err == nil {
				n += compact.Estimate(string(raw))
			}
		}
	}
	return n
}

// splitLayers classifies the sent messages into the bands the panel shows and
// keeps a clipped copy of each. Entries keeps the send order.
func splitLayers(msgs []provider.Message) ([]ContextLayer, []ContextEntry, int) {
	sums := map[string]int{}
	at := map[string]int{}
	var layers []ContextLayer
	entries := make([]ContextEntry, 0, min(len(msgs), contextEntryCap))
	for i, m := range msgs {
		kind := classifyMessage(m, i)
		est := compact.EstimateMessage(m)
		if kind == kindBlock {
			from, to := summaryRange(m.Content)
			layers = append(layers, ContextLayer{Key: keyBlock, Label: "摘要块", Tokens: est, From: from, To: to})
		} else {
			key, label := layerOf(kind)
			sums[key] += est
			if at[key] == 0 {
				layers = append(layers, ContextLayer{Key: key, Label: label, Tokens: est})
				at[key] = len(layers)
			} else {
				layers[at[key]-1].Tokens = sums[key]
			}
		}
		if len(entries) < contextEntryCap {
			entries = append(entries, contextEntry(m, kind, est))
		}
	}
	return layers, entries, len(msgs) - len(entries)
}

// Layer keys. They are also the CSS hook the panel colours by.
const (
	keySys   = "sys"
	keyTools = "tools"
	keyMem   = "mem"
	keyBlock = "block"
	keyEnv   = "env"
	keyHist  = "hist"

	kindSys    = "sys"
	kindMemory = "memory"
	kindBlock  = "block"
	kindEnv    = "env"
	kindHist   = "hist"
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
	default:
		return keyHist, "对话历史"
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

// contextEntry clips one message for display. The text is redacted: the panel
// is the only place a tool result is shown outside the transcript, and the
// vault rule applies to it the same way.
func contextEntry(m provider.Message, kind string, est int) ContextEntry {
	e := ContextEntry{Role: string(m.Role), Kind: kind, Chars: len(m.Content), Tokens: est}
	body := m.Content
	if len(m.ToolCalls) > 0 {
		var b strings.Builder
		for _, tc := range m.ToolCalls {
			b.WriteString(tc.Function.Name)
			b.WriteString(" ")
			b.WriteString(tc.Function.Arguments)
			b.WriteString("\n")
			if e.Tool == "" {
				e.Tool = tc.Function.Name
			}
		}
		body = strings.TrimSpace(b.String())
	}
	if m.Role == provider.RoleTool && e.Tool == "" {
		e.Tool = m.Name
	}
	if kind == kindBlock {
		e.From, e.To = summaryRange(body)
	}
	r := []rune(bot.RedactText(body))
	if len(r) > contextPreviewRunes {
		e.Preview = strings.TrimSpace(string(r[:contextPreviewRunes])) + "…"
		e.Cut = true
		return e
	}
	e.Preview = strings.TrimSpace(string(r))
	return e
}
