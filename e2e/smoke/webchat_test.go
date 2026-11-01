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
// docs/guard/六条基本旅程.md). It logs in, sends two messages, reads the chat timeline
// twice as a refresh would, then opens `xbot chat` on the same bot and checks
// that both directions of the shared conversation show up.
//
// Browser rendering is covered by TestChatTabsJourney. This case checks the
// actual delivery events and persisted timeline shared by web and terminal.
func TestWebChat(t *testing.T) {
	stub := StartStub(t)
	srv := Start(t, stub)
	srv.CreateBot("ada", "You are ada, the bot in the web chat smoke test.")

	first := srv.Send("ada", "SMOKE_HELLO_1")
	second := srv.Send("ada", "SMOKE_HELLO_2")
	want1 := "你好，这是第 1 次流式回复"
	want2 := "你好，这是第 2 次流式回复"
	checkDelivered(t, "SMOKE_HELLO_1", first, want1)
	checkDelivered(t, "SMOKE_HELLO_2", second, want2)

	// Two reads in a row stand in for a page refresh: the same rows, the same
	// order, nothing duplicated or lost.
	refreshed := srv.ChatRows("ada")
	again := srv.ChatRows("ada")
	if !reflect.DeepEqual(refreshed, again) {
		t.Fatalf("two chat reads differ:\nfirst:\n%s\nsecond:\n%s", dumpRows(refreshed), dumpRows(again))
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
		rows := srv.ChatRows("ada")
		terminalQ = rowIndexes(rows, "user", "SMOKE_FROM_TERMINAL")
		terminalA = rowIndexes(rows, "assistant", "终端你好")
		return len(terminalQ) == 1 && len(terminalA) == 1, dumpRows(rows)
	})
	if !(a2[0] < terminalQ[0] && terminalQ[0] < terminalA[0]) {
		t.Fatalf("terminal rows are not after answer 2 and in order: answer 2 at %d, terminal q/a at %d/%d\n%s",
			a2[0], terminalQ[0], terminalA[0], dumpRows(srv.ChatRows("ada")))
	}

	// The terminal continues the model context with both web answers.
	body := requestBodyOf(t, stub, "SMOKE_FROM_TERMINAL")
	for _, text := range []string{"SMOKE_HELLO_1", want1, "SMOKE_HELLO_2", want2} {
		if !strings.Contains(body, text) {
			t.Fatalf("terminal context missing %q", text)
		}
	}
	term.Quit()
}

// checkDelivered checks the first successful owner-message result, not model
// tokens or progress events. A delivery must arrive within the journey's 3 s.
func checkDelivered(t *testing.T, label string, r *Reply, want string) {
	t.Helper()
	if len(r.Delivered) == 0 {
		t.Fatalf("%s: no owner message delivered", label)
	}
	delay := r.Delivered[0].At.Sub(r.Sent)
	t.Logf("%s: first owner message after %d ms", label, delay.Milliseconds())
	if delay >= 3*time.Second {
		t.Fatalf("%s: owner message took %s, want under 3s", label, delay)
	}
	var texts []string
	for _, d := range r.Delivered {
		texts = append(texts, d.Text)
	}
	if got := strings.Join(texts, "\n\n"); got != want {
		t.Fatalf("%s: delivered %q, want %q", label, got, want)
	}
	if r.Done != want {
		t.Fatalf("%s: final reply %q, want %q", label, r.Done, want)
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
