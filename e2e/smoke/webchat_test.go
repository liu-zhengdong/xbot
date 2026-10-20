//go:build smoke

package smoke

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestWebChat is the smoke case for 「网页对话」 (journey and spec in
// docs/guard/六条基本旅程.md). It logs in, sends two messages, reads the transcript
// twice as a refresh would, then opens `xbot chat` on the same bot and checks
// that both directions of the shared conversation show up.
//
// Not covered: how the page renders a reply or a refresh. The repository has
// no headless browser, so this checks the data the interface reads.
func TestWebChat(t *testing.T) {
	stub := StartStub(t)
	srv := Start(t, stub)
	srv.CreateBot("ada", "You are ada, the bot in the web chat smoke test.")

	first := srv.Send("ada", "SMOKE_HELLO_1")
	second := srv.Send("ada", "SMOKE_HELLO_2")
	want1 := "你好，这是第 1 次流式回复"
	want2 := "你好，这是第 2 次流式回复"
	checkStreamed(t, "SMOKE_HELLO_1", first, want1)
	checkStreamed(t, "SMOKE_HELLO_2", second, want2)

	// Two reads in a row stand in for a page refresh: the same rows, the same
	// order, nothing duplicated or lost.
	refreshed := srv.Transcript("ada")
	again := srv.Transcript("ada")
	if !reflect.DeepEqual(refreshed, again) {
		t.Fatalf("two transcript reads differ:\nfirst:\n%s\nsecond:\n%s", dumpRows(refreshed), dumpRows(again))
	}

	checkSeqOrder(t, refreshed)
	checkSeqOrder(t, again)

	q1 := rowIndexes(refreshed, "user", "SMOKE_HELLO_1")
	a1 := rowIndexes(refreshed, "assistant", want1)
	q2 := rowIndexes(refreshed, "user", "SMOKE_HELLO_2")
	a2 := rowIndexes(refreshed, "assistant", want2)
	for _, want := range []struct {
		what    string
		indexes []int
	}{
		{"question SMOKE_HELLO_1", q1},
		{"answer to SMOKE_HELLO_1", a1},
		{"question SMOKE_HELLO_2", q2},
		{"answer to SMOKE_HELLO_2", a2},
	} {
		if len(want.indexes) != 1 {
			t.Fatalf("%s appears %d times in the transcript, want once:\n%s", want.what, len(want.indexes), dumpRows(refreshed))
		}
	}
	if !(q1[0] < a1[0] && a1[0] < q2[0] && q2[0] < a2[0]) {
		t.Fatalf("transcript rows are not q1 < a1 < q2 < a2: positions %v %v %v %v\n%s", q1[0], a1[0], q2[0], a2[0], dumpRows(refreshed))
	}

	// The terminal opens on the same bot. What it says next is the same
	// conversation the page reads.
	term := srv.Chat("ada")
	term.Say("SMOKE_FROM_TERMINAL")

	var terminalQ, terminalA []int
	WaitFor(t, 15*time.Second, "the terminal exchange in the web transcript", func() (bool, string) {
		rows := srv.Transcript("ada")
		terminalQ = rowIndexes(rows, "user", "SMOKE_FROM_TERMINAL")
		terminalA = rowIndexes(rows, "assistant", "终端你好")
		return len(terminalQ) == 1 && len(terminalA) == 1, dumpRows(rows)
	})
	if !(a2[0] < terminalQ[0] && terminalQ[0] < terminalA[0]) {
		t.Fatalf("terminal rows are not after answer 2 and in order: answer 2 at %d, terminal q/a at %d/%d\n%s",
			a2[0], terminalQ[0], terminalA[0], dumpRows(srv.Transcript("ada")))
	}

	// A message sent from the page while the terminal is open shows up in both.
	late := srv.Send("ada", "SMOKE_FROM_WEB_LATE")
	if late.Done != "网页后话" {
		t.Fatalf("SMOKE_FROM_WEB_LATE: done.text %q, want %q", late.Done, "网页后话")
	}
	WaitFor(t, 15*time.Second, "the web exchange in the transcript", func() (bool, string) {
		rows := srv.Transcript("ada")
		q := rowIndexes(rows, "user", "SMOKE_FROM_WEB_LATE")
		a := rowIndexes(rows, "assistant", "网页后话")
		return len(q) == 1 && len(a) == 1 && terminalA[0] < q[0] && q[0] < a[0], dumpRows(rows)
	})
	WaitFor(t, 15*time.Second, "the web exchange in the terminal", func() (bool, string) {
		out := term.Stdout()
		return strings.Contains(out, "SMOKE_FROM_WEB_LATE") && strings.Contains(out, "网页后话"), out
	})
	term.Quit()
}

// checkStreamed asserts the timing and the shape of one streamed reply:
// the first non-empty delta under 3 seconds, at least three non-empty deltas
// spread over 300 ms, and done.text equal to those segments joined. Empty
// deltas do not count as the reply starting or as streaming.
func checkStreamed(t *testing.T, label string, r *Reply, want string) {
	t.Helper()
	var body []Delta
	for _, d := range r.Deltas {
		if d.Text != "" {
			body = append(body, d)
		}
	}
	if len(body) < 3 {
		t.Fatalf("%s: got %d non-empty deltas, want at least 3", label, len(body))
	}
	delay := body[0].At.Sub(r.Sent)
	t.Logf("%s: first delta after %d ms", label, delay.Milliseconds())
	if delay >= 3*time.Second {
		t.Fatalf("%s: first non-empty delta took %d ms, want under 3000 ms", label, delay.Milliseconds())
	}
	span := body[len(body)-1].At.Sub(body[0].At)
	if span < 300*time.Millisecond {
		t.Fatalf("%s: first and last non-empty delta are %d ms apart, want at least 300 ms", label, span.Milliseconds())
	}
	var streamed strings.Builder
	for _, d := range body {
		streamed.WriteString(d.Text)
	}
	if streamed.String() != want {
		t.Fatalf("%s: streamed %q, want %q", label, streamed.String(), want)
	}
	if r.Done != want {
		t.Fatalf("%s: done.text %q, want %q", label, r.Done, want)
	}
}

// rowIndexes returns the position in the returned array of every row with the
// role and the exact text. The page draws rows in array order, so tests that
// care about what the owner sees check positions, not just seq.
func rowIndexes(rows []Row, role, want string) []int {
	var out []int
	for i, r := range rows {
		if r.Role == role && strings.TrimSpace(r.Content) == want {
			out = append(out, i)
		}
	}
	return out
}

// checkSeqOrder requires the array itself to be in increasing seq order.
func checkSeqOrder(t *testing.T, rows []Row) {
	t.Helper()
	for i := 1; i < len(rows); i++ {
		if rows[i].Seq <= rows[i-1].Seq {
			t.Fatalf("transcript rows are not in seq order at position %d: seq %d after %d\n%s",
				i, rows[i].Seq, rows[i-1].Seq, dumpRows(rows))
		}
	}
}

func dumpRows(rows []Row) string {
	raw, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return fmt.Sprintf("%+v", rows)
	}
	return string(raw)
}
