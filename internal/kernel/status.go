package kernel

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"xbot/internal/bot"
	"xbot/internal/clip"
	"xbot/internal/peer"
	"xbot/internal/provider"
)

// Bot state values.
const (
	StateIdle     = "idle"
	StateRunning  = "running"
	StateAwaiting = "awaiting"
	StateOffline  = "offline"
)

// One step's state in the per-turn ring.
const (
	StepRunning  = "running"
	StepDone     = "done"
	StepError    = "error"
	StepAwaiting = "awaiting"
)

const (
	stepRingCap    = 20
	statusPreview  = 80
	stepSummaryCap = 120
	// nodeStatusCap bounds the status a node returns in one call. The JSON
	// stays valid: whole bots are dropped until it fits.
	nodeStatusCap = 16 << 10
	botStatusCap  = 4 << 10
	tailRead      = 5
	tailHydrate   = 512
)

// BotStatus is one bot's derived state. Node is empty for a local bot and the
// node label for a remote one. StaleSeconds is set only for a cached remote
// status whose node is now unreachable.
type BotStatus struct {
	Bot         string `json:"bot"`
	State       string `json:"state"`
	Tool        string `json:"tool,omitempty"`
	Since       string `json:"since,omitempty"`
	Backgrounds int    `json:"backgrounds,omitempty"`
	LastMessage string `json:"last_message,omitempty"`
	// Todo is one line from the bot's task list, when it has open items.
	// The steps ring does not carry the list; GET /api/bots/{name}/todos does.
	Todo string `json:"todo,omitempty"`
	// Tokens is the latest model-call usage and the session totals. Empty
	// until the kernel has seen a completion.
	Tokens       *TokenUse `json:"tokens,omitempty"`
	Node         string    `json:"node,omitempty"`
	StaleSeconds int       `json:"stale_seconds,omitempty"`
}

// Step is one tool step in a turn, kept in a small ring for the web and the
// bot_status tool.
type Step struct {
	Time    time.Time `json:"time"`
	Tool    string    `json:"tool"`
	Summary string    `json:"summary,omitempty"`
	State   string    `json:"state"`
}

// StatusSource lets a kernel read every same-owner bot's status, including
// remote bots. The web server implements it over its live kernels and the node
// registry. A nil source falls back to this process's own bots.
type StatusSource interface {
	BotStatuses() []BotStatus
	BotSteps(name string, n int) []Step
}

func statusStamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format(time.RFC3339)
}

// clipStatusPreview clips to at most n runes, adding an ellipsis when cut.
func clipStatusPreview(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	if n <= 0 {
		return ""
	}
	return string(r[:n-1]) + "…"
}

func oneStatusLine(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.Join(strings.Fields(s), " ")
}

