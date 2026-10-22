package kernel

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"xbot/internal/bot"
	"xbot/internal/code"
	"xbot/internal/compact"
	"xbot/internal/provider"
)

// ActiveBlocks returns summary blocks that are not contained in a coarser block.
func ActiveBlocks(blocks []bot.SummaryBlock) []bot.SummaryBlock {
	var out []bot.SummaryBlock
	for i, b := range blocks {
		if b.From < 1 || b.To < b.From || strings.TrimSpace(b.Text) == "" {
			continue
		}
		superseded := false
		for j, o := range blocks {
			if i == j || o.From < 1 || o.To < o.From || strings.TrimSpace(o.Text) == "" {
				continue
			}
			wider := o.From <= b.From && o.To >= b.To && (o.To-o.From > b.To-b.From)
			sameLater := o.From == b.From && o.To == b.To && j > i
			if wider || sameLater {
				superseded = true
				break
			}
		}
		if !superseded {
			out = append(out, b)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].From == out[j].From {
			return out[i].To < out[j].To
		}
		return out[i].From < out[j].From
	})
	return out
}

func project(entries []bot.Entry, blocks []bot.SummaryBlock) []provider.Message {
	active := ActiveBlocks(blocks)
	covered := coverage(len(entries), active)
	var raw []bot.Entry
	var seqs []int
	for i, e := range entries {
		if covered[i+1] {
			continue
		}
		raw = append(raw, e)
		seqs = append(seqs, i+1)
	}
	return projected(blocks, raw, seqs)
}

// projected is summaries and the lines outside every active block, merged back
// into transcript order. seqs[i] is the sequence number of uncovered[i].
//
// The merge is what keeps a gap readable. A manual compact can summarize a
// middle range and leave raw lines on both sides, so the active blocks do not
// always cover a leading prefix. Emitting every summary first would present
// those raw lines after a summary of later messages, and the model would read
// the transcript out of order. Sorting by sequence number keeps one timeline:
// a summary occupies the place of the range it covers.
//
// A nil seqs means the caller cannot supply positions; the summaries then come
// first, which is the order for a covered leading prefix.
func projected(blocks []bot.SummaryBlock, uncovered []bot.Entry, seqs []int) []provider.Message {
	active := ActiveBlocks(blocks)
	type item struct {
		seq int
		msg provider.Message
	}
	items := make([]item, 0, len(active)+len(uncovered))
	for _, b := range active {
		items = append(items, item{seq: b.From, msg: summaryMessage(b)})
	}
	positioned := len(seqs) == len(uncovered)
	for i, e := range uncovered {
		if code.IsCard(e.Message.Content) {
			continue
		}
		if bot.IsApprovalCard(e.Message.Content) {
			continue
		}
		if e.IsForkMark() {
			continue
		}
		seq := 0
		if positioned {
			seq = seqs[i]
		}
		m := e.Message
		m.Seq, m.Internal = seq, e.IsInternal()
		items = append(items, item{seq: seq, msg: m})
	}
	if positioned {
		// Summaries sort before a raw line that starts at the same place; the
		// two cannot overlap, so this only settles a malformed state.
		sort.SliceStable(items, func(i, j int) bool { return items[i].seq < items[j].seq })
	}
	msgs := make([]provider.Message, 0, len(items))
	for _, it := range items {
		msgs = append(msgs, it.msg)
	}
	return strayToolResults(groupToolResults(msgs))
}

