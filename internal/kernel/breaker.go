package kernel

import (
	"encoding/json"
	"strings"

	"xbot/internal/compact"
	"xbot/internal/provider"
)

// Stop reasons written into the trace. Evals match these codes.
const (
	StopRepeatedOutput     = "repeated_output"
	StopSameToolArgs       = "same_tool_args"
	StopFileFlipFlop       = "file_flip_flop"
	StopPollingFailure     = "polling_failure"
	StopDiminishingReturns = "diminishing_returns"
	StopDeniedApprovals    = "denied_approvals"

	// diminishingLimit is the per-continuation token ceiling. Three or more
	// continuation segments whose last two each add fewer assistant tokens
	// than this are diminishing returns.
	diminishingLimit = 500
	// loopThreshold is how many identical tool calls, or identical failures,
	// count as one trip. The next one is the second trip.
	loopThreshold = 3
)

// Breaker watches one turn for loops. The first time a pattern trips it asks
// for a reminder. The second time it stops the turn.
type Breaker struct {
	trips map[string]int

	prevAssistant string
	hadAssistant  bool

	toolCounts map[string]int
	callArgs   map[string]string // tool call id -> name + args
	failCount  map[string]int
	failPrev   map[string]string
	files      map[string][]string

	denyStreak int

	// contTokens is assistant tokens added by each closed continuation.
	// openCont is the continuation currently running; the opening segment
	// is not one.
	contTokens []int
	openCont   bool
	openTokens int

	pendingRemind []string
	pendingStop   string
}

func newBreaker() *Breaker {
	return &Breaker{
		trips:      map[string]int{},
		toolCounts: map[string]int{},
		callArgs:   map[string]string{},
		failCount:  map[string]int{},
		failPrev:   map[string]string{},
		files:      map[string][]string{},
	}
}

// Observe folds one model step into the counters and reports what to do.
// remind is empty when nothing new fired. stop is the second trip; reason is
// the stable stop code.
func (b *Breaker) Observe(msgs []provider.Message) (remind string, stop bool, reason string) {
	if b == nil {
		return "", false, ""
	}
	for _, m := range msgs {
		switch m.Role {
		case provider.RoleAssistant:
			b.seeAssistant(m)
		case provider.RoleTool:
			b.seeTool(m)
		}
	}
	return b.take()
}

func (b *Breaker) seeAssistant(m provider.Message) {
	text := strings.TrimSpace(m.Content)
	if text != "" {
		if b.hadAssistant && text == b.prevAssistant {
			b.hit(StopRepeatedOutput)
		}
		b.prevAssistant = text
		b.hadAssistant = true
	}
	// Only a continuation segment counts toward diminishing returns. A short
	// tool step in the opening segment is ordinary work.
	if b.openCont {
		b.openTokens += compact.EstimateMessage(m)
	}
	for _, tc := range m.ToolCalls {
		key := tc.Function.Name + "\n" + tc.Function.Arguments
		if tc.ID != "" {
			b.callArgs[tc.ID] = key
		}
		b.seeFile(tc.Function.Name, tc.Function.Arguments)
	}
}

func (b *Breaker) bumpTool(key string) {
	if key == "" {
		return
	}
	b.toolCounts[key]++
	if c := b.toolCounts[key]; c == loopThreshold || c == loopThreshold+1 {
		b.hit(StopSameToolArgs)
	}
}

func (b *Breaker) seeFile(name, args string) {
	path, sig, ok := fileSig(name, args)
	if !ok {
		return
	}
	h := b.files[path]
	if len(h) >= 2 && sig == h[len(h)-2] && sig != h[len(h)-1] {
		b.hit(StopFileFlipFlop)
	}
	b.files[path] = append(h, sig)
}

func (b *Breaker) seeTool(m provider.Message) {
	content := strings.TrimSpace(m.Content)
	key := b.callArgs[m.ToolCallID]
	if key == "" {
		key = m.Name
	}
	// Denials and failures have their own counters. A successful repeat is the
	// same-tool loop. Counting those here, after the result, keeps one call
	// from tripping two detectors.
	if content == "denied by user" {
		b.denyStreak++
		if b.denyStreak == loopThreshold {
			b.hit(StopDeniedApprovals)
			b.denyStreak = 0
		}
		return
	}
	b.denyStreak = 0
	if isToolError(content) {
		if b.failPrev[key] == content {
			b.failCount[key]++
		} else {
			b.failCount[key] = 1
			b.failPrev[key] = content
		}
		if c := b.failCount[key]; c == loopThreshold || c == loopThreshold+1 {
			b.hit(StopPollingFailure)
		}
		return
	}
	b.bumpTool(key)
}

