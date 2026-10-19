package kernel

import (
	"context"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
)

// A line typed on another paired device reaches the model as the owner's own
// words, unwrapped, and ReplyAfter returns the answer once the turn is done.
func TestRemoteOwnerLineIsTrusted(t *testing.T) {
	b := testBot(t, link.NameRemoteOwner)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(b.SetOwners([]string{"u1"}))
	p := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: "answered"}}}
	k := &Kernel{Bot: b, Provider: p, Memory: memory.NopStore{}, MaxSteps: 4}
	ev := link.RemoteOwnerEvent("u1", "please check the build", time.Unix(10, 0).UTC())
	must(k.Handle(context.Background(), ev))
	if len(p.reqs) != 1 {
		t.Fatalf("provider calls %d", len(p.reqs))
	}
	var raw bool
	for _, m := range p.reqs[0].Messages {
		if m.Role == provider.RoleUser && m.Content == "please check the build" {
			raw = true
		}
	}
	if !raw {
		t.Fatal("the remote owner line was not the owner's own words")
	}
	text, done, err := ReplyAfter(b, ev)
	if err != nil || !done || text != "answered" {
		t.Fatalf("text=%q done=%v err=%v", text, done, err)
	}
}

func TestReplyAfter(t *testing.T) {
	b := testBot(t, link.NameRemoteOwner)
	ev := link.RemoteOwnerEvent("u1", "how is the build?", time.Unix(10, 0).UTC())
	if err := b.Append(bot.Entry{Time: ev.Time, EventID: ev.ID, Message: provider.Message{Role: provider.RoleUser, Content: ev.Content}}); err != nil {
		t.Fatal(err)
	}
	// A turn that started but has only tool work so far is not done.
	if err := b.Append(bot.Entry{Message: provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c1"}}}}); err != nil {
		t.Fatal(err)
	}
	if _, done, err := ReplyAfter(b, ev); err != nil || done {
		t.Fatalf("a turn with an open tool call is not done: done=%v err=%v", done, err)
	}
	if err := b.Append(bot.Entry{Message: provider.Message{Role: provider.RoleTool, ToolCallID: "c1", Content: "ok"}}); err != nil {
		t.Fatal(err)
	}
	if _, done, err := ReplyAfter(b, ev); err != nil || done {
		t.Fatalf("a turn without a final line is not done: done=%v err=%v", done, err)
	}
	if err := b.Append(bot.Entry{Message: provider.Message{Role: provider.RoleAssistant, Content: "green"}}); err != nil {
		t.Fatal(err)
	}
	// A later turn belongs to someone else and does not change this answer.
	if err := b.Append(bot.Entry{EventID: "other", Message: provider.Message{Role: provider.RoleUser, Content: "hi"}}); err != nil {
		t.Fatal(err)
	}
	text, done, err := ReplyAfter(b, ev)
	if err != nil || !done || text != "green" {
		t.Fatalf("text=%q done=%v err=%v", text, done, err)
	}
	if _, done, err := ReplyAfter(b, link.RemoteOwnerEvent("u1", "never seen", ev.Time)); err != nil || done {
		t.Fatalf("unknown event: done=%v err=%v", done, err)
	}
}

// A line that arrived while another turn ran is absorbed into it without an
// event id; ReplyAfter finds it by text and returns that turn's final answer.
func TestReplyAfterAbsorbed(t *testing.T) {
	b := testBot(t, link.NameRemoteOwner)
	at := time.Unix(100, 0).UTC()
	ev := link.RemoteOwnerEvent("u1", "also check the docs", at)
	for _, e := range []bot.Entry{
		{Time: at, EventID: "first", Message: provider.Message{Role: provider.RoleUser, Content: "check the build"}},
		{Time: at, Message: provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c1"}}}},
		{Time: at, Message: provider.Message{Role: provider.RoleTool, ToolCallID: "c1", Content: "ok"}},
		{Time: at.Add(time.Second), Message: provider.Message{Role: provider.RoleUser, Content: link.Render(ev, true)}},
	} {
		if err := b.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	if _, done, err := ReplyAfter(b, ev); err != nil || done {
		t.Fatalf("the running turn is not done: done=%v err=%v", done, err)
	}
	if err := b.Append(bot.Entry{Time: at.Add(2 * time.Second), Message: provider.Message{Role: provider.RoleAssistant, Content: "build and docs are fine"}}); err != nil {
		t.Fatal(err)
	}
	text, done, err := ReplyAfter(b, ev)
	if err != nil || !done || text != "build and docs are fine" {
		t.Fatalf("text=%q done=%v err=%v", text, done, err)
	}
	// The same text from before the line arrived does not count.
	early := link.RemoteOwnerEvent("u1", "also check the docs", at.Add(time.Hour))
	if _, done, err := ReplyAfter(b, early); err != nil || done {
		t.Fatalf("an older identical line matched: done=%v err=%v", done, err)
	}
}

// A turn that forks into the background answers on the branch, and the answer
// lands after the owner's next line. ReplyAfter used to stop reading at that
// line, so a finished background task still looked unfinished and the remote
// reply was dropped after 24 hours.
func TestReplyAfterReadsAnswerAfterFork(t *testing.T) {
	b := testBot(t, link.NameRemoteOwner)
	ev := link.RemoteOwnerEvent("u1", "run the long build", time.Unix(10, 0).UTC())
	if err := b.Append(bot.Entry{Time: ev.Time, EventID: ev.ID, Message: provider.Message{Role: provider.RoleUser, Content: ev.Content}}); err != nil {
		t.Fatal(err)
	}
	branch := "bg_test1"
	if _, err := b.AppendFork(branch, 1, "run the long build"); err != nil {
		t.Fatal(err)
	}
	// The owner speaks again while the branch keeps running.
	next := link.RemoteOwnerEvent("u1", "any progress?", time.Unix(20, 0).UTC())
	if err := b.Append(bot.Entry{Time: next.Time, EventID: next.ID, Message: provider.Message{Role: provider.RoleUser, Content: next.Content}}); err != nil {
		t.Fatal(err)
	}
	if err := b.Append(bot.Entry{Branch: branch, Message: provider.Message{Role: provider.RoleAssistant, Content: "the build passed"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.AppendForkEnd(branch, bot.ForkDone, 1); err != nil {
		t.Fatal(err)
	}
	text, done, err := ReplyAfter(b, ev)
	if err != nil {
		t.Fatal(err)
	}
	if !done {
		t.Fatal("a forked turn that answered on its branch is still reported unfinished")
	}
	if text != "the build passed" {
		t.Fatalf("text %q, want the branch's answer", text)
	}
}

// A branch that closed without a final line never answers, so ReplyAfter keeps
// reporting unfinished instead of inventing an empty reply.
func TestReplyAfterForkWithoutAnswerStaysUnfinished(t *testing.T) {
	b := testBot(t, link.NameRemoteOwner)
	ev := link.RemoteOwnerEvent("u1", "run the long build", time.Unix(10, 0).UTC())
	if err := b.Append(bot.Entry{Time: ev.Time, EventID: ev.ID, Message: provider.Message{Role: provider.RoleUser, Content: ev.Content}}); err != nil {
		t.Fatal(err)
	}
	branch := "bg_test2"
	if _, err := b.AppendFork(branch, 1, "run the long build"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.AppendForkEnd(branch, bot.ForkFailed, 1); err != nil {
		t.Fatal(err)
	}
	if _, done, err := ReplyAfter(b, ev); err != nil || done {
		t.Fatalf("done=%v err=%v, want still unfinished", done, err)
	}
}

// Request A never answered, and the next request B forked and finished. A must
// not inherit B's branch: that would report A as answered with B's text and
// make a replay of A skip it.
func TestReplyAfterDoesNotBorrowNextTurnsBranch(t *testing.T) {
	b := testBot(t, link.NameRemoteOwner)
	first := link.RemoteOwnerEvent("u1", "first request", time.Unix(10, 0).UTC())
	if err := b.Append(bot.Entry{Time: first.Time, EventID: first.ID, Message: provider.Message{Role: provider.RoleUser, Content: first.Content}}); err != nil {
		t.Fatal(err)
	}
	// A produced no final line (for example the model errored out).
	second := link.RemoteOwnerEvent("u1", "second request", time.Unix(20, 0).UTC())
	if err := b.Append(bot.Entry{Time: second.Time, EventID: second.ID, Message: provider.Message{Role: provider.RoleUser, Content: second.Content}}); err != nil {
		t.Fatal(err)
	}
	branch := "bg_test3"
	if _, err := b.AppendFork(branch, 2, "second request"); err != nil {
		t.Fatal(err)
	}
	if err := b.Append(bot.Entry{Branch: branch, Message: provider.Message{Role: provider.RoleAssistant, Content: "second answer"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.AppendForkEnd(branch, bot.ForkDone, 1); err != nil {
		t.Fatal(err)
	}
	text, done, err := ReplyAfter(b, first)
	if err != nil {
		t.Fatal(err)
	}
	if done {
		t.Fatalf("the unanswered first request borrowed the next turn's branch: text %q", text)
	}
}

// A forked turn that never forked... the branch must belong to its own turn,
// so its answer is still read when the branch starts inside the turn's span.
func TestReplyAfterForkInsideOwnTurn(t *testing.T) {
	b := testBot(t, link.NameRemoteOwner)
	ev := link.RemoteOwnerEvent("u1", "run it", time.Unix(10, 0).UTC())
	if err := b.Append(bot.Entry{Time: ev.Time, EventID: ev.ID, Message: provider.Message{Role: provider.RoleUser, Content: ev.Content}}); err != nil {
		t.Fatal(err)
	}
	branch := "bg_test4"
	if _, err := b.AppendFork(branch, 1, "run it"); err != nil {
		t.Fatal(err)
	}
	if err := b.Append(bot.Entry{Branch: branch, Message: provider.Message{Role: provider.RoleAssistant, Content: "done on the branch"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.AppendForkEnd(branch, bot.ForkDone, 1); err != nil {
		t.Fatal(err)
	}
	text, done, err := ReplyAfter(b, ev)
	if err != nil || !done || text != "done on the branch" {
		t.Fatalf("text=%q done=%v err=%v", text, done, err)
	}
}
