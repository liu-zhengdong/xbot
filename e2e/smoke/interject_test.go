//go:build smoke

package smoke

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestInterjectMidTask is the smoke case for FOCUS.md 「忙时插话」 (spec in
// docs/dev/smoke.md). It covers both halves:
//
// A: a subagent is running a long task and the owner says something else.
// B: the foreground is waiting on the model and the owner says something else.
//
// Not covered: how the 任务 panel draws the agent (only the agents data it
// reads), and interjecting while the foreground runs a slow tool: ADR 0017
// waits for the tool to return, which can exceed 3 seconds. That is a known
// gap in docs/CANDIDATES.md and outside this case.
func TestInterjectMidTask(t *testing.T) {
	stub := StartStub(t)
	srv := Start(t, stub)
	srv.CreateBot("ada", "You are ada, the bot in the interjection smoke test.")

	// A. The foreground hands the job to a subagent, which parks in the stub.
	long := srv.Send("ada", "SMOKE_LONG")
	if long.Done != "已交给后台" {
		t.Fatalf("SMOKE_LONG: done.text %q, want %q", long.Done, "已交给后台")
	}
	WaitFor(t, 15*time.Second, "the subagent to reach the long gate", func() (bool, string) {
		return stub.Entered("long"), dumpRequests(stub)
	})

	before := runningAgent(t, srv, "ada", "SMOKE_LONG_TASK")

	// Talking to the bot while the subagent works gets an answer, and the
	// subagent keeps running.
	aside := srv.Send("ada", "SMOKE_ASIDE")
	checkFirstDelta(t, "SMOKE_ASIDE", aside, "插话的回答")

	after := runningAgent(t, srv, "ada", "SMOKE_LONG_TASK")
	if after.ID != before.ID {
		t.Fatalf("the running subagent changed across the aside: %s -> %s\n%s",
			before.ID, after.ID, dumpAgents(srv.Agents("ada")))
	}

	// Let the long task finish: its report reaches the conversation once.
	stub.Release("long")
	var reportAt int
	WaitFor(t, 60*time.Second, "the long task report in the transcript", func() (bool, string) {
		rows := srv.Transcript("ada")
		reportAt = countRows(rows, "assistant", "LONG_RESULT_42")
		return reportAt >= 1, dumpRows(rows)
	})
	if reportAt != 1 {
		t.Fatalf("the transcript has %d assistant replies with LONG_RESULT_42, want exactly 1:\n%s",
			reportAt, dumpRows(srv.Transcript("ada")))
	}
	// The long task started once: one subagent record carries that task.
	agents := srv.Agents("ada")
	started := 0
	for _, a := range agents {
		if strings.Contains(a.Task, "SMOKE_LONG_TASK") {
			started++
		}
	}
	if started != 1 {
		t.Fatalf("the panel lists %d subagents tasked with SMOKE_LONG_TASK, want exactly 1:\n%s", started, dumpAgents(agents))
	}

	// B. The foreground is waiting on the model. The newer owner line goes
	// straight into that turn: the waiting call is cancelled and re-sent with
	// both lines, and the answer streams to the newer request.
	slow := srv.BeginSend("ada", "SMOKE_SLOW")
	WaitFor(t, 15*time.Second, "the foreground to reach the model gate", func() (bool, string) {
		return stub.Entered("fg"), dumpRequests(stub)
	})
	aside2 := srv.Send("ada", "SMOKE_ASIDE_2")
	checkFirstDelta(t, "SMOKE_ASIDE_2", aside2, "先答插话")

	slowReply := slow.Wait()
	if !slowReply.HandedOff {
		t.Fatalf("the SMOKE_SLOW stream did not end as handed off: done=%q error=%q", slowReply.Done, slowReply.Error)
	}
	if slowReply.Done != "" {
		t.Fatalf("the SMOKE_SLOW stream ended with text %q, want none: the answer belongs to the aside", slowReply.Done)
	}

	both := 0
	for _, r := range stub.Requests() {
		if r.Caller == callerForeground && strings.Contains(r.Body, "SMOKE_SLOW") && strings.Contains(r.Body, "SMOKE_ASIDE_2") {
			both++
		}
	}
	if both != 1 {
		t.Fatalf("the stub saw %d foreground requests carrying both SMOKE_SLOW and SMOKE_ASIDE_2, want 1\n%s",
			both, dumpRequests(stub))
	}
}

// checkFirstDelta asserts the reply starts under 3 seconds and ends with want.
func checkFirstDelta(t *testing.T, label string, r *Reply, want string) {
	t.Helper()
	var body []Delta
	for _, d := range r.Deltas {
		if d.Text != "" {
			body = append(body, d)
		}
	}
	if len(body) == 0 {
		t.Fatalf("%s: got no non-empty delta, want an answer", label)
	}
	delay := body[0].At.Sub(r.Sent)
	t.Logf("%s: first delta after %d ms", label, delay.Milliseconds())
	if delay >= 3*time.Second {
		t.Fatalf("%s: first non-empty delta took %d ms, want under 3000 ms", label, delay.Milliseconds())
	}
	if r.Done != want {
		t.Fatalf("%s: done.text %q, want %q", label, r.Done, want)
	}
}

// runningAgent returns the one running subagent whose task names marker.
func runningAgent(t *testing.T, srv *Server, bot, marker string) Agent {
	t.Helper()
	agents := srv.Agents(bot)
	var found []Agent
	for _, a := range agents {
		if a.Status == "running" && strings.Contains(a.Task, marker) {
			found = append(found, a)
		}
	}
	if len(found) != 1 {
		t.Fatalf("expected one running subagent whose task contains %s, got %d:\n%s", marker, len(found), dumpAgents(agents))
	}
	return found[0]
}

// countRows counts the rows with the role whose text contains substr.
func countRows(rows []Row, role, substr string) int {
	n := 0
	for _, r := range rows {
		if r.Role == role && strings.Contains(r.Content, substr) {
			n++
		}
	}
	return n
}

func dumpAgents(agents []Agent) string {
	raw, err := json.MarshalIndent(agents, "", "  ")
	if err != nil {
		return fmt.Sprintf("%+v", agents)
	}
	return string(raw)
}

// dumpRequests lists the model calls so a timing failure can be traced.
func dumpRequests(stub *Stub) string {
	var b strings.Builder
	for i, r := range stub.Requests() {
		body := r.Body
		if len(body) > 400 {
			body = body[:400] + "…"
		}
		fmt.Fprintf(&b, "%d %s caller=%s bot=%s\n  %s\n", i, r.At.Format(time.RFC3339Nano), r.Caller, r.Bot, body)
	}
	return b.String()
}