// BeginContinuation opens one continuation turn: a background segment started
// because the previous segment hit its step cap. It first closes the previous
// continuation, which is when diminishing returns can trip. The opening
// segment is not a continuation. A stop means the new segment must not start.
func (b *Breaker) BeginContinuation() (remind string, stop bool, reason string) {
	if b == nil {
		return "", false, ""
	}
	b.closeContinuation()
	remind, stop, reason = b.take()
	if !stop {
		b.openCont = true
		b.openTokens = 0
	}
	return remind, stop, reason
}

// EndContinuation closes the open continuation, if any. The caller uses it
// when the background is not going to start another segment.
func (b *Breaker) EndContinuation() (remind string, stop bool, reason string) {
	if b == nil {
		return "", false, ""
	}
	b.closeContinuation()
	return b.take()
}

// closeContinuation records the assistant tokens of the open continuation.
// Three or more continuations, with the last two each under the token ceiling,
// are one trip.
func (b *Breaker) closeContinuation() {
	if !b.openCont {
		return
	}
	b.contTokens = append(b.contTokens, b.openTokens)
	b.openCont = false
	n := len(b.contTokens)
	if n >= 3 && b.contTokens[n-1] < diminishingLimit && b.contTokens[n-2] < diminishingLimit {
		b.hit(StopDiminishingReturns)
	}
}

func (b *Breaker) hit(reason string) {
	b.trips[reason]++
	if b.trips[reason] >= 2 {
		if b.pendingStop == "" {
			b.pendingStop = reason
		}
		return
	}
	b.pendingRemind = append(b.pendingRemind, reason)
}

func (b *Breaker) take() (string, bool, string) {
	if b.pendingStop != "" {
		reason := b.pendingStop
		b.pendingStop = ""
		b.pendingRemind = nil
		return "", true, reason
	}
	if len(b.pendingRemind) == 0 {
		return "", false, ""
	}
	text := loopReminder(b.pendingRemind)
	b.pendingRemind = nil
	return text, false, ""
}

func isToolError(content string) bool {
	return strings.HasPrefix(content, "error:")
}

func fileSig(name, args string) (path, sig string, ok bool) {
	var m map[string]string
	if json.Unmarshal([]byte(args), &m) != nil {
		return "", "", false
	}
	path = strings.TrimSpace(m["path"])
	if path == "" {
		return "", "", false
	}
	switch name {
	case "write_file":
		return path, "w:" + m["content"], true
	case "edit_file":
		return path, "e:" + m["old_string"] + "\x00" + m["new_string"], true
	default:
		return "", "", false
	}
}

// loopReminder is the soft warning. polling_failure carries the retry guidance
// that used to sit in the always-on rules.
func loopReminder(reasons []string) string {
	var b strings.Builder
	b.WriteString("<system-reminder>\n")
	b.WriteString("Loop warning. You are repeating work. Change approach, or stop and report what is blocked.\n")
	seen := map[string]bool{}
	for _, r := range reasons {
		if seen[r] {
			continue
		}
		seen[r] = true
		b.WriteString("- ")
		b.WriteString(r)
		b.WriteString(": ")
		b.WriteString(loopHint(r))
		b.WriteByte('\n')
	}
	b.WriteString("</system-reminder>")
	return b.String()
}

func breakerHaltReason(why string) string {
	if why == "" {
		why = "unspecified"
	}
	return "熔断 " + why
}

func loopHint(reason string) string {
	switch reason {
	case StopRepeatedOutput:
		return "This reply matches the previous one. Do not send it again."
	case StopSameToolArgs:
		return "The same tool was called with the same arguments. Do not call it that way again."
	case StopFileFlipFlop:
		return "This file was edited back and forth. Stop flipping it and decide."
	case StopPollingFailure:
		return "The same call failed again. Do not retry it. Report the exact error."
	case StopDiminishingReturns:
		return "The last steps added very little. Summarize and stop, or do one different thing."
	case StopDeniedApprovals:
		return "The owner denied the last requests. Ask once what to do instead of trying them again."
	default:
		return "Stop the current approach."
	}
}
