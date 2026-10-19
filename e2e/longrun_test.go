package e2e

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/compact"
	"xbot/internal/inbox"
	"xbot/internal/kernel"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
)

// TestLongRunJourney is the smoke test for the "long run" journey: one bot is
// talked to for a few hundred turns, with tool work mixed in, so the context
// reaches the working set several times and summary blocks pile up.
//
// The model is a scripted provider; no real model is called. The bot, the
// kernel, the transcript store, the tools, the inbox and the compaction path are
// the real ones. The five acceptance points are observed from outside the
// kernel, through what the provider was sent:
//
//  1. Turn 1's key fact survives. The scripted summarizer copies every KEEP:
//     marker it is handed, so the fact lives on exactly as long as a block that
//     carries it stays in the request. At the end the scripted model states the
//     fact from the markers it can see in that request, and the reply must name
//     it.
//  2. Context stays inside the working set and does not grow with turns,
//     measured as the prompt estimate of every chat request, the same unit the
//     working set is declared in, cycle by cycle.
//  3. Prefix cache across a pass: reported per pass below. The cached count is
//     modelled by the scripted provider as the longest common prefix with the
//     previous chat request, which is what a prefix cache hits; see
//     journeyModel.Chat and the note at the measurement.
//  4. Compaction cost: provider calls are classified (a call whose system
//     message is the summarizer instruction is a compaction), and their prompt
//     plus completion tokens are compared with every call's tokens in the run.
//     The base is the run's whole model consumption, not the bytes the
//     conversation added, because every turn resends the whole context.
//  5. expand reads covered content back: the model calls the kernel's own expand
//     tool for the sequence turn 1 was said at, and the result is compared with
//     the transcript.
//
// Turn count. The run continues until three compaction passes have happened,
// and never stops before 200 turns. At the shipped working set (100k) and a turn
// of about 500 estimated tokens (a few paragraphs plus the occasional tool
// pair), one cycle needs about 150 turns, so three passes is about 480 turns.
// That covers the risks the 200-turn journey names — turn 1 summarized away,
// blocks accumulating, the fact surviving a chain of blocks, later passes not
// re-reading earlier ones — with more passes rather than fewer. The loop is
// capped so the test always ends.
func TestLongRunJourney(t *testing.T) {
	started := time.Now()
	root := t.TempDir()
	work := t.TempDir()
	b, err := bot.Create(root, "longrun", "You are a long-run test bot.", []string{
		link.NameTerminal, link.NameTools, link.NameContext, link.NameMemory,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	model := newJourneyModel()
	k := &kernel.Kernel{
		Bot:       b,
		Provider:  model,
		Memory:    memory.Open(root),
		MaxSteps:  4,
		Inbox:     box,
		Links:     []link.Link{link.Terminal{}, link.NewToolsLinkAt(work, ""), link.Context{}, link.Memory{}},
		Compactor: compact.LLM{P: model},
		Approve:   func(context.Context, string, string) (bool, error) { return true, nil },
		Budget:    kernel.BudgetFor(b.Config()),
	}
	// The journey is about the shipped lines, derived from the model, not a
	// scaled-down budget.
	if k.Budget != compact.DefaultBudget() {
		t.Fatalf("budget %+v, want the default (lines derived from the model)", k.Budget)
	}
	lines := k.ContextSnapshot()
	target, ceiling := lines.Target, lines.Ceiling
	if target <= 0 || ceiling <= target {
		t.Fatalf("lines target %d ceiling %d", target, ceiling)
	}

	turns := 0
	var turnBlocks [][]bot.SummaryBlock
	// The loop runs until the journey's minimum and enough passes, then a few
	// more turns so the last pass has a request on each side of it.
	for i := 1; i <= journeyMaxTurns; i++ {
		model.setTurn(i)
		if err := k.Handle(context.Background(), link.TerminalEvent("u1", journeyTurnText(i), time.Now())); err != nil {
			t.Fatalf("turn %d: %v", i, err)
		}
		st, err := b.LoadState()
		if err != nil {
			t.Fatal(err)
		}
		turnBlocks = append(turnBlocks, kernel.ActiveBlocks(st.Blocks))
		turns = i
		if i >= journeyMinTurns && countPasses(turnBlocks) >= journeyPasses {
			for j := 1; j <= journeyAfterPass; j++ {
				model.setTurn(i + j)
				if err := k.Handle(context.Background(), link.TerminalEvent("u1", journeyTurnText(i+j), time.Now())); err != nil {
					t.Fatalf("turn %d: %v", i+j, err)
				}
				st, err := b.LoadState()
				if err != nil {
					t.Fatal(err)
				}
				turnBlocks = append(turnBlocks, kernel.ActiveBlocks(st.Blocks))
				turns = i + j
			}
			break
		}
	}
	passes := countPasses(turnBlocks)
	blocks := turnBlocks[len(turnBlocks)-1]

	chat := model.chatCalls()

	// --- 2. the context stays near the soft target, never at the ceiling --
	// Measured before the "did the journey happen at all" guard, so a run whose
	// context never gets hold of is reported as criterion 2 failing rather than
	// as a run that never reached a compaction.
	cycles := splitCycles(chat)
	peak, over := 0, 0
	var cyclePeaks []int
	for _, cyc := range cycles {
		p := 0
		for _, c := range cyc {
			if c.prompt > p {
				p = c.prompt
			}
			if c.prompt > peak {
				peak = c.prompt
			}
			if d := c.prompt - target; d > over {
				over = d
			}
		}
		cyclePeaks = append(cyclePeaks, p)
	}
	t.Logf("turns=%d passes=%d walls=%s chat calls=%d prompt peak=%d (target %d, over by %d; ceiling %d) peaks per cycle=%v blocks=%s",
		turns, passes, time.Since(started).Round(time.Millisecond), len(chat),
		peak, target, over, ceiling, cyclePeaks, blockRanges(blocks))
	if peak > ceiling {
		t.Errorf("criterion 2: a prompt peaked at %d, past the %d ceiling", peak, ceiling)
	}
	if over > journeyPromptSlack {
		t.Errorf("criterion 2: a prompt peaked at %d, %d over the %d soft target; the allowance is %d (the turns a background pass takes)",
			peak, over, target, journeyPromptSlack)
	}
	for i := 1; i < len(cyclePeaks); i++ {
		if cyclePeaks[i] > cyclePeaks[0]+journeyPromptSlack {
			t.Errorf("criterion 2: cycle %d peaked at %d, above the first cycle's %d; the context grows with turns",
				i+1, cyclePeaks[i], cyclePeaks[0])
			break
		}
	}

	// A run that reached the turn cap without two passes has no chain to check,
	// so the passes below are skipped, not reported as passing.
	if passes < 2 {
		t.Fatalf("the journey was not exercised: only %d compaction pass(es) in %d turns (wall %s)",
			passes, turns, time.Since(started).Round(time.Millisecond))
	}
	checkBlocks(t, turnBlocks)

	// Each pass reads only the stretch no block covered yet. The contrast is the
	// point of the change: re-summarizing from turn 1 would make pass 3 read the
	// whole history three times over, so the sizes below would climb 55k, 115k,
	// 175k instead of staying near one cycle.
	for i, c := range model.allCalls() {
		if !c.compact {
			continue
		}
		t.Logf("compact call %d (turn %d) read %d tokens as its input; the chat request of that turn was about %d tokens",
			i, c.turn, c.prompt, cyclePromptAt(chat, c.turn))
	}

	// --- 4. compaction cost ----------------------------------------------
	//
	// mockopenai's streams carry no usage object, so no call reports its own
	// token count: the provider client marks every call estimated. The numbers
	// below are therefore prompt plus completion estimated by the same estimator
	// that sets the working set (compact.EstimateMessage, about 4 bytes a token),
	// summed over every call the scripted provider saw. Only the part the
	// provider cannot supply is missing; the ratio is computed on one estimator
	// for numerator and denominator, so it is comparable, but it is an estimate
	// and not a provider-reported count.
	//
	// The denominator is the run's whole model consumption, not the bytes the
	// conversation added: every turn resends the whole context, which is what
	// makes a long run expensive and a cheap pass worth having.
	cost := measureCompactCost(model.allCalls())
	t.Logf("criterion 4: compaction spent %d tokens of %d in the run (%d summarizer calls of %d calls), %.3f%%; limit %.1f%%",
		cost.compact, cost.total, cost.compactCalls, cost.calls, cost.ratio()*100, journeyCompactShareLimit*100)
	if cost.ratio() > journeyCompactShareLimit {
		t.Errorf("criterion 4: compaction spent %.3f%% of the run's model tokens, over the %.1f%% limit",
			cost.ratio()*100, journeyCompactShareLimit*100)
	}

	// --- 1 + 5. the fact survives, and expand reads the original back ----
	seq := factSeq(t, b)
	covering := blockCovering(blocks, seq)
	if covering == nil {
		t.Fatalf("the fact at seq %d is not covered by an active block %s", seq, blockRanges(blocks))
	}
	// A short range inside the covered block: the expand output is capped at
	// 8 KiB and each turn here is about 1.5 KiB.
	from, to := covering.From, covering.From+2
	if to > covering.To {
		to = covering.To
	}
	original, err := b.Slice(from, to)
	if err != nil || len(original) != to-from+1 {
		t.Fatalf("slice %d-%d: %v %d lines", from, to, err, len(original))
	}
	t.Logf("turn 1's fact is at seq %d, covered by block %d-%d (%s, %d bytes of summary); expand will read %d-%d back",
		seq, covering.From, covering.To, covering.Focus, len(covering.Text), from, to)

	model.setExpand(from, to)
	fromCall := model.callCount()
	if err := k.Handle(context.Background(), link.TerminalEvent("u1",
		"JOURNEY_RECALL State the key fact from the first turn, then expand the range a summary block covers and quote it.", time.Now())); err != nil {
		t.Fatalf("recall turn: %v", err)
	}
	recall := model.callsFrom(fromCall)
	first, ok := firstChat(recall)
	if !ok {
		t.Fatal("the recall turn made no chat call")
	}

	// 1. The fact has to come from a summary block, not from a raw line the tail
	// happened to keep.
	found := ""
	for _, m := range first.request {
		if strings.Contains(m.Content, journeyKeyFact) {
			found = m.Content
			break
		}
	}
	switch {
	case found == "":
		t.Errorf("criterion 1: the request of turn %d no longer carries %q; the summary chain lost turn 1", turns+1, journeyKeyFact)
	case !strings.Contains(found, "<summary "):
		t.Errorf("criterion 1: the fact is in the request but not in a summary block, so it is still raw: %q", clip(found))
	default:
		t.Logf("criterion 1: the fact reached the request inside a summary block (%d bytes): %q", len(found), clip(found))
	}
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	answer := lastAssistant(entries)
	if !strings.Contains(answer, journeyKeyFact) {
		t.Errorf("criterion 1: the bot's answer about turn 1 was %q; it does not name the fact", clip(answer))
	}
	t.Logf("criterion 1: the answer was %q", clip(answer))

	// 5. expand returned every covered line, by sequence number, unchanged.
	expanded, expandArgs := "", ""
	for _, c := range recall {
		for _, m := range c.request {
			if m.Role == provider.RoleAssistant {
				for _, tc := range m.ToolCalls {
					if tc.Function.Name == "expand" {
						expandArgs = tc.Function.Arguments
					}
				}
			}
			if m.Role == provider.RoleTool && strings.Contains(m.Content, "# seq ") {
				expanded = m.Content
			}
		}
	}
	if expandArgs == "" {
		t.Fatalf("criterion 5: the model was to call expand for %d-%d but made no expand call", from, to)
	}
	if expanded == "" {
		t.Fatalf("criterion 5: expand %s returned no output", expandArgs)
	}
	for i, e := range original {
		seq := from + i
		if want := fmt.Sprintf("# seq %d %s", seq, e.Message.Role); !strings.Contains(expanded, want) {
			t.Errorf("criterion 5: expand output has no %q header", want)
		}
		if !strings.Contains(expanded, e.Message.Content) {
			t.Errorf("criterion 5: expand output does not contain the original text of seq %d", seq)
		}
	}
	if !strings.Contains(expanded, journeyKeyFact) {
		t.Errorf("criterion 5: expand output for the covered range %d-%d does not carry the first turn's fact", from, to)
	}
	t.Logf("criterion 5: expand %s returned %d bytes with %d original lines, all matched", expandArgs, len(expanded), len(original))

	// --- 3. prefix cache across a pass ------------------------------------
	//
	// NOT COVERED as worded: this setup has no cache-hit data.
	//   - mockopenai's streams carry no usage object, so the real provider
	//     client marks every call Estimated and reports cached 0.
	//   - The bot's trace.jsonl only repeats those estimated counts.
	//   - The scripted provider below could model a cache (longest common
	//     prefix with the previous request), but a number this test invents for
	//     itself is not evidence about the implementation, so nothing is
	//     asserted on it. Missing data: a provider that reports
	//     prompt_tokens_details.cached_tokens, or a trace that records it.
	//
	// What IS covered here, from the requests the kernel really built: every
	// message before the new block is the same message in the same order, so a
	// real prefix cache still hits all of them. That is the property an
	// incremental pass provides, and rewriting an old block (the behaviour this
	// change replaced) breaks it at the first rewritten message.
	//
	// Also worth reporting: measured as cached over prompt with a shrinking
	// request, the 5-point bound cannot be met by any implementation. The pass
	// replaces a raw prefix of about 56k tokens with a summary block, so the
	// request drops from about 70k to about 14k tokens, and the bytes that
	// existed before are only the system message, memory, and the earlier
	// blocks, about 1.5k. The best possible post-pass rate is then about 11%,
	// so the fall is about 88 points whatever the code does. A countable bound
	// needs a different denominator, for example cached tokens over the previous
	// request's prompt.
	passList := cachePasses(cycles)
	if len(passList) == 0 {
		t.Errorf("criterion 3: no pass had a chat request on both sides, so not even the retained prefix could be checked")
	}
	for i, p := range passList {
		t.Logf("criterion 3, pass %d (turn %d): every message before the new block: %d required, %d byte-identical (%d tokens retained of %d post-pass tokens)",
			i+1, p.turn, p.keptMsgs, p.sharedMsgs, p.sharedTokens, p.postPrompt)
		if p.sharedMsgs < p.keptMsgs {
			t.Errorf("criterion 3: the pass at turn %d rewrote the retained prefix: only %d of the %d messages before the new block are unchanged. "+
				"An incremental pass appends a block and leaves the older ones byte identical, which is what keeps a provider's prefix cache hitting",
				p.turn, p.sharedMsgs, p.keptMsgs)
		}
	}
	if len(passList) > 0 {
		p := passList[0]
		t.Logf("criterion 3 arithmetic, from real request sizes: prompt %d -> %d tokens; only %d of the smaller request existed before, "+
			"so any implementation scores at most %.1f%% after the pass",
			p.prePrompt, p.postPrompt, p.sharedTokens, 100*float64(p.sharedTokens)/float64(p.postPrompt))
	}

}

// cachePass is one pass as the provider saw it.
type cachePass struct {
	turn                  int
	prePrompt, postPrompt int
	// keptMsgs is how many leading messages the pass must leave untouched: the
	// system message, the memory message, and the blocks that were already there.
	keptMsgs int
	// sharedMsgs is how many leading messages the two requests really share.
	sharedMsgs int
	// sharedTokens is the estimated size of that shared prefix.
	sharedTokens int
}

// cachePasses pairs the last chat request of one cycle with the first of the
// next. A pass lands at a turn boundary, so the two straddle it.
func cachePasses(cycles [][]journeyCall) []cachePass {
	var out []cachePass
	for i := 1; i < len(cycles); i++ {
		before := cycles[i-1][len(cycles[i-1])-1]
		after := cycles[i][0]
		kept, shared, sharedTokens := retainedPrefix(before.request, after.request)
		out = append(out, cachePass{
			turn:         after.turn,
			prePrompt:    before.prompt,
			postPrompt:   after.prompt,
			keptMsgs:     kept,
			sharedMsgs:   shared,
			sharedTokens: sharedTokens,
		})
	}
	return out
}

const (
	// journeyKeyFact is said once, in turn 1, and never again.
	journeyKeyFact = "KEEP:VESSEL-ORCHID-4821"
	// journeyMinTurns is the journey's own "about 200 turns".
	journeyMinTurns = 200
	// journeyPasses is how many compaction passes the run wants to see: more than
	// one, so a second block has to be appended after the first.
	journeyPasses = 3
	// journeyMaxTurns caps the loop so the test always ends.
	journeyMaxTurns = 900
	// journeyAfterPass is how many turns run past the last counted pass, so that
	// pass has a chat request on each side of it.
	journeyAfterPass = 3
	// journeyToolEvery is how often a turn does tool work.
	journeyToolEvery = 9
	// journeyPromptSlack is how far over the working set a prompt may go: one
	// turn's growth plus a tool pair and an environment update.
	journeyPromptSlack = 5000
	// journeyCacheDropLimit is the point 3 limit, as a share of the request.
	journeyCacheDropLimit = 0.05
	// journeyCompactShareLimit is the point 4 limit, as a share of all tokens.
	journeyCompactShareLimit = 0.02
	// journeyCacheSlack is how far under the arithmetic ceiling of a pass the
	// post-pass hit rate may sit: it covers the estimate's per-message overhead.
	journeyCacheSlack = 0.01
	// journeyRecall asks the bot for the first turn's fact.
	journeyRecall = "JOURNEY_RECALL"
	// journeyTool makes a turn do tool work.
	journeyTool = "JOURNEY_TOOL"
)

// journeyTurnText is one owner message. It is short on purpose: a real long
// session grows by a few hundred tokens a turn, and that size against the
// working set decides the compaction share of point 4.
func journeyTurnText(i int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "TURN-%03d ", i)
	if i == 1 {
		b.WriteString(journeyKeyFact)
		b.WriteString(" is the launch code; remember it for later on. ")
	}
	if i%journeyToolEvery == 0 {
		b.WriteString(journeyTool)
		b.WriteString(" ")
	}
	b.WriteString(strings.Repeat("background notes for this long run. ", 40))
	return b.String()
}

// journeyModel is a scripted provider. It has no model: it answers from a fixed
// script, records every request, and reports a cached-token count so the test
// can see which bytes the provider already had.
type journeyModel struct {
	mu    sync.Mutex
	seq   int
	turn  int
	from  int
	to    int
	prev  []provider.Message
	calls []journeyCall
}

// journeyCall is one provider call as the test sees it.
type journeyCall struct {
	turn       int
	compact    bool
	prompt     int
	completion int
	request    []provider.Message
}

// summarizerInstruction starts compact.LLM's system message. A call carrying it
// is a compaction, not a chat turn.
const summarizerInstruction = "Summarize the conversation so it can continue"

var keepMarker = regexp.MustCompile(`KEEP:[A-Z0-9-]+`)

func newJourneyModel() *journeyModel { return &journeyModel{} }

func (m *journeyModel) setTurn(t int) {
	m.mu.Lock()
	m.turn = t
	m.mu.Unlock()
}

func (m *journeyModel) setExpand(from, to int) {
	m.mu.Lock()
	m.from, m.to = from, to
	m.mu.Unlock()
}

func (m *journeyModel) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

func (m *journeyModel) callsFrom(i int) []journeyCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	if i >= len(m.calls) {
		return nil
	}
	return append([]journeyCall(nil), m.calls[i:]...)
}