func statusSystemLine(s string) bool {
	for _, p := range []string{"<environment_", "<memory>", "<summary ", "<background_tasks>"} {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// tailMessage returns the last assistant or user message preview and its time,
// reading only the transcript tail. It never asks the model.
func tailMessage(b *bot.Bot) (string, time.Time) {
	if b == nil {
		return "", time.Time{}
	}
	n, err := b.Count()
	if err != nil || n == 0 {
		return "", time.Time{}
	}
	from := n - tailRead + 1
	if from < 1 {
		from = 1
	}
	entries, err := b.Slice(from, n)
	if err != nil {
		return "", time.Time{}
	}
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e.IsForkMark() {
			continue
		}
		role := e.Message.Role
		if role != provider.RoleAssistant && role != provider.RoleUser {
			continue
		}
		content := e.Message.Content
		if strings.TrimSpace(content) == "" && e.Blob != "" {
			if h, err := b.HydrateLimit(e, tailHydrate); err == nil {
				content = h.Message.Content
			}
		}
		content = strings.TrimSpace(oneStatusLine(content))
		if content == "" {
			continue
		}
		if role == provider.RoleUser && statusSystemLine(content) {
			continue
		}
		if _, ok := bot.ParseApprovalCard(content); ok {
			continue
		}
		return clipStatusPreview(bot.RedactText(content), statusPreview), e.Time
	}
	return "", time.Time{}
}

// IdleStatus builds an idle status for a bot with no live kernel, from the
// transcript tail only.
func IdleStatus(b *bot.Bot, now time.Time) BotStatus {
	st := BotStatus{State: StateIdle}
	if b != nil {
		st.Bot = b.Name()
	}
	preview, at := tailMessage(b)
	st.LastMessage = preview
	st.Todo = TodoSummary(b)
	if at.IsZero() {
		at = now
	}
	st.Since = statusStamp(at)
	return st
}

// Status derives this bot's state from the active turn, pending approvals, and
// running backgrounds. It makes no model calls.
func (k *Kernel) Status() BotStatus {
	var st BotStatus
	if k == nil || k.Bot == nil {
		return st
	}
	st.Bot = k.Bot.Name()
	now := k.now()
	t := k.curTurn()
	t.mu.Lock()
	active := !t.finished && !t.started.IsZero()
	awaiting := t.awaiting
	awaitingSince := t.awaitingSince
	tool := t.lastTool
	started := t.started
	toolStart := t.lastToolStart
	t.mu.Unlock()
	st.Backgrounds = len(k.Backgrounds())
	st.Todo = TodoSummary(k.Bot)
	if use, ok := k.tokenSnap(); ok {
		st.Tokens = &use
	}
	bgs := st.Backgrounds
	st.Tool = tool
	switch {
	case awaiting:
		st.State = StateAwaiting
		if awaitingSince.IsZero() {
			awaitingSince = now
		}
		st.Since = statusStamp(awaitingSince)
	case active || bgs > 0:
		st.State = StateRunning
		when := started
		if !toolStart.IsZero() && toolStart.After(when) {
			when = toolStart
		}
		if when.IsZero() {
			when = now
		}
		st.Since = statusStamp(when)
	default:
		st.State = StateIdle
		preview, at := tailMessage(k.Bot)
		st.LastMessage = preview
		if at.IsZero() {
			at = now
		}
		st.Since = statusStamp(at)
	}
	return st
}

// Steps returns up to n recent steps of the current turn, oldest first.
func (k *Kernel) Steps(n int) []Step {
	if k == nil {
		return nil
	}
	if n <= 0 || n > stepRingCap {
		n = stepRingCap
	}
	t := k.curTurn()
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.steps) == 0 {
		return nil
	}
	from := len(t.steps) - n
	if from < 0 {
		from = 0
	}
	out := make([]Step, len(t.steps)-from)
	copy(out, t.steps[from:])
	return out
}

// beginStep records a tool call starting, for the turn ring and the status.
func (k *Kernel) beginStep(t *turn, name, summary string) {
	if t == nil {
		return
	}
	redact := k.redactor()
	t.mu.Lock()
	defer t.mu.Unlock()
	t.tools++
	t.lastTool = name
	t.lastToolStart = k.now()
	line := clipStatusPreview(oneStatusLine(redact(summary)), stepSummaryCap)
	// Concurrent tool calls can interleave; pick the last step that is still
	// running rather than trusting a stored index.
	for i := range t.steps {
		if t.steps[i].State == StepRunning {
			t.steps[i].State = StepDone
		}
	}
	t.steps = append(t.steps, Step{Time: t.lastToolStart, Tool: name, Summary: line, State: StepRunning})
	if len(t.steps) > stepRingCap {
		t.steps = append([]Step(nil), t.steps[len(t.steps)-stepRingCap:]...)
	}
}

// endStep records a tool call finishing. output is the tool result, whose
// "error:" prefix marks a failed step.
func (k *Kernel) endStep(t *turn, name, output string) {
	if t == nil {
		return
	}
	state := StepDone
	if strings.HasPrefix(strings.TrimSpace(output), "error:") {
		state = StepError
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.lastToolEnd = k.now()
	for i := len(t.steps) - 1; i >= 0; i-- {
		if t.steps[i].Tool == name && (t.steps[i].State == StepRunning || t.steps[i].State == StepAwaiting) {
			t.steps[i].State = state
			return
		}
	}
}

// beginAwait marks that a turn is blocked on the owner's approval.
func (k *Kernel) beginAwait(t *turn, tool string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.awaiting = true
	t.awaitingSince = k.now()
	t.lastTool = tool
	for i := len(t.steps) - 1; i >= 0; i-- {
		if t.steps[i].Tool == tool && t.steps[i].State == StepRunning {
			t.steps[i].State = StepAwaiting
			break
		}
	}
}

func (k *Kernel) endAwait(t *turn) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.awaiting = false
	t.awaitingSince = time.Time{}
	for i := range t.steps {
		if t.steps[i].State == StepAwaiting {
			t.steps[i].State = StepRunning
		}
	}
}

