//go:build smoke

package smoke

import (
	"strings"
	"testing"
	"time"

	"xbot/internal/peer"
)

// TestBotCollaboration is the smoke case for 「Bot 之间协作」 (journey and spec
// in docs/dev/smoke.md): ada asks bob a question, bob answers, ada brings the
// opinion into its reply to the owner; a runaway loop stops at the hop limit.
//
// Not covered: whether bob's words are treated as data rather than orders. That
// is model behavior and the stub cannot be convinced; this case only shows the
// message arrives as a bot event in ada's conversation.
func TestBotCollaboration(t *testing.T) {
	stub := StartStub(t)

	t.Run("ask bob", func(t *testing.T) {
		srv := Start(t, stub)
		srv.CreateBot("ada", "You are ada, the bot in the collaboration smoke test.")
		srv.CreateBot("bob", "You are bob, another bot in the collaboration smoke test.")

		if r := srv.Send("ada", "SMOKE_ASK_B 问问 B 怎么看"); r.Done != "已经问 B 了" {
			t.Fatalf("SMOKE_ASK_B: done.text %q, want %q", r.Done, "已经问 B 了")
		}

		// Bob's answer comes back as an event and ada passes it on.
		var answer string
		WaitFor(t, 60*time.Second, "ada's final answer with B_OPINION_77", func() (bool, string) {
			rows := srv.Transcript("ada")
			answer = ""
			for _, r := range rows {
				if r.Role == "assistant" && strings.TrimSpace(r.Content) != "" {
					answer = r.Content
				}
			}
			return strings.Contains(answer, "B_OPINION_77"), dumpRows(rows)
		})
		if !strings.Contains(answer, "B 的意见是") {
			t.Fatalf("ada's final answer does not pass on bob's opinion (旅程「Bot 之间协作」: A 的最终回答里包含 B 的意见): %q", answer)
		}

		// One grey event line in the owner's conversation, from bob.
		rows := srv.Transcript("ada")
		if n := countUserRows(rows, `source="bot:bob"`); n != 1 {
			t.Fatalf("ada has %d bot:bob event rows, want exactly 1 (旅程「Bot 之间协作」: 主人的对话里只出现一行灰色事件):\n%s",
				n, dumpRows(rows))
		}

		// The thread page shows both sides.
		thread := srv.PeerMessages("ada", "bob")
		if !messagesContain(thread, "SMOKE_Q") || !messagesContain(thread, "B_OPINION_77") {
			t.Fatalf("the ada/bob thread is missing a side (旅程「Bot 之间协作」: 点开能看到两边的往来): %v", thread)
		}
	})

	t.Run("hop limit", func(t *testing.T) {
		srv := Start(t, stub)
		srv.CreateBot("ada", "You are ada, the bot in the collaboration smoke test.")
		srv.CreateBot("bob", "You are bob, another bot in the collaboration smoke test.")

		// Both bots keep answering each other; the chain must stop at the cap.
		srv.Send("ada", "SMOKE_LOOP")

		time.Sleep(60 * time.Second)
		quietFrom := len(stub.Requests())
		time.Sleep(30 * time.Second)
		if n := len(stub.Requests()); n != quietFrom {
			t.Fatalf("the stub got %d more calls in the last 30 seconds (旅程「Bot 之间协作」: 两个 Bot 不会无限来回):\n%s",
				n-quietFrom, dumpRequests(stub))
		}

		thread := srv.PeerMessages("ada", "bob")
		if len(thread) > peer.MaxHops {
			t.Fatalf("the two bots exchanged %d messages, want at most peer.MaxHops (%d) (旅程「Bot 之间协作」: 到上限会停): %v",
				len(thread), peer.MaxHops, thread)
		}
		if !transcriptsContain(srv, "hop limit") {
			t.Fatalf("no transcript shows the hop limit (旅程「Bot 之间协作」: 到上限会停)")
		}
	})
}

func messagesContain(msgs []PeerMessage, want string) bool {
	for _, m := range msgs {
		if strings.Contains(m.Text, want) {
			return true
		}
	}
	return false
}

// transcriptsContain reports whether either bot's transcript carries want.
func transcriptsContain(srv *Server, want string) bool {
	for _, bot := range []string{"ada", "bob"} {
		for _, r := range srv.Transcript(bot) {
			if strings.Contains(r.Content, want) {
				return true
			}
		}
	}
	return false
}