// groupToolResults moves each tool result right after the assistant message
// that made its call. A background branch writes its tool results after main
// line rows (a cap notice, an owner message), and strict OpenAI-compatible
// gateways reject any message between tool_calls and their results. Rows that
// sat between a call and its results keep their order and follow the results.
// Results whose call is not in msgs stay where they are.
func groupToolResults(msgs []provider.Message) []provider.Message {
	// owner maps a call id to the nearest assistant row before the current
	// position that made it. Scanning forward keeps that true even when an id
	// repeats across turns (a gateway that omits ids gets local ones).
	owner := map[string]int{}
	results := map[int][]provider.Message{}
	moved := make([]bool, len(msgs))
	for i, m := range msgs {
		if m.Role == provider.RoleAssistant {
			for _, tc := range m.ToolCalls {
				if tc.ID != "" {
					owner[tc.ID] = i
				}
			}
			continue
		}
		if m.Role != provider.RoleTool {
			continue
		}
		if at, ok := owner[m.ToolCallID]; ok {
			results[at] = append(results[at], m)
			moved[i] = true
			delete(owner, m.ToolCallID)
		}
	}
	if len(results) == 0 {
		return msgs
	}
	out := make([]provider.Message, 0, len(msgs))
	for i, m := range msgs {
		if moved[i] {
			continue
		}
		out = append(out, m)
		out = append(out, results[i]...)
	}
	return out
}

// strayToolResults fills in a short result for every tool call whose result is
// missing from the projected history, so no assistant message carries an
// unanswered tool_calls. A background branch writes its tool calls into main
// line rows; when the turn the projection starts from has not answered them
// yet, a strict OpenAI-compatible gateway rejects the whole request. The real
// result still lands later, in its own row.
func strayToolResults(msgs []provider.Message) []provider.Message {
	answered := map[string]bool{}
	for _, m := range msgs {
		if m.Role == provider.RoleTool && m.ToolCallID != "" {
			answered[m.ToolCallID] = true
		}
	}
	need := false
	for _, m := range msgs {
		if m.Role != provider.RoleAssistant {
			continue
		}
		for _, tc := range m.ToolCalls {
			if !answered[tc.ID] {
				need = true
				break
			}
		}
		if need {
			break
		}
	}
	if !need {
		return msgs
	}
	out := make([]provider.Message, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, m)
		if m.Role != provider.RoleAssistant {
			continue
		}
		for _, tc := range m.ToolCalls {
			if answered[tc.ID] {
				continue
			}
			out = append(out, provider.Message{
				Role:       provider.RoleTool,
				ToolCallID: tc.ID,
				Content:    "(" + tc.Function.Name + ": no result in this context yet)",
			})
		}
	}
	return out
}

func summaryMessage(b bot.SummaryBlock) provider.Message {
	return provider.Message{
		Role:    provider.RoleUser,
		Content: fmt.Sprintf("<summary from=\"%d\" to=\"%d\">\n%s\n</summary>", b.From, b.To, strings.TrimSpace(b.Text)),
	}
}

func coverage(n int, active []bot.SummaryBlock) []bool {
	covered := make([]bool, n+1)
	for _, b := range active {
		for s := b.From; s <= b.To && s <= n; s++ {
			covered[s] = true
		}
	}
	return covered
}

func migrate(st bot.State) bot.State {
	if len(st.Blocks) == 0 && st.Covered > 0 && strings.TrimSpace(st.Summary) != "" {
		st.Blocks = []bot.SummaryBlock{{From: 1, To: st.Covered, Text: st.Summary}}
	}
	return st
}

// CompactRange summarizes a finished inclusive sequence range. The transcript is not rewritten.
// The last KeepTail messages stay raw. A range may contain whole active blocks and replace them
// with one coarser block. It may not cut through a block or a tool-call pair.
func (k *Kernel) CompactRange(ctx context.Context, from, to int, focus string) (string, error) {
	n, err := k.Bot.Count()
	if err != nil {
		return "", err
	}
	st, err := k.Bot.LoadState()
	if err != nil {
		return "", err
	}
	st = migrate(st)
	var span []bot.Entry
	if from >= 1 && to >= from && to <= n {
		span, err = k.Bot.Slice(from, to)
		if err != nil {
			return "", err
		}
	}
	if err := validateRange(span, from, from, to, k.keepTail(), n, st.Blocks); err != nil {
		return "", err
	}
	focus = strings.TrimSpace(focus)
	if focus == "" {
		focus = defaultFocus
	}
	text, err := k.summarize(ctx, span, st.Blocks, from, to, focus)
	if err != nil {
		return "", err
	}
	k.extractInto(ctx, span, focus)
	st.Blocks = addBlock(st.Blocks, bot.SummaryBlock{
		From: from, To: to, Text: text, Focus: focus, At: k.now(),
	})
	if err := k.refreshMemSnap(ctx, &st); err != nil {
		return "", err
	}
	if err := k.saveState(st); err != nil {
		return "", err
	}
	return fmt.Sprintf("compacted %d-%d", from, to), nil
}