func (m *journeyModel) allCalls() []journeyCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]journeyCall(nil), m.calls...)
}

// chatCalls is every call that is a conversation turn, in order.
func (m *journeyModel) chatCalls() []journeyCall {
	var out []journeyCall
	for _, c := range m.allCalls() {
		if !c.compact {
			out = append(out, c)
		}
	}
	return out
}

func (m *journeyModel) Chat(_ context.Context, req provider.Request, _ func(string)) (provider.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	call := journeyCall{turn: m.turn, prompt: compact.EstimateMessages(req.Messages), request: req.Messages}
	for _, msg := range req.Messages {
		if msg.Role == provider.RoleSystem && strings.HasPrefix(msg.Content, summarizerInstruction) {
			call.compact = true
			break
		}
	}
	var reply provider.Message
	if call.compact {
		reply = provider.Message{Role: provider.RoleAssistant, Content: summarizeScript(strings.Join(messageContents(req.Messages), "\n"))}
	} else {
		// No cache figure is reported. mockopenai sends no usage object and the
		// provider client then marks the call estimated with cached 0, so a
		// count the test made up itself would say nothing about the kernel.
		reply = m.answer(req)
	}
	call.completion = compact.EstimateMessage(reply)
	m.calls = append(m.calls, call)
	return reply, nil
}

