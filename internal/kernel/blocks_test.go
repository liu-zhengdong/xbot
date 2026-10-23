package kernel

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/compact"
	"xbot/internal/link"
	"xbot/internal/provider"
)

func callMsg(id, name string) provider.Message {
	return provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
		ID: id, Type: "function", Function: provider.FunctionCall{Name: name, Arguments: "{}"},
	}}}
}

func resultMsg(id string) provider.Message {
	return provider.Message{Role: provider.RoleTool, ToolCallID: id, Content: "ok"}
}

// TestProjectFillsMissingToolResults is the regression for a foreground turn
// whose transcript starts after a background branch's tool call: the projection
// holds the call but the branch's result row is not in the window, and a strict
// OpenAI-compatible gateway rejects the request. Every call must be answered.
func TestProjectFillsMissingToolResults(t *testing.T) {
	entries := []bot.Entry{
		{Message: callMsg("c1", "shell")},
		{Message: provider.Message{Role: provider.RoleAssistant, Content: "继续做"}},
	}
	msgs := project(entries, nil)
	if len(msgs) != 3 {
		t.Fatalf("messages %d, want 3 (call, placeholder result, answer): %+v", len(msgs), msgs)
	}
	if msgs[1].Role != provider.RoleTool || msgs[1].ToolCallID != "c1" {
		t.Fatalf("the call was left unanswered: %+v", msgs)
	}
	if msgs[1].Content == "" {
		t.Fatal("the placeholder result is empty")
	}
}

// TestProjectKeepsRealToolResults checks the placeholder is not added when the
// result is there, and that a result still lands right after its call.
func TestProjectKeepsRealToolResults(t *testing.T) {
	entries := []bot.Entry{
		{Message: callMsg("c1", "shell")},
		{Message: provider.Message{Role: provider.RoleAssistant, Content: "中间一行"}},
		{Message: resultMsg("c1")},
	}
	msgs := project(entries, nil)
	var tools int
	for _, m := range msgs {
		if m.Role == provider.RoleTool {
			tools++
		}
	}
	if tools != 1 {
		t.Fatalf("tool messages %d, want only the real result: %+v", tools, msgs)
	}
	if msgs[1].Role != provider.RoleTool || msgs[1].ToolCallID != "c1" {
		t.Fatalf("the real result did not follow its call: %+v", msgs)
	}
}

// TestStrayToolResultsCountsEachCallOnce checks a second call in the same
// message is filled in too, and that the projection is left alone otherwise.
func TestStrayToolResultsCountsEachCallOnce(t *testing.T) {
	two := provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
		{ID: "c1", Function: provider.FunctionCall{Name: "shell"}},
		{ID: "c2", Function: provider.FunctionCall{Name: "read_file"}},
	}}
	got := strayToolResults([]provider.Message{two, resultMsg("c1")})
	if len(got) != 3 {
		t.Fatalf("messages %d, want 3: %+v", len(got), got)
	}
	var filled, real int
	for _, m := range got {
		if m.Role != provider.RoleTool {
			continue
		}
		switch m.ToolCallID {
		case "c2":
			filled++
		case "c1":
			real++
		}
	}
	if filled != 1 {
		t.Fatalf("the second call was not filled exactly once: %+v", got)
	}
	if real != 1 {
		t.Fatalf("the real result was disturbed: %+v", got)
	}

	// Nothing is added when every call is answered.
	plain := []provider.Message{{Role: provider.RoleAssistant, Content: "好了"}}
	if got := strayToolResults(plain); len(got) != 1 {
		t.Fatalf("messages %d, want 1: %+v", len(got), got)
	}
}

// TestCompactFromSkipsCoveredPrefix pins the start of a new summary block. It is
// one past the contiguous covered prefix, not 1, so a pass never re-summarizes a
// block the model has already seen. Starting at 1 rewrote the whole prefix on
// every pass: the cost grew with the transcript and the rewritten bytes dropped
// the provider's prefix cache from the first block on.
func TestCompactFromSkipsCoveredPrefix(t *testing.T) {
	cases := []struct {
		name   string
		blocks []bot.SummaryBlock
		want   int
	}{
		{name: "empty", want: 1},
		{
			name:   "one block",
			blocks: []bot.SummaryBlock{{From: 1, To: 10, Text: "a"}},
			want:   11,
		},
		{
			name: "two contiguous blocks",
			blocks: []bot.SummaryBlock{
				{From: 1, To: 10, Text: "a"},
				{From: 11, To: 20, Text: "b"},
			},
			want: 21,
		},
		{
			name: "gap before the tail",
			blocks: []bot.SummaryBlock{
				{From: 1, To: 10, Text: "a"},
				{From: 25, To: 30, Text: "c"},
			},
			want: 11,
		},
		{
			name: "unsorted input",
			blocks: []bot.SummaryBlock{
				{From: 11, To: 20, Text: "b"},
				{From: 1, To: 10, Text: "a"},
			},
			want: 21,
		},
		{
			name: "empty text does not cover",
			blocks: []bot.SummaryBlock{
				{From: 1, To: 10, Text: "a"},
				{From: 11, To: 20, Text: "   "},
			},
			want: 11,
		},
		{
			name:   "a fold supersedes the blocks it covers",
			blocks: []bot.SummaryBlock{{From: 1, To: 30, Text: "a"}, {From: 31, To: 40, Text: "b"}},
			want:   41,
		},
	}
	for _, c := range cases {
		if got := compactFrom(c.blocks); got != c.want {
			t.Fatalf("%s: compactFrom = %d, want %d", c.name, got, c.want)
		}
	}
}