const defaultFocus = "Preserve decisions, names, and unfinished work."

// keepTail is the floor on the raw tail, in messages. It guards the manual
// compact path, where the model picks a range itself. The automatic paths bound
// the tail by size instead, because a few large tool results would otherwise
// pin the context above the soft target and leave nothing to compact.
func (k *Kernel) keepTail() int {
	if k.Budget.KeepTail > 0 {
		return k.Budget.KeepTail
	}
	return compact.DefaultBudget().KeepTail
}

// lines returns the soft target and the hard ceiling (ADR 0015). A value set
// on Budget wins; otherwise both come from the bound model's window, lowered
// by the bot's context_limit, and follow the model when the binding changes.
// The ceiling here is the request's: tool definitions are not taken off.
func (k *Kernel) lines() (target, ceiling int) {
	window := k.contextWindow()
	if k != nil && k.Bot != nil {
		if limit := k.Bot.Config().ContextLimit; limit > 0 && (window <= 0 || limit < window) {
			window = limit
		}
	}
	target, ceiling = compact.Lines(window)
	if k.Budget.Ceiling > 0 {
		ceiling = k.Budget.Ceiling
		target = compact.TargetFor(ceiling)
	}
	if k.Budget.Target > 0 {
		target = k.Budget.Target
	}
	if target > ceiling {
		target = ceiling
	}
	return target, ceiling
}

// target is the soft line: past it the kernel compacts in the background.
func (k *Kernel) target() int {
	t, _ := k.lines()
	return t
}

// ceiling is the hard line for the messages: the request's ceiling less the
// tool definitions last sent, which ride on every request but are not in the
// message estimate. Past it the kernel compacts inside the turn.
func (k *Kernel) ceiling() int {
	_, c := k.lines()
	k.context.mu.Lock()
	tools := k.context.tools
	k.context.mu.Unlock()
	if c-tools < 1 {
		return 1
	}
	return c - tools
}

// tailShare is the raw tail's size bound as a share of the soft target: one
// eighth. Bounding the tail by size rather than by a message count matters
// because a few large tool results can otherwise fill the whole target on
// their own, leaving nothing compactable and pushing every request over budget.
// One eighth keeps the last exchange readable without crowding out the blocks.
const tailShare = 8

// tailBudget is that bound in estimated tokens.
func (k *Kernel) tailBudget() int {
	budget := k.target() / tailShare
	if budget < 0 {
		budget = 0
	}
	return budget
}

// tailPage is how many transcript lines tailStart reads at a time.
const tailPage = 64

// tailStart returns the first sequence number that stays raw. The tail holds at
// least keepTail messages, then grows backwards while its estimated size stays
// under tailBudget.
func (k *Kernel) tailStart(n int) (int, error) {
	if n < 1 {
		return 1, nil
	}
	start := n - k.keepTail() + 1
	if start < 1 {
		start = 1
	}
	seed, err := k.Bot.Slice(start, n)
	if err != nil {
		return 0, err
	}
	used := 0
	for _, e := range seed {
		used += compact.EstimateMessage(e.Message)
	}
	budget := k.tailBudget()
	for start > 1 && used < budget {
		lo := start - tailPage
		if lo < 1 {
			lo = 1
		}
		part, err := k.Bot.Slice(lo, start-1)
		if err != nil {
			return 0, err
		}
		if len(part) == 0 {
			break
		}
		for i := len(part) - 1; i >= 0; i-- {
			used += compact.EstimateMessage(part[i].Message)
			if used > budget {
				return lo + i + 1, nil
			}
			start = lo + i
		}
	}
	return start, nil
}