// answer is the script. The summarizer keeps markers, so a summary chain either
// carries the first turn forward or is visibly broken; the answer to the recall
// question is built only from the markers visible in that request.
func (m *journeyModel) answer(req provider.Request) provider.Message {
	lastUser, sawShell, sawExpand := scanRequest(req)
	id := fmt.Sprintf("call_j%d", m.seq)
	switch {
	case strings.Contains(lastUser, journeyRecall) && !sawExpand:
		return toolCall(id, "expand", fmt.Sprintf(`{"from":%d,"to":%d}`, m.from, m.to))
	case strings.Contains(lastUser, journeyRecall):
		markers := keepMarker.FindAllString(strings.Join(messageContents(req.Messages), "\n"), -1)
		if len(markers) == 0 {
			return text("I cannot find the first turn's fact in what I can see.")
		}
		return text("The key fact from the first turn is " + markers[0] + ".")
	case strings.Contains(lastUser, journeyTool) && !sawShell:
		return toolCall(id, "shell", `{"command":"printf journey-step"}`)
	}
	return text("ack " + turnTag(lastUser))
}

// summarizeScript keeps every marker it was given and nothing else.
func summarizeScript(data string) string {
	seen := map[string]bool{}
	var kept []string
	for _, mk := range keepMarker.FindAllString(data, -1) {
		if seen[mk] {
			continue
		}
		seen[mk] = true
		kept = append(kept, mk)
	}
	out := "Summary of the covered turns."
	if len(kept) > 0 {
		out += " Keep: " + strings.Join(kept, " ") + "."
	}
	return out + " Decisions and unfinished work are unchanged."
}

