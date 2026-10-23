package kernel

import (
	"errors"
	"strings"
	"time"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/provider"
)

const interrupted = "interrupted before the tool returned"

// Messages copies transcript entries into provider messages.
func Messages(entries []bot.Entry) []provider.Message {
	out := make([]provider.Message, len(entries))
	for i, e := range entries {
		out[i] = e.Message
	}
	return out
}

// unresolvedTail returns tool calls that have no result and can still be answered.
// See unresolvedCalls; entries start at seq 1.
func unresolvedTail(entries []bot.Entry) ([]provider.ToolCall, error) {
	calls, _, err := unresolvedCalls(entries, 1)
	return calls, err
}

// unresolvedCalls returns unanswered tool calls in span (which starts at seq first)
// and the branch of each. Several branches can each have an open call at their own end.
// Incoming user/environment lines may follow an in-flight call. Repair appends
// its tombstone; the existing provider adapter pairs results without rewriting
// the transcript. A later assistant on that branch still indicates corruption.
func unresolvedCalls(span []bot.Entry, first int) ([]provider.ToolCall, map[string]string, error) {
	return unresolvedCallsExcept(span, first, nil)
}

// unresolvedCallsExcept leaves live branches to their executing turns.
func unresolvedCallsExcept(span []bot.Entry, first int, live map[string]bool) ([]provider.ToolCall, map[string]string, error) {
	missing := missingResults(Messages(span))
	if len(missing) == 0 {
		return nil, nil, nil
	}
	seqs := make([]bot.SeqEntry, len(span))
	for i, e := range span {
		seqs[i] = bot.SeqEntry{Seq: first + i, Entry: e}
	}
	rows, _ := bot.Replay(seqs)
	at := map[string]int{}
	branches := map[string]string{}
	for i, r := range rows {
		if r.Entry.IsForkMark() || r.Entry.Message.Role != provider.RoleAssistant {
			continue
		}
		for _, tc := range r.Entry.Message.Calls() {
			at[tc.ID] = i
			branches[tc.ID] = r.Branch
		}
	}
	var dangling []provider.ToolCall
	for _, tc := range missing {
		if live[branches[tc.ID]] {
			continue
		}
		dangling = append(dangling, tc)
		i, ok := at[tc.ID]
		if !ok {
			return nil, nil, errors.New("transcript: unanswered tool calls are not at the tail")
		}
		for _, r := range rows[i+1:] {
			if r.Branch != branches[tc.ID] || r.Entry.IsForkMark() || (r.Entry.Message.Role == provider.RoleTool || r.Entry.Message.Role == provider.RoleUser) {
				continue
			}
			return nil, nil, errors.New("transcript: unanswered tool calls are not at the tail")
		}
	}
	return dangling, branches, nil
}

func missingResults(msgs []provider.Message) []provider.ToolCall {
	answered := map[string]bool{}
	var calls []provider.ToolCall
	for _, m := range msgs {
		if m.Role == provider.RoleTool && m.CallID() != "" {
			answered[m.CallID()] = true
		}
		if m.Role == provider.RoleAssistant {
			calls = append(calls, m.Calls()...)
		}
	}
	var missing []provider.ToolCall
	for _, tc := range calls {
		if tc.ID != "" && !answered[tc.ID] {
			missing = append(missing, tc)
		}
	}
	return missing
}

func indexEvent(entries []bot.Entry, eventID string) int {
	for i, e := range entries {
		if e.EventID == eventID && e.Message.Role == provider.RoleUser {
			return i
		}
	}
	return -1
}

// turnSpan groups all inputs before the shared normal answer. A new EventID
// alone is no boundary: input is durable even while the model/tool is busy.
// An answered input (or a fork handoff) does end before the next wake.
func turnSpan(entries []bot.Entry, idx int) []bot.Entry {
	end := len(entries)
	forked := false
	for i := idx + 1; i < len(entries); i++ {
		e := entries[i]
		if e.IsForkMark() && e.Mark.Kind == bot.ForkKindStart {
			forked = true
		}
		if e.Message.Role == provider.RoleUser && e.EventID != "" && e.EventID != entries[idx].EventID {
			if done, _ := answered(entries[idx+1:i], ""); done || forked {
				end = i
				break
			}
		}
	}
	return entries[idx+1 : end]
}

// answered reports whether these lines end on a final assistant answer.
func answered(entries []bot.Entry, branch string) (bool, string) {
	pending := map[string]bool{}
	sawFinal := false
	text := ""
	for _, e := range entries {
		if e.Branch != branch {
			continue
		}
		m := e.Message
		switch m.Role {
		case provider.RoleAssistant:
			if len(m.Calls()) == 0 && len(pending) == 0 {
				sawFinal = true
				if m.Text() != "" {
					text = m.Text()
				}
				continue
			}
			sawFinal = false
			for _, tc := range m.Calls() {
				pending[tc.ID] = true
			}
		case provider.RoleTool:
			delete(pending, m.CallID())
		}
	}
	if len(pending) > 0 {
		return false, ""
	}
	return sawFinal, text
}

