//go:build smoke

package smoke

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestRemember is the smoke case for 「记住事情」 (journey and spec in
// docs/guard/六条基本旅程.md): the owner tells ada something about themselves, serve
// restarts, ada and bob both answer from it, the panel shows it, and a
// correction reaches the model after the old fact.
//
// Not covered: whether the answer follows the newer wording. That is the model
// reading dates and judging; the stub cannot. This case shows the newer line
// is delivered and ordered after the old one, and the panel shows it.
func TestRemember(t *testing.T) {
	stub := StartStub(t)
	srv := Start(t, stub)
	srv.CreateBot("ada", "You are ada, the bot in the remember smoke test.")
	srv.CreateBot("bob", "You are bob, another bot in the remember smoke test.")

	if r := srv.Send("ada", "SMOKE_REMEMBER 我对花生过敏"); r.Done != "记住了" {
		t.Fatalf("SMOKE_REMEMBER: done.text %q, want %q", r.Done, "记住了")
	}

	srv.Restart()

	// The same bot answers after the restart.
	ada := srv.Send("ada", "SMOKE_RECALL 我有什么忌口")
	if !strings.Contains(ada.Done, "MARK_PEANUT_1") {
		t.Fatalf("ada after restart does not answer with MARK_PEANUT_1 (旅程「记住事情」: 重启之后，同一个 Bot 答得出来): %q", ada.Done)
	}

	// A fact about the owner is visible to another bot too.
	bob := srv.Send("bob", "SMOKE_RECALL 我有什么忌口")
	if !strings.Contains(bob.Done, "MARK_PEANUT_1") {
		t.Fatalf("bob does not answer with MARK_PEANUT_1 (旅程「记住事情」: 关于主人本人的事，另一个 Bot 也答得出来): %q", bob.Done)
	}

	// The panel's memory list carries the person entry.
	if !hasPersonFact(srv, "ada", "花生") {
		t.Fatalf("ada's panel has no person memory entry about 花生 (旅程「记住事情」: 详情的「记忆」里能看到这一条):\n%s",
			dumpFacts(srv.Memory("ada")))
	}

	if r := srv.Send("ada", "SMOKE_CORRECT 其实我不过敏了"); r.Done != "改好了" {
		t.Fatalf("SMOKE_CORRECT: done.text %q, want %q", r.Done, "改好了")
	}

	corrected := srv.Send("ada", "SMOKE_RECALL 我有什么忌口")
	if !strings.Contains(corrected.Done, "MARK_PEANUT_2") {
		t.Fatalf("the recall after the correction does not carry MARK_PEANUT_2 (旅程「记住事情」: 更正之后，回答以最新的说法为准): %q", corrected.Done)
	}

	// The corrected fact reaches the model, ordered after the old one (or
	// without it). The check reads the whole request: the memory block keeps
	// the snapshot the bot first saw (it is the stable prefix), so a bot's own
	// write reaches the model through the tool call in the request instead.
	body := requestBodyOf(t, stub, "SMOKE_CORRECT")
	first := strings.Index(body, "MARK_PEANUT_1")
	second := strings.Index(body, "MARK_PEANUT_2")
	if second < 0 {
		t.Fatalf("the request after the correction has no MARK_PEANUT_2 (旅程「记住事情」: 更正之后，回答以最新的说法为准)")
	}
	if first >= 0 && first > second {
		t.Fatalf("MARK_PEANUT_1 comes after MARK_PEANUT_2 in the request (旅程「记住事情」: 最新的说法为准)")
	}

	if !hasPersonFact(srv, "ada", "MARK_PEANUT_2") {
		t.Fatalf("ada's panel does not show the corrected entry (旅程「记住事情」: 详情里能看到更正那条):\n%s",
			dumpFacts(srv.Memory("ada")))
	}
}

// hasPersonFact reports a person entry on the panel that contains want.
func hasPersonFact(srv *Server, bot, want string) bool {
	for _, f := range srv.Memory(bot) {
		if f.Scope == "person" && strings.Contains(f.Text, want) {
			return true
		}
	}
	return false
}

// requestBodyOf is the raw body of the last ada request that carries cue: what
// the model was given.
func requestBodyOf(t *testing.T, stub *Stub, cue string) string {
	t.Helper()
	var body string
	for _, r := range stub.Requests() {
		if r.Caller == callerForeground && r.Bot == "ada" && strings.Contains(r.Body, cue) {
			body = r.Body
		}
	}
	if body == "" {
		t.Fatalf("no foreground request for ada carries %q", cue)
	}
	return body
}

func dumpFacts(facts []MemoryFact) string {
	raw, err := json.MarshalIndent(facts, "", "  ")
	if err != nil {
		return ""
	}
	return string(raw)
}