func text(s string) provider.Message {
	return provider.Message{Role: provider.RoleAssistant, Content: s}
}

func toolCall(id, name, args string) provider.Message {
	return provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
		ID: id, Type: "function", Function: provider.FunctionCall{Name: name, Arguments: args},
	}}}
}

// scanRequest reads a conversation the way a scripted model does: the newest
// user message decides the step, and tool calls already made under it mean the
// step is further along.
func scanRequest(req provider.Request) (lastUser string, sawShell, sawExpand bool) {
	for _, msg := range req.Messages {
		if msg.Role == provider.RoleUser {
			lastUser = msg.Content
			sawShell, sawExpand = false, false
		}
		for _, tc := range msg.ToolCalls {
			switch tc.Function.Name {
			case "shell":
				sawShell = true
			case "expand":
				sawExpand = true
			}
		}
	}
	return lastUser, sawShell, sawExpand
}

func turnTag(s string) string {
	if f := strings.Fields(s); len(f) > 0 {
		return f[0]
	}
	return "unknown"
}

func messageContents(msgs []provider.Message) []string {
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, m.Content)
	}
	return out
}

func lastAssistant(entries []bot.Entry) string {
	out := ""
	for _, e := range entries {
		if e.Message.Role == provider.RoleAssistant && e.Message.Content != "" {
			out = e.Message.Content
		}
	}
	return out
}

