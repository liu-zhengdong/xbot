package kernel

import (
	"context"
	"fmt"
	"sync"

	"xbot/internal/bot"
	"xbot/internal/compact"
	"xbot/internal/provider"
)

// turnFit keeps the requests of one turn under the two lines while the turn
// runs. Compaction between turns (autoCompact, the background job) cannot
// reach a turn's own messages: they are protected until the turn ends. A long
// tool-heavy turn, a background one above all, would otherwise grow past the
// ceiling and then past the model's window inside a single turn.
//
// It changes only what is sent. The agent's conversation and the transcript
// keep every message whole. Each decision is kept, so the next request is cut
// the same way and its prefix stays what the model already saw; a cut only
// ever grows.
//
// Three steps, cheapest first:
//  1. past the target, the larger results of this turn's earlier steps are
//     left out (a note stands in for each);
//  2. if that is not enough and this turn's earlier steps are worth it, or the
//     ceiling is still crossed, those steps are summarized into one note;
//  3. past the ceiling still, the larger results of earlier turns are left out
//     too.
//
// A request the provider rejects as too long is cut with all three and sent
// once more.
type turnFit struct {
	mu sync.Mutex
	// pre is the number of messages assembled at the turn's start. Everything
	// from it on was produced by this turn. The agent only appends, so an
	// index names the same message in every request of the turn.
	pre int
	// omitted holds the indices of tool results left out.
	omitted map[int]bool
	// cut, when past pre, means messages [pre, cut) are sent as summary.
	cut     int
	summary string
}

// omitMin is the smallest result worth leaving out, in estimated tokens.
// Below it the saving does not pay for the cache miss.
const omitMin = 512

// turnFoldShare: this turn's earlier steps are summarized past the target
// only when they are at least this share of it, so a turn is not summarized
// for a few thousand tokens.
const turnFoldShare = 4

func newTurnFit(pre int) *turnFit {
	return &turnFit{pre: pre, omitted: map[int]bool{}}
}

// start is the first message of this turn that is still sent verbatim.
func (f *turnFit) start() int {
	if f.cut > f.pre {
		return f.cut
	}
	return f.pre
}

// view is msgs as they will be sent: results left out, earlier steps summarized.
func (f *turnFit) view(msgs []provider.Message) []provider.Message {
	if len(f.omitted) == 0 && f.cut <= f.pre {
		return msgs
	}
	out := make([]provider.Message, 0, len(msgs)+1)
	for i, m := range msgs {
		if f.cut > f.pre && i >= f.pre && i < f.cut {
			if i == f.pre {
				out = append(out, turnSummary(f.summary))
			}
			continue
		}
		if f.omitted[i] {
			m = omittedResult(m)
		}
		out = append(out, m)
	}
	return out
}

func omittedResult(m provider.Message) provider.Message {
	n := compact.EstimateMessage(m)
	m.Content = fmt.Sprintf("[result left out to keep the request inside the context window: about %d tokens. Run the call again if you still need it.]", n)
	m.Images = nil
	return m
}

func turnSummary(text string) provider.Message {
	return provider.Message{
		Role:     provider.RoleUser,
		Internal: true,
		Content:  "<turn_progress>\nThe earlier steps of this turn were summarized to keep the request inside the context window. The steps after this note are verbatim.\n\n" + text + "\n</turn_progress>",
	}
}

// latestStep is the index of the newest assistant message at or after from:
// it and what follows are the step the model is working on, kept whole.
func latestStep(msgs []provider.Message, from int) int {
	for i := len(msgs) - 1; i >= from; i-- {
		if msgs[i].Role == provider.RoleAssistant {
			return i
		}
	}
	return len(msgs)
}

// omit leaves out tool results in [from, to), oldest first, until size is at
// most goal. It returns the new size.
func (k *Kernel) omit(f *turnFit, msgs []provider.Message, from, to, size, goal int) int {
	for i := from; i < to && i < len(msgs) && size > goal; i++ {
		m := msgs[i]
		if m.Role != provider.RoleTool || f.omitted[i] {
			continue
		}
		if f.cut > f.pre && i >= f.pre && i < f.cut {
			continue
		}
		old := compact.EstimateMessage(m)
		if old < omitMin {
			continue
		}
		f.omitted[i] = true
		size -= k.scaled(old) - k.scaled(compact.EstimateMessage(omittedResult(m)))
	}
	return size
}

// foldTurn summarizes this turn's steps before keep into one note. The
// previous note, if any, is the prior summary, so the new one covers both.
func (k *Kernel) foldTurn(ctx context.Context, f *turnFit, msgs []provider.Message, keep int) error {
	start := f.start()
	if keep <= start || k.Compactor == nil {
		return nil
	}
	turns := make([]provider.Message, 0, keep-start)
	for i := start; i < keep; i++ {
		m := msgs[i]
		if f.omitted[i] {
			m = omittedResult(m)
		}
		m.Images = nil
		turns = append(turns, m)
	}
	text, err := k.Compactor.Compact(ctx, f.summary, turns)
	if err != nil {
		return err
	}
	f.summary, f.cut = text, keep
	return nil
}

// fit returns req cut to fit. overflow says the provider already rejected it
// as too long: the estimate was low, so every step runs.
func (k *Kernel) fit(ctx context.Context, t *turn, f *turnFit, req provider.Request, overflow bool) provider.Request {
	if k == nil || f == nil {
		return req
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if overflow {
		k.overflowed()
	}
	msgs := req.Messages
	if f.pre > len(msgs) {
		// Not this turn's conversation (a summary request after a stop is
		// still a prefix of it, so this does not happen in practice).
		return req
	}
	measure := func() int { return k.scaled(compact.EstimateMessages(f.view(msgs))) }
	target, ceiling := k.target(), k.ceiling()
	size := measure()
	if size <= target && !overflow {
		req.Messages = f.view(msgs)
		return req
	}
	goal := target * 3 / 4
	keep := latestStep(msgs, f.start())
	before := len(f.omitted)
	size = k.omit(f, msgs, f.start(), keep, size, goal)
	if len(f.omitted) > before {
		k.noteFit(t, "omit")
	}
	if overflow || size > ceiling || (size > target && k.earlier(f, msgs, keep) >= target/turnFoldShare) {
		if keep > f.start() {
			if err := k.foldTurn(ctx, f, msgs, keep); err == nil {
				k.noteFit(t, "fold_turn")
			} else {
				k.noteFit(t, "fold_turn_failed")
			}
			size = measure()
		}
	}
	if overflow || size > ceiling {
		before = len(f.omitted)
		size = k.omit(f, msgs, 0, f.pre, size, goal)
		if len(f.omitted) > before {
			k.noteFit(t, "omit_earlier")
		}
	}
	req.Messages = f.view(msgs)
	return req
}

// earlier is what this turn's steps before keep take in the request now.
func (k *Kernel) earlier(f *turnFit, msgs []provider.Message, keep int) int {
	sent := f.view(msgs)[f.pre:]
	return k.scaled(compact.EstimateMessages(sent)) - k.scaled(compact.EstimateMessages(msgs[keep:]))
}

func (k *Kernel) noteFit(t *turn, reason string) {
	if t != nil && reason != "fold_turn_failed" {
		t.shrunk.Store(true)
	}
	if k.Bot != nil {
		_ = k.Bot.LogTrace(bot.TraceFit, reason, k.now())
	}
}
