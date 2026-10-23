package kernel

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
)

// The model reads all owner requests in the same conversation.
// It deliberately cannot finish before all five interjections have arrived.
type queuedOwnerModel struct {
	started chan string
	release chan struct{}
	fail    bool
	prefix  []provider.Message
	t       *testing.T
}

func (m *queuedOwnerModel) Chat(ctx context.Context, req provider.Request, _ func(string)) (provider.Message, error) {
	if m.prefix == nil {
		m.prefix = snapshotReq(req).Messages
	} else if len(req.Messages) < len(m.prefix) || !reflect.DeepEqual(m.prefix, req.Messages[:len(m.prefix)]) {
		m.t.Error("cancelled resend rewrote the committed prefix")
	}
	for _, want := range []string{"earlier owner words", "earlier normal answer", "result facts 731 and 42"} {
		if !strings.Contains(messagesText(req.Messages), want) {
			m.t.Errorf("resend lost %q", want)
		}
	}
	seen := map[string]bool{}
	latest := "opening"
	for _, msg := range req.Messages {
		if msg.Role == provider.RoleUser && strings.HasPrefix(msg.Text(), "request-") {
			if seen[msg.Text()] {
				m.t.Errorf("request repeated %q", msg.Text())
			}
			seen[msg.Text()] = true
			latest = msg.Text()
		}
	}
	m.started <- latest
	select {
	case <-ctx.Done():
		return provider.Message{}, ctx.Err()
	case <-m.release:
	}
	// A cancelled scripted request has no completed answer. A real nil-error
	// completion is tested separately at the post-Chat preemption boundary.
	if err := ctx.Err(); err != nil {
		return provider.Message{}, err
	}
	if m.fail {
		return provider.Message{}, fmt.Errorf("model unavailable")
	}
	var replies []string
	for _, msg := range req.Messages {
		if msg.Role == provider.RoleUser && strings.HasPrefix(msg.Text(), "request-") {
			replies = append(replies, "handled "+msg.Text())
		}
	}
	return provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent(strings.Join(replies, "\n"))}, nil
}
func runFiveInterjections(t *testing.T, fail bool) []bot.Entry {
	t.Helper()
	b := testBot(t)
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	for _, msg := range []provider.Message{{Role: provider.RoleUser, Content: provider.TextContent("earlier owner words")}, {Role: provider.RoleAssistant, Content: provider.TextContent("earlier normal answer")}, {Role: provider.RoleUser, Content: provider.TextContent("result facts 731 and 42")}} {
		if err := b.Append(bot.Entry{Time: time.Now(), Origin: bot.OriginOwner, Message: msg}); err != nil {
			t.Fatal(err)
		}
	}
	model := &queuedOwnerModel{t: t, started: make(chan string, 20), release: make(chan struct{}), fail: fail}
	k := &Kernel{Bot: b, Provider: model, Memory: memory.NopStore{}, MaxSteps: 4}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- k.Handle(ctx, link.TerminalEvent("u1", "opening", time.Now())) }()
	waitStarted := func(want string) {
		t.Helper()
		select {
		case got := <-model.started:
			if got != want {
				t.Fatalf("model started %s, want %s", got, want)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	waitStarted("opening")
	for i := 1; i <= 5; i++ {
		if !Interject(b, link.TerminalEvent("u1", fmt.Sprintf("request-%d", i), time.Now()), &Sink{Done: make(chan Outcome, 1)}) {
			t.Fatalf("request-%d was not accepted", i)
		}
		if i == 1 {
			waitStarted("request-1")
		}
	}
	before, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 5; i++ {
		n := 0
		for _, e := range before {
			if e.Message.Text() == fmt.Sprintf("request-%d", i) {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("input %d not durably received before model completion: %d", i, n)
		}
	}
	close(model.release)
	select {
	case err := <-done:
		if (err != nil) != fail {
			t.Fatalf("turn error = %v, fail=%v", err, fail)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func TestFiveInterjectionsEachGetAnAnswer(t *testing.T) {
	entries := runFiveInterjections(t, false)
	for i := 1; i <= 5; i++ {
		want := fmt.Sprintf("handled request-%d", i)
		n := 0
		for _, entry := range entries {
			if entry.Message.Role == provider.RoleAssistant && strings.Contains(entry.Message.Text(), want) {
				n++
			}
		}
		if n != 1 {
			t.Errorf("request-%d has %d responses, want one explicit destination", i, n)
		}
	}
}

func TestQueuedInterjectionsGetAnOutcomeWhenTurnErrors(t *testing.T) {
	entries := runFiveInterjections(t, true)
	for i := 1; i <= 5; i++ {
		text := fmt.Sprintf("request-%d", i)
		seq, users, replies := 0, 0, 0
		for j, e := range entries {
			if e.Message.Role == provider.RoleUser && e.Message.Text() == text {
				seq = j + 1
				users++
			}
		}
		for _, e := range entries {
			if e.Message.Role == provider.RoleAssistant && e.ReplyTo == seq && strings.Contains(e.Message.Text(), "还没处理完") {
				replies++
			}
		}
		if users != 1 || replies != 1 {
			t.Errorf("%s: user rows=%d explicit failure replies=%d", text, users, replies)
		}
	}
}
