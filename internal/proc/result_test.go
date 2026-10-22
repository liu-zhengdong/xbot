package proc

import (
	"context"
	"errors"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/exec"
	"xbot/internal/inbox"
	"xbot/internal/kernel"
	"xbot/internal/link"
	"xbot/internal/provider"
)

type resultProvider struct{ fail bool }

func (p resultProvider) Chat(context.Context, provider.Request, func(string)) (provider.Message, error) {
	if p.fail {
		return provider.Message{}, errors.New("model unavailable")
	}
	return provider.Message{Role: provider.RoleAssistant, Content: "子代理结果已转告。"}, nil
}

func TestInboxConsumersRetainUnreportedResults(t *testing.T) {
	for _, consumer := range []string{"Run", "Drain"} {
		t.Run(consumer, func(t *testing.T) {
			b, err := bot.Create(t.TempDir(), "ada", "Be brief.", []string{link.NameTerminal})
			if err != nil {
				t.Fatal(err)
			}
			box, err := inbox.Open(b.InboxPath())
			if err != nil {
				t.Fatal(err)
			}
			ev := link.NewEvent(link.SourceSubagent, exec.EvDone, "结果已完成", "sa_a", time.Now())
			if err := box.Post(ev); err != nil {
				t.Fatal(err)
			}
			k := &kernel.Kernel{Bot: b, Inbox: box, Provider: resultProvider{fail: true}, MaxSteps: 2}
			if consumer == "Drain" {
				err = Drain(context.Background(), k)
			} else {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				err = Run(ctx, Config{K: k, SkipDeliver: true, After: func(link.Event, error) { cancel() }})
			}
			if !errors.Is(err, kernel.ErrPendingResult) {
				t.Fatalf("delivery error=%v", err)
			}
			if err := box.Reload(); err != nil {
				t.Fatal(err)
			}
			if len(box.Pending()) != 1 {
				t.Fatal("consumer acknowledged a result without its final reply")
			}
			// A new processor can retry the same durable event after restart.
			restarted := &kernel.Kernel{Bot: b, Inbox: box, Provider: resultProvider{}, MaxSteps: 2, Desktop: func(context.Context, string, string) error { return nil }}
			if err := Drain(context.Background(), restarted); err != nil {
				t.Fatal(err)
			}
			if len(box.Pending()) != 0 {
				t.Fatal("successful delivery still pending")
			}
		})
	}
}
