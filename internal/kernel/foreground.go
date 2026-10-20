package kernel

import (
	"context"
	"encoding/json"
	"errors"

	"xbot/internal/link"
	"xbot/internal/review"
	"xbot/internal/tools"
)

// The foreground under ADR 0016: it understands, plans, dispatches, reports,
// and keeps memory. What it may do is decided here, by tool name and
// arguments, not by asking the model to hold back.

// DispatchRule tells the foreground model its job. It is added to the rules
// only while dispatch is on.
const DispatchRule = "You are the foreground: you talk with the owner, work out what they want, plan, hand work out, report results, and keep memory. You do not do the work yourself. Answer directly when a few quick reads are enough (a file, a directory listing, memory, the clock). Anything that changes files, runs commands that write, or takes more than a few steps goes to a subagent: call dispatch with the right type and a task that says everything the subagent needs, since it starts with an empty context (goal, paths, constraints, what done looks like). Then tell the owner in a sentence or two what you handed out. When a subagent's result arrives, tell the owner what came of it and record anything worth keeping in memory. While a subagent runs, use steer, queue, interrupt, or cancel to change its course; do not start a second one for the same work."

// dispatching reports whether the foreground hands out work. It needs the
// agents link: without dispatch the work would have nowhere to go.
func (k *Kernel) dispatching() bool {
	return k != nil && k.Dispatch && k.hasLink(link.NameAgents)
}

// foregroundHidden names the tools the foreground never sees while dispatching:
// the ones whose only use is doing the work. File edits and the shell stay,
// limited at run time (foregroundRefuses), because memory is edited with the
// file tools and a plain read through the shell is a quick look.
var foregroundHidden = map[string]bool{
	"spawn":          true,
	"write_skill":    true,
	"http_request":   true,
	"code_start":     true,
	"code_steer":     true,
	"code_queue":     true,
	"code_interrupt": true,
	"code_cancel":    true,
	"desktop_click":  true,
	"desktop_type":   true,
	"desktop_scroll": true,
	"desktop_key":    true,
	"browser_open":   true,
	"browser_click":  true,
}

// foregroundKept are the tools in a work class (below) that the foreground
// keeps anyway, each for a reason of its own. Everything else in those
// classes is hidden, including tools added later, so a new tool that writes
// or sends never reaches the foreground by default.
var foregroundKept = map[string]string{
	"write_file":      "memory only (foregroundRefuses)",
	"edit_file":       "memory only (foregroundRefuses)",
	"shell":           "plain reads only (foregroundRefuses)",
	"propose_persona": "the bot's own persona, like memory",
	"propose_profile": "the bot's own profile, like memory",
	"links":           "managing the link catalog the owner asks for",
	"delete_bot":      "managing the owner's bots on request",
}

// workClass reports the review classes that do the work: changing files,
// running commands, sending out, deleting.
func workClass(c review.Class) bool {
	switch c {
	case review.Delete, review.ExternalSend, review.HTTP, review.Shell, review.SelfEdit:
		return true
	}
	return false
}

func toolClass(t tools.Tool) review.Class {
	if h, ok := t.(review.ReadOnlyHinter); ok {
		return review.ClassOfMCP(t.Name(), h.ReadOnlyHint())
	}
	return review.ClassOf(t.Name())
}

func (k *Kernel) foregroundTools(list []tools.Tool) []tools.Tool {
	if !k.dispatching() {
		return list
	}
	out := list[:0:0]
	for _, t := range list {
		name := t.Name()
		if foregroundHidden[name] {
			continue
		}
		if _, kept := foregroundKept[name]; !kept && workClass(toolClass(t)) {
			continue
		}
		out = append(out, t)
	}
	return out
}

// errForeground is what the model reads when it tries to do the work itself.
var errForeground = errors.New("the foreground does not do this itself: hand it to a subagent with dispatch (dev for changes, research for looking things up)")

// foregroundRefuses blocks the work tools that stay visible: a file edit
// outside a memory folder, and a shell command that is not a plain read.
func (k *Kernel) foregroundRefuses(ctx context.Context, tool tools.Tool, args json.RawMessage) error {
	if !k.dispatching() || k.turnFor(ctx).isForked() {
		return nil
	}
	switch tool.Name() {
	case "write_file", "edit_file":
		if _, dir, _, _ := k.memoryCall(ctx, tool, args); dir == "" {
			return errForeground
		}
	case "shell":
		if !review.ShellIsReadOnly(args) {
			return errForeground
		}
	}
	return nil
}

// capTools is the tool set once the foreground has used its step limit: only
// handing the rest out and talking to the owner remain.
var capTools = map[string]bool{
	"dispatch": true, "steer": true, "queue": true, "interrupt": true, "cancel": true,
	"ask_user": true, "read_subagent": true,
}

// capGrace is how many model steps past the cap the foreground gets to
// dispatch and answer before the run is stopped. Steps, not tool calls: a call
// to a tool it no longer has does not count as one.
const capGrace = 2

// capNote is the reminder the foreground gets when it reaches its limit.
const capNote = "<foreground_limit>\nYou have used the foreground's step limit. Do not keep working here: hand what is left to a subagent with dispatch now, then answer the owner in a sentence or two. Only dispatch and the subagent controls are available until the turn ends.\n</foreground_limit>"

// foregroundCap reports what the guard does at a step boundary while
// dispatching: the reminder to attach the first time the cap is reached, and
// whether to stop because the grace steps are used up.
func (k *Kernel) foregroundCap(t *turn) (remind string, stop bool) {
	if !k.dispatching() || t == nil {
		return "", false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.branch != "" {
		return "", false
	}
	if !t.capped {
		if !k.foregroundCapped(t) {
			return "", false
		}
		t.capped = true
		return capNote, false
	}
	t.capSteps++
	return "", t.capSteps >= capGrace
}

// cappedTools narrows the list once the cap was reached in this turn.
func (k *Kernel) cappedTools(t *turn, list []tools.Tool) []tools.Tool {
	if t == nil {
		return list
	}
	t.mu.Lock()
	capped := t.capped
	t.mu.Unlock()
	if !capped {
		return list
	}
	out := list[:0:0]
	for _, tl := range list {
		if capTools[tl.Name()] {
			out = append(out, tl)
		}
	}
	return out
}