// TestAutoCompactIsIncremental is the regression for a pass that re-read the
// whole history. Each pass must summarize only the stretch no block covers yet,
// and an earlier block's bytes must never change.
func TestAutoCompactIsIncremental(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	seedMarked(t, b, 6, 4000)
	comp := &recCompactor{}
	k := &Kernel{Bot: b, Compactor: comp, Budget: compact.Budget{Ceiling: 1, KeepTail: 1}}
	if _, err := k.autoCompact(context.Background(), bot.State{}, 0, ""); err != nil {
		t.Fatal(err)
	}
	if comp.calls != 1 {
		t.Fatalf("calls %d", comp.calls)
	}
	st, err := b.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	active := ActiveBlocks(st.Blocks)
	if len(active) != 1 || active[0].From != 1 {
		t.Fatalf("first block %+v", st.Blocks)
	}
	covered, firstText := active[0].To, active[0].Text
	// The newest message stays raw, so the first block stops one short of the end.
	if covered != 5 {
		t.Fatalf("first block covered %d, want 5 with one raw message", covered)
	}
	coveredEntries, err := b.Slice(1, covered)
	if err != nil {
		t.Fatal(err)
	}

	// One more message arrives. The next pass covers what the first left out,
	// and the new message becomes the raw tail.
	if err := b.Append(bot.Entry{Message: provider.Message{Role: provider.RoleUser, Content: marker(7)}}); err != nil {
		t.Fatal(err)
	}
	st, err = b.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := k.autoCompact(context.Background(), st, 0, ""); err != nil {
		t.Fatal(err)
	}
	if comp.calls != 2 {
		t.Fatalf("second pass did not run: calls %d", comp.calls)
	}
	var second []provider.Message
	for _, m := range comp.got {
		if !strings.HasPrefix(m.Content, environmentOpen) {
			second = append(second, m)
		}
	}
	if len(second) != 1 {
		t.Fatalf("second pass saw %d messages, want only the one the first block left out", len(second))
	}
	for _, e := range coveredEntries {
		if second[0].Content == e.Message.Content {
			t.Fatalf("second pass re-read a message block 1-%d already covers", covered)
		}
	}
	if head := head(second[0].Content); head != "msg-006" {
		t.Fatalf("second pass covered %q, want msg-006", head)
	}
	st, err = b.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	active = ActiveBlocks(st.Blocks)
	if len(active) != 2 {
		t.Fatalf("want two blocks, got %+v", active)
	}
	if active[0].From != 1 || active[0].To != covered || active[0].Text != firstText {
		t.Fatalf("the first block was rewritten: %+v", active[0])
	}
	if active[1].From != covered+1 || active[1].To != 7 {
		t.Fatalf("the second block does not continue where the first ends: %+v", active[1])
	}
}

// head is the leading marker of a test message, for readable failures.
func head(content string) string {
	if len(content) > 7 {
		return content[:7]
	}
	return content
}

// marker is a message body unique to one sequence number.
func marker(seq int) string {
	return fmt.Sprintf("msg-%03d-%s", seq, strings.Repeat("x", 4000))
}

// seedMarked appends n messages of the given size, each with its own marker.
func seedMarked(t *testing.T, b *bot.Bot, n, size int) {
	t.Helper()
	for i := 1; i <= n; i++ {
		body := fmt.Sprintf("msg-%03d-%s", i, strings.Repeat("x", size))
		if err := b.Append(bot.Entry{Message: provider.Message{Role: provider.RoleUser, Content: body}}); err != nil {
			t.Fatal(err)
		}
	}
}