// turnDone reports whether the turn opened at idx already has a final answer.
// A turn that forked into the background is done once that branch closed with an
// answer; a branch that failed or was cancelled never answers, so no reply is
// invented for it.
func turnDone(entries []bot.Entry, idx int) bool {
	if idx < 0 {
		return false
	}
	if done, _ := answered(turnSpan(entries, idx), ""); done {
		return true
	}
	return forkedAndAnswered(entries, idx)
}

// forkBranch returns the branch the turn opened at idx forked into, and whether
// it closed. The opening mark must belong to this turn: scanning past the next
// user line would attach the following turn's branch to this one, so an
// unanswered request would inherit the next turn's answer.
func forkBranch(entries []bot.Entry, idx int) (branch string, closed bool) {
	span := entries[idx+1:]
	for _, e := range span {
		if e.Message.Role == provider.RoleUser && e.EventID != "" && e.EventID != entries[idx].EventID {
			break
		}
		if !e.IsForkMark() || e.Mark.Kind != bot.ForkKindStart {
			continue
		}
		branch = e.Mark.Branch
		// The branch outlives the turn's span: its lines land after the next
		// user line, so the closing mark is looked up in the whole transcript.
		for _, c := range entries {
			if c.IsForkMark() && c.Mark.Kind == bot.ForkKindEnd && c.Mark.Branch == branch {
				return branch, true
			}
		}
		return branch, false
	}
	return "", false
}

// branchLines is the branch's own lines, in order, up to its closing mark.
func branchLines(entries []bot.Entry, branch string) []bot.Entry {
	var out []bot.Entry
	for _, e := range entries {
		if e.IsForkMark() {
			if e.Mark.Kind == bot.ForkKindEnd && e.Mark.Branch == branch {
				break
			}
			continue
		}
		if e.Branch == branch {
			out = append(out, e)
		}
	}
	return out
}

// forkedAndAnswered reports whether the turn opened at idx forked into a branch
// that closed after writing a final answer.
func forkedAndAnswered(entries []bot.Entry, idx int) bool {
	branch, closed := forkBranch(entries, idx)
	if !closed {
		return false
	}
	done, _ := answered(branchLines(entries, branch), branch)
	return done
}

// lastAssistant is the newest final answer in the turn opened at idx, on the
// main line or on the branch it forked into.
func lastAssistant(entries []bot.Entry, idx int) string {
	if _, text := answered(turnSpan(entries, idx), ""); text != "" {
		return text
	}
	branch, closed := forkBranch(entries, idx)
	if !closed {
		return ""
	}
	_, text := answered(branchLines(entries, branch), branch)
	return text
}

// ReplyAfter returns the final answer to the turn that ev started, or, when
// ev arrived while another turn was running and was absorbed into it, the final
// answer of that turn. done is false while the turn is still running or never
// started; text is empty when the bot finished with no final line. It reads
// the transcript only.
func ReplyAfter(b *bot.Bot, ev link.Event) (text string, done bool, err error) {
	seq, err := b.FindEvent(ev.ID)
	if err != nil {
		return "", false, err
	}
	n, err := b.Count()
	if err != nil || n == 0 {
		return "", false, err
	}
	if seq > 0 {
		span, err := b.Slice(seq, n)
		if err != nil || !turnDone(span, 0) {
			return "", false, err
		}
		return lastAssistant(span, 0), true, nil
	}
	// Absorbed lines are recorded without an event id: find the line by its
	// text among the recent entries, then the turn it joined.
	from := max(1, n-absorbWindow+1)
	span, err := b.Slice(from, n)
	if err != nil {
		return "", false, err
	}
	want := link.Render(ev, true)
	for i, e := range span {
		if e.Message.Role != provider.RoleUser || e.EventID != "" || e.Time.Before(ev.Time.Add(-2*time.Second)) || !strings.Contains(e.Message.Text(), want) {
			continue
		}
		for j := i - 1; j >= 0; j-- {
			if span[j].Message.Role == provider.RoleUser && span[j].EventID != "" {
				if !turnDone(span, j) {
					return "", false, nil
				}
				return lastAssistant(span, j), true, nil
			}
		}
		return "", false, nil
	}
	return "", false, nil
}

// absorbWindow is how many recent entries ReplyAfter searches for an absorbed line.
const absorbWindow = 400

func lastReplyTo(entries []bot.Entry, idx int) int {
	replyTo := 0
	for _, e := range turnSpan(entries, idx) {
		if e.Message.Role == provider.RoleAssistant && len(e.Message.Calls()) == 0 {
			replyTo = e.ReplyTo
		}
	}
	return replyTo
}