func sameMessage(a, b provider.Message) bool {
	if a.Role != b.Role || a.Content != b.Content || a.Name != b.Name || a.ToolCallID != b.ToolCallID {
		return false
	}
	if len(a.ToolCalls) != len(b.ToolCalls) {
		return false
	}
	for i := range a.ToolCalls {
		if a.ToolCalls[i].ID != b.ToolCalls[i].ID ||
			a.ToolCalls[i].Function.Name != b.ToolCalls[i].Function.Name ||
			a.ToolCalls[i].Function.Arguments != b.ToolCalls[i].Function.Arguments {
			return false
		}
	}
	return true
}

// isSummary reports whether a message is a rendered summary block.
func isSummary(m provider.Message) bool {
	return m.Role == provider.RoleUser && strings.HasPrefix(m.Content, "<summary ")
}

// summaryHeader matches the range header a rendered block starts with.
var summaryHeader = regexp.MustCompile(`^<summary from="(\d+)" to="(\d+)">`)

// summarySig identifies the summaries in a request, in order, so a pass shows up
// as a change between two consecutive chat calls.
func summarySig(msgs []provider.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		if !isSummary(m) {
			continue
		}
		if h := summaryHeader.FindStringSubmatch(m.Content); h != nil {
			fmt.Fprintf(&b, "%s-%s;", h[1], h[2])
			continue
		}
		b.WriteString("?;")
	}
	return b.String()
}