// TestFoldBlocksKeepsNewestDetailed checks the fold: once the block region passes
// its share of the soft target, everything but the newest block becomes one
// coarser block, and the newest keeps its own range.
func TestFoldBlocksKeepsNewestDetailed(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	st := bot.State{Blocks: []bot.SummaryBlock{
		{From: 1, To: 10, Text: strings.Repeat("a", 800)},
		{From: 11, To: 20, Text: strings.Repeat("b", 800)},
		{From: 21, To: 30, Text: strings.Repeat("c", 800)},
	}}
	if err := b.SaveState(st); err != nil {
		t.Fatal(err)
	}
	comp := &recCompactor{}
	k := &Kernel{Bot: b, Compactor: comp, Budget: compact.Budget{Ceiling: 100, KeepTail: 1}}
	st, err := b.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := k.foldBlocks(context.Background(), st); err != nil {
		t.Fatal(err)
	}
	st, err = b.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	active := ActiveBlocks(st.Blocks)
	if len(active) != 2 {
		t.Fatalf("want the fold and the newest block, got %+v", active)
	}
	if active[0].From != 1 || active[0].To != 20 {
		t.Fatalf("fold range %+v", active[0])
	}
	if active[1].From != 21 || active[1].To != 30 {
		t.Fatalf("the newest block must keep its range: %+v", active[1])
	}
	if comp.calls != 1 {
		t.Fatalf("calls %d", comp.calls)
	}
}

// TestFoldBlocksStopsAtAGap checks the fold stops at a gap between blocks. A
// block from a manual compact can leave raw messages uncovered in the middle;
// folding across them would mark them summarized, so they would silently stop
// reaching the model.
func TestFoldBlocksStopsAtAGap(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	st := bot.State{Blocks: []bot.SummaryBlock{
		{From: 1, To: 10, Text: strings.Repeat("a", 800)},
		// 11-40 stay raw: a manual compact covered 41-50 but not this stretch.
		{From: 41, To: 50, Text: strings.Repeat("b", 800)},
		{From: 51, To: 60, Text: strings.Repeat("c", 800)},
	}}
	if err := b.SaveState(st); err != nil {
		t.Fatal(err)
	}
	comp := &recCompactor{}
	k := &Kernel{Bot: b, Compactor: comp, Budget: compact.Budget{Ceiling: 100, KeepTail: 1}}
	st, err := b.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := k.foldBlocks(context.Background(), st); err != nil {
		t.Fatal(err)
	}
	if comp.calls != 0 {
		t.Fatalf("folded across a gap: calls %d", comp.calls)
	}
	st, err = b.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	for _, blk := range ActiveBlocks(st.Blocks) {
		if blk.From <= 40 && blk.To >= 41 {
			t.Fatalf("folded across the gap 11-40: %+v", blk)
		}
	}
}

// TestFoldBlocksWaitsForEnoughBlocks checks the fold does not run on a short
// block list, so an early session is never folded into one block.
func TestFoldBlocksWaitsForEnoughBlocks(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	st := bot.State{Blocks: []bot.SummaryBlock{
		{From: 1, To: 10, Text: strings.Repeat("a", 800)},
		{From: 11, To: 20, Text: strings.Repeat("b", 800)},
	}}
	if err := b.SaveState(st); err != nil {
		t.Fatal(err)
	}
	comp := &recCompactor{}
	k := &Kernel{Bot: b, Compactor: comp, Budget: compact.Budget{Ceiling: 100, KeepTail: 1}}
	st, err := b.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := k.foldBlocks(context.Background(), st); err != nil {
		t.Fatal(err)
	}
	if comp.calls != 0 {
		t.Fatalf("folded two blocks: calls %d", comp.calls)
	}
}

// TestTailStartBoundsBySize checks the raw tail is bounded by size, not only by
// a message count. A long run of large tool results would otherwise pin the
// whole context above the soft target and leave nothing compactable.
func TestTailStartBoundsBySize(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	seedMarked(t, b, 40, 4000)
	k := &Kernel{Bot: b, Budget: compact.Budget{Target: 100_000, Ceiling: 160_000, KeepTail: 1}}
	start, err := k.tailStart(40)
	if err != nil {
		t.Fatal(err)
	}
	// 40 messages of 4000 bytes are about 40000 estimated tokens, well over the
	// 12500-token tail budget, so the tail must stop short of the first message.
	if start <= 1 {
		t.Fatalf("tail start %d reached back over the whole transcript", start)
	}
	entries, err := b.Slice(start, 40)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) < 1 {
		t.Fatalf("empty tail from %d", start)
	}
	// The scan stops on the message that crosses the budget, so the tail is at
	// most one message over it.
	if size := compact.EstimateMessages(entryMessages(entries)); size > k.tailBudget()+1500 {
		t.Fatalf("tail of %d tokens is over the budget %d", size, k.tailBudget())
	}

	// A short transcript stays whole even though it exceeds no budget.
	small := testBot(t, link.NameTerminal)
	seedMarked(t, small, 3, 100)
	k2 := &Kernel{Bot: small, Budget: compact.Budget{Target: 100_000, Ceiling: 160_000, KeepTail: 1}}
	start2, err := k2.tailStart(3)
	if err != nil {
		t.Fatal(err)
	}
	if start2 != 1 {
		t.Fatalf("a short transcript must stay whole: %d", start2)
	}

	// The message floor still applies: the newest keepTail messages are always raw.
	k3 := &Kernel{Bot: small, Budget: compact.Budget{Ceiling: 1, KeepTail: 3}}
	start3, err := k3.tailStart(3)
	if err != nil {
		t.Fatal(err)
	}
	if start3 != 1 {
		t.Fatalf("keepTail 3 of 3 must keep all: %d", start3)
	}
}

