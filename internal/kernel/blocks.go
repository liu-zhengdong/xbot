package kernel

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"xbot/internal/bot"
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
	var sums []provider.Message
	for _, b := range active {
		sums = append(sums, summaryMessage(b))
	}
	var raw []provider.Message
	for i, e := range entries {
		if covered[i+1] {
			continue
		}
		raw = append(raw, e.Message)
	}
	return append(sums, raw...)
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
	entries, err := k.Bot.Entries()
	if err != nil {
		return "", err
	}
	st, err := k.Bot.LoadState()
	if err != nil {
		return "", err
	}
	st = migrate(st)
	if err := validateRange(entries, from, to, k.keepTail(), st.Blocks); err != nil {
		return "", err
	}
	focus = strings.TrimSpace(focus)
	if focus == "" {
		focus = defaultFocus
	}
	text, err := k.summarize(ctx, entries, st.Blocks, from, to, focus)
	if err != nil {
		return "", err
	}
	k.extractInto(ctx, entries, from, to, focus)
	st.Blocks = append(st.Blocks, bot.SummaryBlock{
		From: from, To: to, Text: text, Focus: focus, At: k.now(),
	})
	if err := k.Bot.SaveState(st); err != nil {
		return "", err
	}
	return fmt.Sprintf("compacted %d-%d", from, to), nil
}

const defaultFocus = "Preserve decisions, names, and unfinished work."

func (k *Kernel) keepTail() int {
	if k.Budget.KeepTail > 0 {
		return k.Budget.KeepTail
	}
	return compact.DefaultBudget().KeepTail
}

func (k *Kernel) compactAt() int {
	if k.Budget.CompactAt > 0 {
		return k.Budget.CompactAt
	}
	return compact.DefaultBudget().CompactAt
}

func (k *Kernel) autoCompact(ctx context.Context, entries []bot.Entry, st bot.State, protectSeq int, mem string) ([]bot.Entry, bot.State, error) {
	_, usage, err := k.assemble(entries, st, mem)
	if err != nil {
		return entries, st, err
	}
	if usage.Total() <= k.compactAt() {
		st.Usage = usage
		return entries, st, nil
	}
	maxTo := len(entries) - k.keepTail()
	if protectSeq > 0 && protectSeq-1 < maxTo {
		maxTo = protectSeq - 1
	}
	to := safeTo(entries, st.Blocks, maxTo)
	if to < 1 || coveredBy(st.Blocks, 1, to) {
		st.Usage = usage
		return entries, st, nil
	}
	text, err := k.summarize(ctx, entries, st.Blocks, 1, to, defaultFocus)
	if err != nil {
		return entries, st, err
	}
	k.extractInto(ctx, entries, 1, to, defaultFocus)
	st.Blocks = append(st.Blocks, bot.SummaryBlock{
		From: 1, To: to, Text: text, Focus: "automatic", At: k.now(),
	})
	if err := k.Bot.SaveState(st); err != nil {
		return entries, st, err
	}
	return entries, st, nil
}

func safeTo(entries []bot.Entry, blocks []bot.SummaryBlock, maxTo int) int {
	for to := maxTo; to >= 1; to-- {
		if validateRange(entries, 1, to, 0, blocks) == nil {
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

func validateRange(entries []bot.Entry, from, to, keep int, blocks []bot.SummaryBlock) error {
	n := len(entries)
	if from < 1 || to < from || to > n {
		return fmt.Errorf("compact: range %d-%d outside 1-%d", from, to, n)
	}
	if keep > 0 && to > n-keep {
		return fmt.Errorf("compact: last %d messages stay raw", keep)
	}
	if err := toolBoundary(entries, from, to); err != nil {
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

func toolBoundary(entries []bot.Entry, from, to int) error {
	callAt := map[string]int{}
	var results []struct {
		id  string
		seq int
	}
	for i, e := range entries {
		seq := i + 1
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

func (k *Kernel) summarize(ctx context.Context, entries []bot.Entry, blocks []bot.SummaryBlock, from, to int, focus string) (string, error) {
	if k.Compactor == nil {
		return "", errors.New("compact: context exceeded and no compactor is configured")
	}
	var prior strings.Builder
	for _, b := range blocks {
		if b.From >= from && b.To <= to && !(b.From == from && b.To == to) {
			if prior.Len() > 0 {
				prior.WriteString("\n\n")
			}
			prior.WriteString(b.Text)
		}
	}
	turns := make([]provider.Message, 0, to-from+1)
	for i := from - 1; i < to && i < len(entries); i++ {
		turns = append(turns, entries[i].Message)
	}
	if f, ok := k.Compactor.(focusedCompactor); ok {
		return f.CompactFocus(ctx, prior.String(), focus, turns)
	}
	p := prior.String()
	if strings.TrimSpace(focus) != "" {
		if p != "" {
			p += "\n\n"
		}
		p += "Focus: " + focus
	}
	return k.Compactor.Compact(ctx, p, turns)
}

type focusedCompactor interface {
	CompactFocus(ctx context.Context, prior, focus string, turns []provider.Message) (string, error)
}

func uncoveredHasEnv(entries []bot.Entry, blocks []bot.SummaryBlock) bool {
	covered := coverage(len(entries), ActiveBlocks(blocks))
	for i, e := range entries {
		if covered[i+1] {
			continue
		}
		if strings.HasPrefix(e.Message.Content, "<environment_") {
			return true
		}
	}
	return false
}