// splitCycles groups chat calls by the summaries in their request. A new cycle
// starts at the call that first shows a new block.
func splitCycles(chat []journeyCall) [][]journeyCall {
	var cycles [][]journeyCall
	cur := ""
	for _, c := range chat {
		sig := summarySig(c.request)
		if len(cycles) == 0 || sig != cur {
			cycles = append(cycles, nil)
			cur = sig
		}
		cycles[len(cycles)-1] = append(cycles[len(cycles)-1], c)
	}
	return cycles
}

// retainedPrefix is the number of leading messages a pass must leave untouched
// (system, memory, and the blocks that were already there) and the number that
// really are identical in the two requests.
func retainedPrefix(before, after []provider.Message) (kept, shared, sharedTokens int) {
	firstSummary := len(after)
	for i, m := range after {
		if isSummary(m) {
			firstSummary = i
			break
		}
	}
	old := 0
	for _, m := range before {
		if isSummary(m) {
			old++
		}
	}
	kept = firstSummary + old
	if kept > len(after) {
		kept = len(after)
	}
	if kept > len(before) {
		kept = len(before)
	}
	for i := 0; i < kept; i++ {
		if !sameMessage(before[i], after[i]) {
			break
		}
		shared = i + 1
		sharedTokens += compact.EstimateMessage(after[i])
	}
	return kept, shared, sharedTokens
}