// TestAddBlockDropsSuperseded checks a covered block leaves state.json instead
// of piling up. Each pass appends now, so keeping superseded text would grow the
// state file and the read on every wake with text the model never sees.
func TestAddBlockDropsSuperseded(t *testing.T) {
	blocks := []bot.SummaryBlock{
		{From: 1, To: 10, Text: "old"},
		{From: 11, To: 20, Text: "middle"},
		{From: 21, To: 30, Text: "newest"},
	}
	// A fold over the first two replaces both.
	got := addBlock(blocks, bot.SummaryBlock{From: 1, To: 20, Text: "folded"})
	if len(got) != 2 {
		t.Fatalf("want the fold and the newest block, got %+v", got)
	}
	if got[0].From != 1 || got[0].To != 20 || got[0].Text != "folded" {
		t.Fatalf("the fold must come first: %+v", got[0])
	}
	if got[1].From != 21 || got[1].To != 30 {
		t.Fatalf("an uncovered block must stay: %+v", got[1])
	}
	// A wider block drops a narrower one even when it arrives first.
	got = addBlock([]bot.SummaryBlock{{From: 5, To: 8, Text: "inner"}},
		bot.SummaryBlock{From: 1, To: 10, Text: "outer"})
	if len(got) != 1 || got[0].Text != "outer" {
		t.Fatalf("covered block kept: %+v", got)
	}
	// A partial overlap keeps both: neither covers the other.
	got = addBlock([]bot.SummaryBlock{{From: 1, To: 20, Text: "left"}},
		bot.SummaryBlock{From: 11, To: 30, Text: "right"})
	if len(got) != 2 {
		t.Fatalf("partial overlap dropped: %+v", got)
	}
}

func entryMessages(entries []bot.Entry) []provider.Message {
	out := make([]provider.Message, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Message)
	}
	return out
}

// TestProjectedKeepsSequenceOrderAcrossBlocks is the regression for a gap: a
// manual compact left raw lines between two blocks, and the projection put
// every summary first, so the model read 1-10, 20-30, then the raw 11.
// A gap happens when a manual compact summarizes a middle range; automatic
// compaction stops at the gap and never creates one.
func TestProjectedKeepsSequenceOrderAcrossBlocks(t *testing.T) {
	blocks := []bot.SummaryBlock{
		{From: 1, To: 10, Text: "older"},
		{From: 20, To: 30, Text: "newer"},
	}
	uncovered := []bot.Entry{
		{Message: provider.Message{Role: provider.RoleUser, Content: "raw-11"}},
		{Message: provider.Message{Role: provider.RoleUser, Content: "raw-12"}},
		{Message: provider.Message{Role: provider.RoleUser, Content: "raw-19"}},
		{Message: provider.Message{Role: provider.RoleUser, Content: "raw-31"}},
	}
	seqs := []int{11, 12, 19, 31}
	msgs := projected(blocks, uncovered, seqs)
	want := []string{"<summary", "<summary", "raw-11", "raw-12", "raw-19", "raw-31"}
	if len(msgs) != len(want) {
		t.Fatalf("messages %d, want %d: %+v", len(msgs), len(want), msgs)
	}
	// The gap lines belong between the two summaries, not after both.
	if !strings.HasPrefix(msgs[0].Content, "<summary") {
		t.Fatalf("first message is not the 1-10 summary: %q", head(msgs[0].Content))
	}
	if !strings.HasPrefix(msgs[1].Content, "raw-11") {
		t.Fatalf("second message is %q, want raw-11 (the gap must stay in order)", head(msgs[1].Content))
	}
	if !strings.HasPrefix(msgs[4].Content, "<summary") {
		t.Fatalf("fifth message is %q, want the 20-30 summary", head(msgs[4].Content))
	}
	if !strings.HasPrefix(msgs[5].Content, "raw-31") {
		t.Fatalf("last message is %q, want raw-31", head(msgs[5].Content))
	}
}
