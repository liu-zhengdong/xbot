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

// TestWebChat is the smoke case for FOCUS.md 「网页对话」 (spec in
// docs/dev/smoke.md). It logs in, sends two messages, reads the transcript
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

	q1 := findRows(refreshed, "user", "SMOKE_HELLO_1")
	a1 := findRows(refreshed, "assistant", want1)
	q2 := findRows(refreshed, "user", "SMOKE_HELLO_2")
	a2 := findRows(refreshed, "assistant", want2)
	for _, want := range []struct {
		what string
		seqs []int
	}{
		{"question SMOKE_HELLO_1", q1},
		{"answer to SMOKE_HELLO_1", a1},
		{"question SMOKE_HELLO_2", q2},
		{"answer to SMOKE_HELLO_2", a2},
	} {
		if len(want.seqs) != 1 {
			t.Fatalf("%s appears %d times in the transcript, want once:\n%s", want.what, len(want.seqs), dumpRows(refreshed))
		}
	}
	if !(q1[0] < a1[0] && a1[0] < q2[0] && q2[0] < a2[0]) {
		t.Fatalf("transcript order is not q1 < a1 < q2 < a2: %v %v %v %v", q1[0], a1[0], q2[0], a2[0])
	}

	// The terminal opens on the same bot. What it says next is the same
	// conversation the page reads.
	term := srv.Chat("ada")
	term.Say("SMOKE_FROM_TERMINAL")

	var terminalQ, terminalA []int
	WaitFor(t, 15*time.Second, "the terminal exchange in the web transcript", func() (bool, string) {
		rows := srv.Transcript("ada")
		terminalQ = findRows(rows, "user", "SMOKE_FROM_TERMINAL")
		terminalA = findRows(rows, "assistant", "终端你好")
		return len(terminalQ) == 1 && len(terminalA) == 1, dumpRows(rows)
	})
	if terminalQ[0] <= a2[0] || terminalA[0] <= a2[0] {
		t.Fatalf("terminal lines (seq %d, %d) are not after answer 2 (seq %d)", terminalQ[0], terminalA[0], a2[0])
	}

	// A message sent from the page while the terminal is open shows up in both.
	late := srv.Send("ada", "SMOKE_FROM_WEB_LATE")
	if late.Done != "网页后话" {
		t.Fatalf("SMOKE_FROM_WEB_LATE: done.text %q, want %q", late.Done, "网页后话")
	}
	WaitFor(t, 15*time.Second, "the web exchange in the transcript", func() (bool, string) {
		rows := srv.Transcript("ada")
		q := findRows(rows, "user", "SMOKE_FROM_WEB_LATE")
		a := findRows(rows, "assistant", "网页后话")
		return len(q) == 1 && len(a) == 1 && q[0] > terminalA[0] && a[0] > q[0], dumpRows(rows)
	})
	WaitFor(t, 15*time.Second, "the web exchange in the terminal", func() (bool, string) {
		out := term.Stdout()
		return strings.Contains(out, "SMOKE_FROM_WEB_LATE") && strings.Contains(out, "网页后话"), out
	})
	term.Quit()
}

// checkStreamed asserts the timing and the shape of one streamed reply:
// first delta under 3 seconds, at least three deltas spread over 300 ms, and
// done.text equal to the segments joined.
func checkStreamed(t *testing.T, label string, r *Reply, want string) {
	t.Helper()
	if len(r.Deltas) < 3 {
		t.Fatalf("%s: got %d deltas, want at least 3", label, len(r.Deltas))
	}
	delay := r.Deltas[0].At.Sub(r.Sent)
	t.Logf("%s: first delta after %d ms", label, delay.Milliseconds())
	if delay >= 3*time.Second {
		t.Fatalf("%s: first delta took %d ms, want under 3000 ms", label, delay.Milliseconds())
	}
	span := r.Deltas[len(r.Deltas)-1].At.Sub(r.Deltas[0].At)
	if span < 300*time.Millisecond {
		t.Fatalf("%s: first and last delta are %d ms apart, want at least 300 ms", label, span.Milliseconds())
	}
	var streamed strings.Builder
	for _, d := range r.Deltas {
		streamed.WriteString(d.Text)
	}
	if streamed.String() != want {
		t.Fatalf("%s: streamed %q, want %q", label, streamed.String(), want)
	}
	if r.Done != want {
		t.Fatalf("%s: done.text %q, want %q", label, r.Done, want)
	}
}

// findRows returns the seq of every row with the role and the exact text.
func findRows(rows []Row, role, want string) []int {
	var seqs []int
	for _, r := range rows {
		if r.Role == role && strings.TrimSpace(r.Content) == want {
			seqs = append(seqs, r.Seq)
		}
	}
	return seqs
}

func dumpRows(rows []Row) string {
	raw, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return fmt.Sprintf("%+v", rows)
	}
	return string(raw)
}