// compactFrom returns the first sequence number a new summary block should
// cover: one past the contiguous prefix that active blocks already represent.
// Starting there rather than at 1 is what makes a pass incremental. An old
// block is never summarized again, so the bytes the model already saw stay
// unchanged and its prefix cache keeps hitting, and the pass costs the size of
// the new range instead of the size of the whole history.
func compactFrom(blocks []bot.SummaryBlock) int {
	active := ActiveBlocks(blocks)
	through := 0
	for {
		grew := false
		for _, b := range active {
			if b.From <= through+1 && b.To > through {
				through = b.To
				grew = true
			}
		}
		if !grew {
			return through + 1
		}
	}
}

// compactPlan picks the range one automatic pass should cover: the oldest
// uncovered stretch, ending before the raw tail and before the current turn.
// ok is false when nothing is left to cover.
func (k *Kernel) compactPlan(st bot.State, protectSeq int) (from, to int, span []bot.Entry, ok bool, err error) {
	n, err := k.Bot.Count()
	if err != nil {
		return 0, 0, nil, false, err
	}
	from = compactFrom(st.Blocks)
	tail, err := k.tailStart(n)
	if err != nil {
		return 0, 0, nil, false, err
	}
	maxTo := tail - 1
	if protectSeq > 0 && protectSeq < tail {
		maxTo = protectSeq - 1
	}
	if maxTo < from {
		return 0, 0, nil, false, nil
	}
	span, err = k.Bot.Slice(from, maxTo)
	if err != nil {
		return 0, 0, nil, false, err
	}
	to = safeTo(span, from, from, maxTo, n, st.Blocks)
	if to < from || to-from+1 > len(span) {
		return 0, 0, nil, false, nil
	}
	return from, to, span[:to-from+1], true, nil
}

// foldBlocksAt is the share of the soft target the summary blocks may occupy
// before the oldest of them are folded into one.
func (k *Kernel) foldBlocksAt() int {
	at := k.target() / 3
	if at < 1 {
		at = 1
	}
	return at
}

// addBlock appends a block and drops every block it supersedes. Blocks used to
// be rewritten in place, so the list stayed short; now each pass appends, so
// leaving superseded text in place would grow state.json and the read on every
// wake without ever being shown to the model. A block is superseded when it is
// entirely inside the new one. The result stays ordered by range, because the
// exporter walks the stored list in order rather than sorting it.
func addBlock(blocks []bot.SummaryBlock, b bot.SummaryBlock) []bot.SummaryBlock {
	kept := blocks[:0]
	for _, o := range blocks {
		if o.From >= b.From && o.To <= b.To {
			continue
		}
		kept = append(kept, o)
	}
	kept = append(kept, b)
	sort.SliceStable(kept, func(i, j int) bool {
		if kept[i].From == kept[j].From {
			return kept[i].To < kept[j].To
		}
		return kept[i].From < kept[j].From
	})
	return kept
}

// contiguousRun returns the blocks from the oldest one on that join head to
// tail with no gap. A gap holds messages that are still raw, so a range folded
// across it would claim to summarize them.
func contiguousRun(active []bot.SummaryBlock) []bot.SummaryBlock {
	if len(active) == 0 {
		return nil
	}
	run := []bot.SummaryBlock{active[0]}
	through := active[0].To
	for _, b := range active[1:] {
		if b.To <= through {
			continue
		}
		if b.From > through+1 {
			break
		}
		run = append(run, b)
		through = b.To
	}
	return run
}

