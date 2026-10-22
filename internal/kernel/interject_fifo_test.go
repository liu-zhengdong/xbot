package kernel

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
)

// Like the real foreground, this model acts on the newest owner message.
// It deliberately cannot finish before all five interjections have arrived.
type queuedOwnerModel struct {
	started chan string
	release chan struct{}
	fail    bool
}

func (m *queuedOwnerModel) Chat(ctx context.Context, req provider.Request, _ func(string)) (provider.Message, error) {
	latest := "opening"
	for _, msg := range req.Messages {
		if msg.Role == provider.RoleUser && strings.HasPrefix(msg.Content, "request-") {
			latest = msg.Content
		}
	}
	m.started <- latest
	select {
	case <-ctx.Done():
		return provider.Message{}, ctx.Err()
	case <-m.release:
	}
	if m.fail {
		return provider.Message{}, fmt.Errorf("model unavailable")
	}
	return provider.Message{Role: provider.RoleAssistant, Content: "handled " + latest}, nil
}
func runFiveInterjections(t *testing.T, fail bool) []bot.Entry {
	t.Helper()
	b := testBot(t)
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	model := &queuedOwnerModel{started: make(chan string, 20), release: make(chan struct{}), fail: fail}
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
			if entry.Message.Role == provider.RoleAssistant && entry.Message.Content == want {
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
			if e.Message.Role == provider.RoleUser && e.Message.Content == text {
				seq = j + 1
				users++
			}
		}
		for _, e := range entries {
			if e.Message.Role == provider.RoleAssistant && e.ReplyTo == seq && strings.Contains(e.Message.Content, "还没处理完") {
				replies++
			}
		}
		if users != 1 || replies != 1 {
			t.Errorf("%s: user rows=%d explicit failure replies=%d", text, users, replies)
		}
	}
}
