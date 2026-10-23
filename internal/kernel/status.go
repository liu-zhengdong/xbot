package kernel

import (
	"sort"
	"strconv"
	"strings"
	"sync"
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
	botStatusCap   = 4 << 10
	tailRead       = 5
	tailHydrate    = 512
)

// BotStatus is one bot's derived state.
type BotStatus struct {
	Bot             string `json:"bot"`
	State           string `json:"state"`
	Tool            string `json:"tool,omitempty"`
	ToolSummary     string `json:"tool_summary,omitempty"`
	Since           string `json:"since,omitempty"`
	Backgrounds     int    `json:"backgrounds,omitempty"`
	Agents          int    `json:"agents,omitempty"`
	CompletedUnread int    `json:"completed_unread,omitempty"`
	LastMessage     string `json:"last_message,omitempty"`
	// Todo is one line from the bot's task list, when it has open items.
	// The steps ring does not carry the list; GET /api/bots/{name}/todos does.
	Todo string `json:"todo,omitempty"`
	// Tokens is the latest model-call usage and the session totals. Empty
	// until the kernel has seen a completion.
	Tokens *TokenUse `json:"tokens,omitempty"`
}

// Step is one tool step in a turn, kept in a small ring for the web and the
// bot_status tool.
type Step struct {
	callID  string    // private correlation key; never sent to the UI
	Time    time.Time `json:"time"`
	Tool    string    `json:"tool"`
	Summary string    `json:"summary,omitempty"`
	State   string    `json:"state"`
}

// StatusSource lets a kernel read every same-owner bot's status. The web
// server implements it over its live kernels. A nil source falls back to this
// process's own bots.
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

// tailMessage returns the last assistant or user message preview and its time,
// reading only the transcript tail. It never asks the model.
//
// The status endpoint is polled, and reading the tail takes the transcript
// lock and checks the index for every bot on every poll. The raw line is
// cached against the transcript's stamp, which moves on every append from any
// process, so an unchanged transcript costs two stats. The redaction runs on
// every call, not into the cache, so a secret added to the vault later is
// still masked.
func tailMessage(b *bot.Bot) (string, time.Time) {
	if b == nil {
		return "", time.Time{}
	}
	key := bot.Dir(b.Root, b.Name())
	stamp, ok := b.TranscriptStamp()
	if ok {
		tails.mu.Lock()
		c, hit := tails.m[key]
		tails.mu.Unlock()
		if hit && c.stamp == stamp {
			return tailPreview(c.content), c.at
		}
	}
	content, at := readTail(b)
	if ok {
		tails.mu.Lock()
		if tails.m == nil || len(tails.m) >= tailCacheMax {
			tails.m = map[string]tailEntry{}
		}
		tails.m[key] = tailEntry{stamp: stamp, content: content, at: at}
		tails.mu.Unlock()
	}
	return tailPreview(content), at
}

// tailCacheMax bounds the cache; past it the map starts over. A machine has a
// handful of bots, so this only matters for a process that sees many.
const tailCacheMax = 256

type tailEntry struct {
	stamp   string
	content string
	at      time.Time
}

var tails struct {
	mu sync.Mutex
	m  map[string]tailEntry
}

func tailPreview(content string) string {
	if content == "" {
		return ""
	}
	return clipStatusPreview(bot.RedactText(content), statusPreview)
}

// readTail finds the last assistant or user line in the transcript tail and
// returns it on one line, before redaction.
func readTail(b *bot.Bot) (string, time.Time) {
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
		content := e.Message.Text()
		if strings.TrimSpace(content) == "" && e.Blob != "" {
			if h, err := b.HydrateLimit(e, tailHydrate); err == nil {
				content = h.Message.Text()
			}
		}
		content = strings.TrimSpace(oneStatusLine(content))
		if content == "" {
			continue
		}
		if e.IsInternal() {
			continue
		}
		if _, ok := bot.ParseApprovalCard(content); ok {
			continue
		}
		return content, e.Time
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
	tool := ""
	started := t.started
	var toolStart time.Time
	// Show the longest-running call, even when a later parallel call finished.
	for _, step := range t.steps {
		if active && (step.State == StepRunning || (awaiting && step.State == StepAwaiting)) {
			tool, toolStart, st.ToolSummary = step.Tool, step.Time, step.Summary
			break
		}
	}
	t.mu.Unlock()
	st.Backgrounds = len(k.Backgrounds())
	if active, ok := k.execSnapshot().(interface{ ActiveCount() int }); ok {
		st.Agents = active.ActiveCount()
	}
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
	case active || bgs > 0 || st.Agents > 0:
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
func (k *Kernel) beginStep(t *turn, id, name, summary string) {
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
	t.steps = append(t.steps, Step{callID: id, Time: t.lastToolStart, Tool: name, Summary: line, State: StepRunning})
	t.steps = trimSteps(t.steps)
}

// endStep records a tool call finishing. output is the tool result, whose
// "error:" prefix or non-zero "exit N" first line (shell) marks a failed step.
func (k *Kernel) endStep(t *turn, id, output string) {
	if t == nil {
		return
	}
	state := StepDone
	if strings.HasPrefix(strings.TrimSpace(output), "error:") || failedExit(output) {
		state = StepError
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.lastToolEnd = k.now()
	for i := len(t.steps) - 1; i >= 0; i-- {
		if t.steps[i].callID == id && (t.steps[i].State == StepRunning || t.steps[i].State == StepAwaiting) {
			t.steps[i].State = state
			t.steps = trimSteps(t.steps)
			return
		}
	}
}

// trimSteps bounds finished history without discarding live calls. Storage is
// bounded by max(stepRingCap, the number of concurrent calls); Steps still
// exposes at most stepRingCap records.
func trimSteps(steps []Step) []Step {
	excess := len(steps) - stepRingCap
	if excess <= 0 {
		return steps
	}
	n := 0
	for _, step := range steps {
		if excess > 0 && (step.State == StepDone || step.State == StepError) {
			excess--
			continue
		}
		steps[n] = step
		n++
	}
	clear(steps[n:])
	return steps[:n]
}

// beginAwait marks that a turn is blocked on the owner's approval.
func (k *Kernel) beginAwait(t *turn, id string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.awaiting = true
	t.awaitingSince = k.now()
	for i := len(t.steps) - 1; i >= 0; i-- {
		if t.steps[i].callID == id && t.steps[i].State == StepRunning {
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
// web server's source covers its live kernels; without one only this process
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

// failedExit reports a shell result whose first line is "exit N" with N != 0,
// so an allowed command that failed is not shown as a finished step.
func failedExit(output string) bool {
	line, _, _ := strings.Cut(strings.TrimSpace(output), "\n")
	code, ok := strings.CutPrefix(line, "exit ")
	if !ok {
		return false
	}
	n, err := strconv.Atoi(strings.TrimSpace(code))
	return err == nil && n != 0
}