// foldBlocks summarizes the oldest active blocks into one when the block region
// has grown past its share of the soft target, so summaries cannot take the room
// the raw tail and the next range need. Blocks are the part of the context that
// must be rewritten to shrink it, and rewriting them drops the prefix cache from
// the first block on, so this runs rarely: it folds everything except the newest
// block, which keeps the most recent summary detailed. The transcript is not
// read; the input is the block texts.
func (k *Kernel) foldBlocks(ctx context.Context, st bot.State) (bot.State, error) {
	active := ActiveBlocks(st.Blocks)
	if len(active) < 3 || k.Compactor == nil {
		return st, nil
	}
	// Only the contiguous run that starts at the oldest block can be folded: a
	// block created by a manual compact can leave a gap, and folding across it
	// would mark the raw messages in that gap as summarized, so they would stop
	// reaching the model.
	run := contiguousRun(active)
	if len(run) < 3 {
		return st, nil
	}
	total := 0
	for _, b := range run {
		total += compact.Estimate(b.Text)
	}
	if total <= k.foldBlocksAt() {
		return st, nil
	}
	first, last := run[0].From, run[len(run)-2].To
	if last < first {
		return st, nil
	}
	text, err := k.summarize(ctx, nil, run, first, last, defaultFocus)
	if err != nil {
		return st, err
	}
	st.Blocks = addBlock(st.Blocks, bot.SummaryBlock{
		From: first, To: last, Text: text, Focus: "folded", At: k.now(),
	})
	if err := k.saveState(st); err != nil {
		return st, err
	}
	return st, nil
}

func (k *Kernel) autoCompact(ctx context.Context, st bot.State, protectSeq int, mem string) (bot.State, error) {
	st, err := k.foldBlocks(ctx, st)
	if err != nil {
		return st, err
	}
	_, usage, err := k.assemble(st, mem)
	if err != nil {
		return st, err
	}
	if k.scaled(usage.Total()) <= k.ceiling() {
		st.Usage = usage
		return st, nil
	}
	from, to, span, ok, err := k.compactPlan(st, protectSeq)
	if err != nil {
		return st, err
	}
	if !ok {
		st.Usage = usage
		return st, nil
	}
	text, err := k.summarize(ctx, span, st.Blocks, from, to, defaultFocus)
	if err != nil {
		return st, err
	}
	k.extractInto(ctx, span, defaultFocus)
	st.Blocks = addBlock(st.Blocks, bot.SummaryBlock{
		From: from, To: to, Text: text, Focus: "automatic", At: k.now(),
	})
	if err := k.refreshMemSnap(ctx, &st); err != nil {
		return st, err
	}
	if err := k.saveState(st); err != nil {
		return st, err
	}
	return st, nil
}

// safeTo walks back from maxTo to the first end that validateRange accepts, so a
// range never cuts a tool-call pair or an active block. 0 means none did.
func safeTo(entries []bot.Entry, base, from, maxTo, n int, blocks []bot.SummaryBlock) int {
	for to := maxTo; to >= from; to-- {
		if validateRange(entries, base, from, to, 0, n, blocks) == nil {
			return to
		}
	}
	return 0
}

func coveredBy(blocks []bot.SummaryBlock, from, to int) bool {
	for _, b := range ActiveBlocks(blocks) {
		if b.From <= from && b.To >= to {
			return true
		}
	}
	return false
}

// validateRange checks an inclusive range. entries is the slice whose first line is sequence base.
// n is the transcript length, which may be longer than the slice.
func validateRange(entries []bot.Entry, base, from, to, keep, n int, blocks []bot.SummaryBlock) error {
	if from < 1 || to < from || to > n {
		return fmt.Errorf("compact: range %d-%d outside 1-%d", from, to, n)
	}
	if keep > 0 && to > n-keep {
		return fmt.Errorf("compact: last %d messages stay raw", keep)
	}
	if err := toolBoundary(entries, base, from, to); err != nil {
		return err
	}
	return blockOverlap(from, to, blocks)
}