// cyclePromptAt is the largest prompt among chat calls of a given turn.
func cyclePromptAt(chat []journeyCall, turn int) int {
	n := 0
	for _, c := range chat {
		if c.turn == turn && c.prompt > n {
			n = c.prompt
		}
	}
	return n
}

// firstChat is the first conversation call of a turn, skipping any compaction.
func firstChat(calls []journeyCall) (journeyCall, bool) {
	for _, c := range calls {
		if !c.compact {
			return c, true
		}
	}
	return journeyCall{}, false
}

// checkBlocks asserts the chain's contract: the active blocks cover a
// contiguous range from 1, an existing block's text never changes, and a block
// that leaves the active list is covered by a wider one (the fold).
func checkBlocks(t *testing.T, turnBlocks [][]bot.SummaryBlock) {
	t.Helper()
	texts := map[[2]int]string{}
	for i, blocks := range turnBlocks {
		for j, blk := range blocks {
			switch {
			case j == 0 && blk.From != 1:
				t.Fatalf("turn %d: the first active block starts at %d, not 1: %+v", i+1, blk.From, blk)
			case j > 0 && blk.From != blocks[j-1].To+1:
				t.Fatalf("turn %d: block %d starts at %d but the one before ends at %d; the chain has a gap or an overlap: %s",
					i+1, j, blk.From, blocks[j-1].To, blockRanges(blocks))
			}
			key := [2]int{blk.From, blk.To}
			if prevText, ok := texts[key]; ok && prevText != blk.Text {
				t.Fatalf("turn %d: summary block %d-%d was rewritten; a pass must only append", i+1, blk.From, blk.To)
			}
			texts[key] = blk.Text
		}
	}
	last := turnBlocks[len(turnBlocks)-1]
	for key := range texts {
		still := false
		for _, blk := range last {
			if blk.From <= key[0] && blk.To >= key[1] {
				still = true
				break
			}
		}
		if !still {
			t.Fatalf("block %d-%d left the context and nothing covers it", key[0], key[1])
		}
	}
}

