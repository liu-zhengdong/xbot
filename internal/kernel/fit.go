package kernel

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

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
	mu      sync.Mutex
	reserve int // next full environment, counted before fitting not after sending
	// pre is the number of messages assembled at the turn's start. Everything
	// from it on was produced by this turn. The agent only appends, so an
	// index names the same message in every request of the turn.
	pre int
	// omitted holds the indices of tool results left out, each with its
	// transcript sequence (0 when it is not known), so the note can say where
	// expand finds the result.
	omitted map[int]int
	// cut, when past pre, means messages [pre, cut) are sent as summary.
	// from and to are the transcript range the summary covers (0 when not
	// known).
	cut      int
	summary  string
	from, to int
	// seqOf maps an index of this turn to its transcript sequence, or 0.
	seqOf func(i int) int
	// noSeq, when set, is what the note says for a result without a
	// sequence, in place of the pointer to recall_history.
	noSeq string
}

// omitMin is the smallest result worth leaving out, in estimated tokens.
// Below it the saving does not pay for the cache miss.
const omitMin = 512

// turnFoldShare: this turn's earlier steps are summarized past the target
// only when they are at least this share of it, so a turn is not summarized
// for a few thousand tokens.
const turnFoldShare = 4

func newTurnFit(pre int, seqOf func(i int) int) *turnFit {
	return &turnFit{pre: pre, omitted: map[int]int{}, seqOf: seqOf}
}

// seq is the transcript sequence of message i, or 0. Only this turn's
// messages have one here: earlier turns reach the request through the
// projection, which does not keep a one-to-one index.
func (f *turnFit) seq(i int) int {
	if i < f.pre || f.seqOf == nil {
		return 0
	}
	return f.seqOf(i)
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
				out = append(out, turnSummary(f.summary, f.from, f.to))
			}
			continue
		}
		if seq, ok := f.omitted[i]; ok {
			m = f.omittedResult(m, seq)
		}
		out = append(out, m)
	}
	return out
}

// omittedResult is the note that stands in for a result. The transcript keeps
// the result whole, so the note points at expand rather than at running the
// call again: a rerun can be slow, and a file read again may have changed
// since.
func (f *turnFit) omittedResult(m provider.Message, seq int) provider.Message {
	n := compact.EstimateMessage(m)
	if seq > 0 {
		m.SetText(fmt.Sprintf("[result left out to keep the request inside the context window: about %d tokens, transcript seq %d. Call expand with from=%d to=%d to read it as it was; do not run the call again for that.]", n, seq, seq, seq))
	} else if f.noSeq != "" {
		m.SetText(fmt.Sprintf("[result left out to keep the request inside the context window: about %d tokens. %s]", n, f.noSeq))
	} else {
		m.SetText(fmt.Sprintf("[result left out to keep the request inside the context window: about %d tokens. The transcript keeps it: find it with recall_history, then expand.]", n))
	}
	m.Images = nil
	return m
}

func turnSummary(text string, from, to int) provider.Message {
	where := ""
	if from > 0 && to >= from {
		where = fmt.Sprintf(" from=\"%d\" to=\"%d\"", from, to)
	}
	return provider.Message{Role: provider.RoleUser, Internal: true, Content: provider.TextContent("<turn_progress" + where + ">\nThe earlier steps of this turn were summarized to keep the request inside the context window. The transcript keeps them whole: expand the range to read them. The steps after this note are verbatim.\n\n" + text + "\n</turn_progress>")}
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
		if _, done := f.omitted[i]; m.Role != provider.RoleTool || done {
			continue
		}
		if f.cut > f.pre && i >= f.pre && i < f.cut {
			continue
		}
		old := compact.EstimateMessage(m)
		if old < omitMin {
			continue
		}
		seq := k.lineSeq(f, msgs, i)
		f.omitted[i] = seq
		size -= k.scaled(old) - k.scaled(compact.EstimateMessage(f.omittedResult(m, seq)))
	}
	return size
}

// lineSeq is the transcript sequence of msgs[i] after checking that the
// stored line is that message (same role, same tool call), or 0. A pairing
// shifted by a failed write must not point expand at the wrong line.
func (k *Kernel) lineSeq(f *turnFit, msgs []provider.Message, i int) int {
	seq := f.seq(i)
	if seq <= 0 || k.Bot == nil || i >= len(msgs) {
		return 0
	}
	got, err := k.Bot.Slice(seq, seq)
	if err != nil || len(got) != 1 {
		return 0
	}
	if e := got[0].Message; e.Role != msgs[i].Role || e.CallID() != msgs[i].CallID() {
		return 0
	}
	return seq
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
		// Summarize the original facts, not the omission placeholders sent earlier.
		m.Images = nil
		turns = append(turns, m)
	}
	text, err := k.foldFacts(ctx, f.summary, turns)
	if err != nil {
		return err
	}
	if f.from == 0 {
		f.from = k.lineSeq(f, msgs, f.pre)
	}
	f.summary, f.cut, f.to = text, keep, k.lineSeq(f, msgs, keep-1)
	if f.from == 0 || f.to < f.from {
		f.from, f.to = 0, 0
	}
	return nil
}

// foldFacts uses the existing compactor in bounded batches. Restoring omitted
// originals must not turn the summary call itself into an oversized request.
func (k *Kernel) foldFacts(ctx context.Context, prior string, msgs []provider.Message) (string, error) {
	limit := min(256<<10, max(16<<10, k.ceiling()*2))
	// Half the batch is reserved for the previous 8 KiB summary, rules and JSON.
	partLimit := limit / 2
	var batch []provider.Message
	size := 0
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		text, err := k.compactor(turnSeqOf(ctx)).Compact(ctx, strings.TrimSpace(prior+"\n"+summaryFacts), batch)
		if err != nil {
			return err
		}
		prior = text
		batch = nil
		size = 0
		return nil
	}
	for _, msg := range msgs {
		text := msg.PortableText()
		split := false
		for _, tc := range msg.Calls() {
			text += "\n tool " + tc.Function.Name + " " + tc.Function.Arguments
		}
		for len(text) > partLimit {
			n := partLimit
			for n > 0 && !utf8.RuneStart(text[n]) {
				n--
			}
			part := provider.Message{Role: msg.Role, Content: provider.TextContent(text[:n])}
			if size+len(part.Text()) > partLimit {
				if err := flush(); err != nil {
					return "", err
				}
			}
			batch = append(batch, part)
			size += len(part.Text()) + 64
			text = text[n:]
			if err := flush(); err != nil {
				return "", err
			}
			split = true
		}
		// Batches are portable text once split; keeping opaque parts here
		// would send their full summaries again alongside the remainder.
		if split {
			msg.Content = provider.TextContent(text)
		}
		n := len(text) + 64
		if size+n > partLimit {
			if err := flush(); err != nil {
				return "", err
			}
		}
		batch = append(batch, msg)
		size += n
	}
	if err := flush(); err != nil {
		return "", err
	}
	return prior, nil
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
	measure := func() int { return k.scaled(compact.EstimateMessages(f.view(msgs))) + f.reserve }
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
