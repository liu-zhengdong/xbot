package kernel

import (
	"encoding/json"
	"strings"

	"xbot/internal/provider"
)

// Stop reasons written into the trace. Evals match these codes.
const (
	StopRepeatedOutput  = "repeated_output"
	StopSameToolArgs    = "same_tool_args"
	StopFileFlipFlop    = "file_flip_flop"
	StopPollingFailure  = "polling_failure"
	StopDeniedApprovals = "denied_approvals"

	// loopThreshold is how many consecutive identical tool calls, or identical
	// failures, count as one trip. The next one of that same key is the second trip.
	loopThreshold = 3
	// looseThreshold is the longer streak allowed for read-only tools, test and
	// build reruns, and background polls. A normal edit/test loop must not stop.
	looseThreshold = 8
)

// Breaker watches one turn for loops. The first time a pattern trips it asks
// for a reminder. The second time it stops the turn.
type Breaker struct {
	trips map[string]int

	prevAssistant string
	hadAssistant  bool

	callArgs map[string]string // tool call id -> name + args
	files    map[string][]string

	// Streaks count only consecutive repeats. Another call resets them.
	streakKey  string
	streak     int
	failKey    string
	failText   string
	failStreak int
	denyStreak int

	pendingRemind []string
	pendingStop   string
	// hardStop is set once a second trip has stopped the turn. The todo
	// completion gate checks it and does not start another step.
	hardStop bool
}

func newBreaker() *Breaker {
	return &Breaker{
		trips:    map[string]int{},
		callArgs: map[string]string{},
		files:    map[string][]string{},
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
	text := strings.TrimSpace(m.Text())
	if text != "" {
		if b.hadAssistant && text == b.prevAssistant {
			b.hit(StopRepeatedOutput, text)
		}
		b.prevAssistant = text
		b.hadAssistant = true
	}
	for _, tc := range m.Calls() {
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
	if key != b.streakKey {
		b.streakKey = key
		b.streak = 0
	}
	b.streak++
	if th := callThreshold(key); b.streak == th || b.streak == th+1 {
		b.hit(StopSameToolArgs, key)
	}
}

func (b *Breaker) seeFile(name, args string) {
	path, sig, ok := fileSig(name, args)
	if !ok {
		return
	}
	h := b.files[path]
	if len(h) >= 2 && sig == h[len(h)-2] && sig != h[len(h)-1] {
		b.hit(StopFileFlipFlop, path)
	}
	b.files[path] = append(h, sig)
}

func (b *Breaker) seeTool(m provider.Message) {
	content := strings.TrimSpace(m.Text())
	key := b.callArgs[m.CallID()]
	if key == "" {
		key = m.Name
	}
	// Denials and failures have their own counters. A successful repeat is the
	// same-tool loop. Counting those here, after the result, keeps one call
	// from tripping two detectors.
	if content == "denied by user" {
		b.breakToolStreaks()
		b.denyStreak++
		if b.denyStreak == loopThreshold {
			b.hit(StopDeniedApprovals, "")
			b.denyStreak = 0
		}
		return
	}
	b.denyStreak = 0
	if isToolError(content) {
		if b.failKey == key && b.failText == content {
			b.failStreak++
		} else {
			b.failKey = key
			b.failText = content
			b.failStreak = 1
		}
		b.streakKey = ""
		b.streak = 0
		if th := callThreshold(key); b.failStreak == th || b.failStreak == th+1 {
			b.hit(StopPollingFailure, key)
		}
		return
	}
	// A success ends the failure streak. fail, fail, ok, fail is not a poll.
	b.failKey = ""
	b.failText = ""
	b.failStreak = 0
	b.bumpTool(key)
}

func (b *Breaker) breakToolStreaks() {
	b.streakKey = ""
	b.streak = 0
	b.failKey = ""
	b.failText = ""
	b.failStreak = 0
}

// ResetSegment clears consecutive-loop state before another run inside the same
// turn. File flip-flop history stays: it describes the task, not one run's
// tool streak.
func (b *Breaker) ResetSegment() {
	if b == nil {
		return
	}
	b.prevAssistant = ""
	b.hadAssistant = false
	b.breakToolStreaks()
	b.denyStreak = 0
	for id := range b.trips {
		switch {
		case strings.HasPrefix(id, StopSameToolArgs),
			strings.HasPrefix(id, StopPollingFailure),
			strings.HasPrefix(id, StopRepeatedOutput),
			strings.HasPrefix(id, StopDeniedApprovals):
			delete(b.trips, id)
		}
	}
}

func (b *Breaker) hit(reason, key string) {
	id := reason
	if key != "" {
		id += "\n" + key
	}
	b.trips[id]++
	if b.trips[id] >= 2 {
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
		b.hardStop = true
		return "", true, reason
	}
	if len(b.pendingRemind) == 0 {
		return "", false, ""
	}
	text := loopReminder(b.pendingRemind)
	b.pendingRemind = nil
	return text, false, ""
}

// Stopped reports whether this breaker has already hard-stopped the turn.
func (b *Breaker) Stopped() bool {
	return b != nil && b.hardStop
}

func isToolError(content string) bool {
	return strings.HasPrefix(content, "error:")
}

// callThreshold is the consecutive-repeat count that makes one trip.
// Read-only tools, test and build reruns, and background polls get a longer
// leash: go test output that does not start with "error:" is still a success,
// and an edit/test loop or a few go build / go vet passes must not hard-stop.
func callThreshold(key string) int {
	name, args, _ := strings.Cut(key, "\n")
	if looseCall(name, args) {
		return looseThreshold
	}
	return loopThreshold
}

func looseCall(name, args string) bool {
	switch name {
	case "read_file", "list_secrets", "list_bots", "list_groups", "read_skill",
		"recall", "recall_history", "read_subagent", "wait", "sleep", "poll":
		return true
	case "shell", "bash", "sh", "zsh", "powershell", "cmd", "git-bash":
		return looseShell(args)
	default:
		return false
	}
}

func looseShell(args string) bool {
	var m struct {
		Command string `json:"command"`
	}
	if json.Unmarshal([]byte(args), &m) != nil {
		return false
	}
	cmd := strings.TrimSpace(m.Command)
	if i := strings.IndexAny(cmd, "\n;&|"); i >= 0 {
		cmd = strings.TrimSpace(cmd[:i])
	}
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return false
	}
	switch fields[0] {
	case "gofmt", "sleep":
		return true
	case "go":
		if len(fields) < 2 {
			return false
		}
		switch fields[1] {
		case "test", "build", "vet":
			return true
		}
	}
	return false
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
	case StopDeniedApprovals:
		return "The owner denied the last requests. Ask once what to do instead of trying them again."
	default:
		return "Stop the current approach."
	}
}