// countPasses counts summary blocks that covered new ground: one compaction pass
// each. A fold adds a block without covering new ground, so it is not counted.
func countPasses(turnBlocks [][]bot.SummaryBlock) int {
	passes, prev := 0, 0
	for _, blocks := range turnBlocks {
		for _, blk := range blocks {
			if blk.To > prev {
				passes++
				prev = blk.To
			}
		}
	}
	return passes
}

func blockRanges(blocks []bot.SummaryBlock) string {
	if len(blocks) == 0 {
		return "none"
	}
	parts := make([]string, len(blocks))
	for i, blk := range blocks {
		parts[i] = fmt.Sprintf("%d-%d(%s,%dB)", blk.From, blk.To, blk.Focus, len(blk.Text))
	}
	return strings.Join(parts, " ")
}

func blockCovering(blocks []bot.SummaryBlock, seq int) *bot.SummaryBlock {
	for i, blk := range blocks {
		if blk.From <= seq && blk.To >= seq {
			return &blocks[i]
		}
	}
	return nil
}

// factSeq is the transcript sequence of the line that first states the key fact.
func factSeq(t *testing.T, b *bot.Bot) int {
	t.Helper()
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	for i, e := range entries {
		if strings.Contains(e.Message.Content, journeyKeyFact) {
			return i + 1
		}
	}
	t.Fatalf("turn 1's fact %q is not in the transcript", journeyKeyFact)
	return 0
}

// compactCost is the compaction share of the run's model tokens.
type compactCost struct {
	compact, total, compactCalls, calls int
}

func (c compactCost) ratio() float64 {
	if c.total == 0 {
		return 0
	}
	return float64(c.compact) / float64(c.total)
}

// measureCompactCost sums prompt and completion tokens. A turn resends the whole
// context, so the total is far larger than the bytes the conversation added;
// that total is the base point 4 asks for.
func measureCompactCost(all []journeyCall) compactCost {
	var c compactCost
	for _, call := range all {
		n := call.prompt + call.completion
		c.total += n
		c.calls++
		if call.compact {
			c.compact += n
			c.compactCalls++
		}
	}
	return c
}

// clip shortens a string for a test log.
func clip(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 160 {
		return s[:160] + "..."
	}
	return s
}