// localStatuses lists this process's same-owner bots. A bot with the live
// kernel is derived; every other one is idle from its transcript tail.
func (k *Kernel) localStatuses() []BotStatus {
	var out []BotStatus
	if k == nil || k.Bot == nil {
		return out
	}
	out = append(out, k.Status())
	others, err := peer.VisibleBots(k.Bot.Root, k.Bot.Name())
	if err != nil {
		return out
	}
	now := k.now()
	for _, cfg := range others {
		b, err := bot.Load(k.Bot.Root, cfg.Name)
		if err != nil {
			continue
		}
		out = append(out, IdleStatus(b, now))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Bot < out[j].Bot })
	return out
}

// BotStatuses returns the status for every same-owner bot, live or idle. The
// web server's source also merges remote nodes; without one only this process
// is visible.
func (k *Kernel) BotStatuses() []BotStatus {
	if k != nil && k.Statuses != nil {
		return k.Statuses.BotStatuses()
	}
	return k.localStatuses()
}

// BotSteps returns the current turn's steps for one bot when a source or a
// live kernel has them.
func (k *Kernel) BotSteps(name string, n int) []Step {
	if k == nil {
		return nil
	}
	if k.Statuses != nil {
		return k.Statuses.BotSteps(name, n)
	}
	if k.Bot != nil && name == k.Bot.Name() {
		return k.Steps(n)
	}
	return nil
}

// NodeStatusJSON is what the node status capability returns: this machine's
// bots, idle-only, capped at nodeStatusCap bytes of valid JSON.
func NodeStatusJSON(root string) (string, error) {
	cfgs, err := bot.List(root)
	if err != nil {
		return "", err
	}
	now := time.Now()
	out := make([]BotStatus, 0, len(cfgs))
	for _, cfg := range cfgs {
		b, err := bot.Load(root, cfg.Name)
		if err != nil {
			continue
		}
		out = append(out, IdleStatus(b, now))
	}
	if out == nil {
		out = []BotStatus{}
	}
	return statusJSONCapped(out)
}

// statusJSONCapped marshals bot statuses, dropping whole bots until the JSON
// fits nodeStatusCap. The result is always valid JSON.
func statusJSONCapped(out []BotStatus) (string, error) {
	for {
		raw, err := json.Marshal(out)
		if err != nil {
			return "", err
		}
		if len(raw) <= nodeStatusCap || len(out) == 0 {
			return string(raw), nil
		}
		out = out[:len(out)-1]
	}
}

// botStatusText renders statuses and optional steps for the bot_status tool.
func botStatusText(list []BotStatus, steps []Step) string {
	var b strings.Builder
	for _, st := range list {
		b.WriteString(st.State)
		b.WriteByte('\t')
		b.WriteString(st.Bot)
		if st.Since != "" {
			b.WriteString("\tsince ")
			b.WriteString(st.Since)
		}
		if st.Tool != "" {
			b.WriteString("\ttool ")
			b.WriteString(st.Tool)
		}
		if st.Backgrounds > 0 {
			b.WriteString("\tbackgrounds ")
			b.WriteString(strconv.Itoa(st.Backgrounds))
		}
		if st.Node != "" {
			b.WriteString("\tnode ")
			b.WriteString(st.Node)
		}
		if st.StaleSeconds > 0 {
			b.WriteString("\tstale ")
			b.WriteString(strconv.Itoa(st.StaleSeconds))
			b.WriteString("s")
		}
		if st.LastMessage != "" {
			b.WriteString("\tlast: ")
			b.WriteString(st.LastMessage)
		}
		b.WriteByte('\n')
	}
	for _, s := range steps {
		b.WriteString("step\t")
		b.WriteString(s.State)
		b.WriteByte('\t')
		b.WriteString(s.Tool)
		if s.Summary != "" {
			b.WriteString("\t")
			b.WriteString(s.Summary)
		}
		b.WriteByte('\n')
	}
	out := strings.TrimRight(b.String(), "\n")
	if out == "" {
		return "no bots"
	}
	clipped, _ := clip.Bytes(out, botStatusCap)
	return clipped
}
