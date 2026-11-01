//go:build smoke

package smoke

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/memory"
	"xbot/internal/peer"
)

func TestCLIBotForkRuntimeCurrentDuringArchive(t *testing.T) {
	cliSkillIsolation(t)
	const old, current, child = "ForkArchiveOld", "ForkArchiveCurrent", "ForkArchiveChild"
	const role = "Review the frozen public input and ask the current source for details."
	const handoff = "FROZEN_ARCHIVE_CHOSEN_HANDOFF: the public project is ready for review."
	model := newCLIBotForkModel(t, handoff)
	entered, release := make(chan struct{}, 1), make(chan struct{})
	model.entered, model.release = entered, release
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	srv := cliBotForkPartialSource(t, model, old)
	srv.Restart()
	const cue = "CLI_FORK_NETWORK_CURRENT_MOVE"
	command := `printf '%s\n' "$XBOT_CALL_ID" > fork-original-call.txt` + "\n" +
		`"$XBOT_BIN" bot-fork ` + child + ` --self --role ` + shellQuote(role) + ` --group 'Current archive group' --json &` + "\n" +
		`fork_pid=$!` + "\n" + `while [ ! -f fork-route-ready ]; do sleep 0.025; done` + "\n" +
		`"$XBOT_BIN" profile set --self --name ` + current + ` --json` + "\n" +
		`printf 'FORK_CURRENT_POST_MODEL_RULE\n' > ` + shellQuote(filepath.Join(srv.Root, "bots", current, "rules.md")) + "\n" +
		`printf 'CURRENT_ROUTE_FINISHED\n' > fork-route-done` + "\n" + `wait "$fork_pid"`
	model.base.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "Rename the actual source while original archive model call is held outside its route", "command": command}))
	done := make(chan struct{})
	go func() { defer close(done); cliSkillSend(t, srv, old, cue) }()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("original nonempty compact.LLM did not reach real scripted provider")
	}
	model.mu.Lock()
	archives := append([]cliBotForkArchiveRequest(nil), model.archive...)
	model.mu.Unlock()
	if len(archives) != 1 || len(archives[0].Tools) != 0 {
		t.Fatal("current test did not hold exactly one original no-tools archive Chat")
	}
	input := ""
	for _, msg := range archives[0].Messages {
		input += msg.Content + "\n"
	}
	for _, marker := range []string{old, role, "FORK_PARTIAL_SAVED_SUMMARY", "FORK_PARTIAL_UNCOVERED_RAW_DECISION", "FORK_PARTIAL_SOURCE_BOT_FACT"} {
		if !strings.Contains(input, marker) {
			t.Fatal("archive input was not the frozen original current source history/facts/persona")
		}
	}
	// Projected transcript legitimately contains this original Shell's future
	// rename/rules commands. Only frozen prior metadata identifies source state.
	prior, _, ok := strings.Cut(input, "</prior_summary>")
	if !ok || !strings.Contains(prior, "BOTNAME="+old) || strings.Contains(prior, "BOTNAME="+current) || strings.Contains(prior, "FORK_CURRENT_POST_MODEL_RULE") {
		t.Fatal("archive prior metadata did not freeze the original current source")
	}
	if err := os.WriteFile(filepath.Join(srv.Root, "work", old, "fork-route-ready"), []byte("ALLOW_REAL_SOURCE_MOVE\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// The same actual Shell performs a second source operation before the LLM
	// is released. This cannot complete if the model holds the source route.
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	moved := false
	for !moved {
		if _, err := os.Stat(filepath.Join(srv.Root, "work", current, "fork-route-done")); err == nil {
			moved = true
			break
		}
		select {
		case <-timer.C:
			t.Fatal("actual current-source route was locked across the archive network call")
		case <-done:
			t.Fatal("source Shell finished without completing the model-held current route operation")
		case <-tick.C:
		}
	}
	if _, err := os.Stat(filepath.Join(srv.Root, "bots", current, "config.json")); err != nil {
		t.Fatal("same original Shell did not actually rename its source before archive returned")
	}
	model.mu.Lock()
	heldCount := len(model.archive)
	model.mu.Unlock()
	if heldCount != 1 {
		t.Fatal("current route operation retried or replaced the held original archive")
	}
	close(release)
	released = true
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("original fork did not finish after releasing its real archive response")
	}
	results := model.base.results(cue)
	if len(results) != 1 || !strings.HasPrefix(results[0], "exit 0\n") || len(commandOutput(results[0])) > 32<<10 {
		t.Fatal("same original Shell lost bounded successful current move/fork acknowledgements")
	}
	objects := strings.Split(strings.TrimSpace(commandOutput(results[0])), "\n")
	if len(objects) != 2 {
		t.Fatal("same Shell did not return exactly move and fork JSON acknowledgements")
	}
	var move struct {
		Name           string
		PreviousName   string `json:"previous_name"`
		Applied, Moved bool
		Error          string
	}
	var reply cliBotForkReply
	if json.Unmarshal([]byte(objects[0]), &move) != nil || json.Unmarshal([]byte(objects[1]), &reply) != nil {
		t.Fatal("actual current move/fork typed replies were not valid complete JSON")
	}
	if move.Name != current || move.PreviousName != old || !move.Applied || !move.Moved || move.Error != "" || reply.Name != child || !reply.Created || reply.Stage != "complete" || reply.Error != "" || reply.UncertainMemory != nil || reply.HandoffParts != reply.HandoffTotal || reply.HandoffParts <= 0 || reply.GroupID == "" || !slices.Equal(reply.Confirmed, []string{"owners", "binding", "rules", "handoff", "child_note", "source_note", "group"}) {
		t.Fatal("model-held current move lost real fork completion facts")
	}
	created, err := bot.Load(srv.Root, child)
	if err != nil {
		t.Fatal(err)
	}
	rules, err := created.ReadRules("")
	if err != nil || strings.TrimSpace(rules) != "FORK_CURRENT_POST_MODEL_RULE" {
		t.Fatal("fork used stale pre-model source rules instead of original current rules read")
	}
	mem := memory.Open(srv.Root)
	for _, name := range []string{current, child} {
		facts, err := mem.Recall(context.Background(), memory.Query{Scope: memory.ScopeBot, BotName: name, Tier: memory.TierProfile})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, fact := range facts {
			want := "fork-source"
			if name == current {
				want = "fork-" + child
			}
			if fact.Key == want {
				found = true
				if fact.WrittenBy != current || !strings.Contains(fact.Text, "peer-message") || strings.Contains(fact.Text, old) {
					t.Fatal("post-model recognition note used an old source name or writer")
				}
			}
		}
		if !found {
			t.Fatal("actual post-model current source or child recognition note was not committed")
		}
	}
	groups, err := (&peer.Bus{Root: srv.Root}).ListGroups()
	if err != nil || len(groups) != 1 || groups[0].ID != reply.GroupID || !slices.Equal(groups[0].Members, []string{child, current}) {
		t.Fatal("post-model group did not use original current source membership")
	}
	actual, err := bot.Load(srv.Root, current)
	if err != nil {
		t.Fatal(err)
	}
	ownerSeq := 0
	if err := actual.Walk(1, 0, func(seq int, e bot.Entry) error {
		if e.Message.Role == "user" && strings.Contains(e.Message.Text(), cue) {
			ownerSeq = seq
		}
		return nil
	}); err != nil || ownerSeq <= 0 {
		t.Fatal("actual prepared owner input sequence missing from current source transcript")
	}
	traces, err := actual.Traces()
	if err != nil {
		t.Fatal(err)
	}
	compactCount := 0
	for _, trace := range traces {
		if trace.Call == bot.TraceCallCompact {
			compactCount++
			if trace.Turn != ownerSeq || trace.Prompt != 20 || trace.Completion != 10 {
				t.Fatal("original archive usage lost its actual prepared turn sequence after current-source move")
			}
		}
	}
	if compactCount != 1 {
		t.Fatal("original compact usage was not recorded exactly once on current source")
	}
	panel := srv.Memory(child)
	if !strings.Contains(panel.Bot.Markdown, handoff) || !strings.Contains(panel.Bot.Markdown, current) {
		t.Fatal("existing child memory consumer did not read frozen handoff/current recognition note")
	}
	for _, path := range []string{filepath.Join(srv.Root, "bots", old), filepath.Join(srv.Root, "work", old)} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("post-model source writes or current consumer recreated an old Bot/work path")
		}
	}
}