func blockOverlap(from, to int, blocks []bot.SummaryBlock) error {
	for _, b := range ActiveBlocks(blocks) {
		if to < b.From || from > b.To {
			continue
		}
		if from <= b.From && to >= b.To && (from < b.From || to > b.To) {
			continue
		}
		return fmt.Errorf("compact: %d-%d overlaps active summary %d-%d", from, to, b.From, b.To)
	}
	return nil
}

func toolBoundary(entries []bot.Entry, base, from, to int) error {
	if base < 1 {
		base = 1
	}
	callAt := map[string]int{}
	var results []struct {
		id  string
		seq int
	}
	for i, e := range entries {
		seq := base + i
		m := e.Message
		if m.Role == provider.RoleAssistant {
			for _, tc := range m.ToolCalls {
				if tc.ID == "" {
					continue
				}
				if _, ok := callAt[tc.ID]; !ok {
					callAt[tc.ID] = seq
				}
			}
		}
		if m.Role == provider.RoleTool && m.ToolCallID != "" {
			results = append(results, struct {
				id  string
				seq int
			}{m.ToolCallID, seq})
		}
	}
	in := func(s int) bool { return s >= from && s <= to }
	seenResult := map[string]bool{}
	for _, r := range results {
		seenResult[r.id] = true
		cseq, ok := callAt[r.id]
		if in(r.seq) && (!ok || !in(cseq)) {
			return fmt.Errorf("compact: tool result %s crosses %d-%d", r.id, from, to)
		}
		if ok && in(cseq) && !in(r.seq) {
			return fmt.Errorf("compact: tool call %s crosses %d-%d", r.id, from, to)
		}
	}
	for id, cseq := range callAt {
		if in(cseq) && !seenResult[id] {
			return fmt.Errorf("compact: tool call %s in %d-%d has no result", id, from, to)
		}
	}
	return nil
}

const summaryFacts = "Preserve each task's identity, conclusion, key numbers, source sequence and result-file path; keep unfinished work, pending owner requests and decisions, actions already executed, and what the bot already told the owner. Preserve the latest environment values and identify whose person facts they are. Never replace results with only an already-reported label or confuse different tasks or people."

func (k *Kernel) summarize(ctx context.Context, entries []bot.Entry, blocks []bot.SummaryBlock, from, to int, focus string) (string, error) {
	if k.Compactor == nil {
		return "", errors.New("compact: context exceeded and no compactor is configured")
	}
	focus = strings.TrimSpace(focus + "\n" + summaryFacts)
	var prior strings.Builder
	for _, b := range blocks {
		if b.From >= from && b.To <= to && !(b.From == from && b.To == to) {
			if prior.Len() > 0 {
				prior.WriteString("\n\n")
			}
			prior.WriteString(b.Text)
		}
	}
	turns := make([]provider.Message, 0, len(entries))
	for _, e := range entries {
		if code.IsCard(e.Message.Content) || bot.IsApprovalCard(e.Message.Content) {
			continue
		}
		if e.IsForkMark() {
			continue
		}
		msg := e.Message
		// A summary of an old range must say which pictures those lines carried;
		// otherwise the range is summarized into words about a picture nobody can
		// find any more.

		msg.Content = k.withPastedNote(msg.Content, e.Images)
		turns = append(turns, msg)
	}
	if f, ok := k.compactor(turnSeqOf(ctx)).(focusedCompactor); ok {
		return f.CompactFocus(ctx, prior.String(), focus, turns)
	}
	p := prior.String()
	if strings.TrimSpace(focus) != "" {
		if p != "" {
			p += "\n\n"
		}
		p += "Focus: " + focus
	}
	return k.compactor(turnSeqOf(ctx)).Compact(ctx, p, turns)
}

type focusedCompactor interface {
	CompactFocus(ctx context.Context, prior, focus string, turns []provider.Message) (string, error)
}

func uncoveredHasEnv(entries []bot.Entry) bool {
	for _, e := range entries {
		if strings.HasPrefix(e.Message.Content, "<environment_") {
			return true
		}
	}
	return false
}
