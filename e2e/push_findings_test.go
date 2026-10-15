package e2e

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/compact"
	"xbot/internal/kernel"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/mockopenai"
	"xbot/internal/notify"
	"xbot/internal/provider"
)

func TestWebhookReplyBecomesNote(t *testing.T) {
	root := t.TempDir()
	srv := httptest.NewServer(mockopenai.NewMux(t.TempDir()))
	defer srv.Close()
	client := &provider.Client{BaseURL: srv.URL + "/v1", APIKey: "test", Model: "mock", HTTPClient: srv.Client()}
	b := mustBot(t, root, "ada")
	k := findingKernel(b, client)
	ev := link.NewEvent("webhook:orders", "push", "PEANUT_ORDER cake with peanut topping", "hook", time.Now().UTC())
	if err := k.Handle(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	notes, err := notify.Open(b.OutboxPath()).Unread()
	if err != nil || len(notes) != 1 || notes[0].Text != "X" {
		t.Fatalf("outbox %+v %v", notes, err)
	}
	body := notesJSON(t, root)
	if !strings.Contains(body, `"text":"X"`) || strings.Count(body, `"id"`) != 1 {
		t.Fatalf("notes %s", body)
	}
}

func TestWebhookSilentProducesNoNote(t *testing.T) {
	root := t.TempDir()
	srv := httptest.NewServer(mockopenai.NewMux(t.TempDir()))
	defer srv.Close()
	client := &provider.Client{BaseURL: srv.URL + "/v1", APIKey: "test", Model: "mock", HTTPClient: srv.Client()}
	b := mustBot(t, root, "ada")
	k := findingKernel(b, client)
	ev := link.NewEvent("webhook:orders", "push", "WEBHOOK_SILENT nothing to say", "hook", time.Now().UTC())
	if err := k.Handle(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	notes, err := notify.Open(b.OutboxPath()).Unread()
	if err != nil || len(notes) != 0 {
		t.Fatalf("outbox %+v %v", notes, err)
	}
}

func findingKernel(b *bot.Bot, client *provider.Client) *kernel.Kernel {
	return &kernel.Kernel{
		Bot: b, Provider: client, Memory: memory.Open(b.Root), MaxSteps: 4,
		Links:   []link.Link{link.Terminal{}},
		Approve: func(context.Context, string, string) (bool, error) { return false, nil },
		Budget:  compact.DefaultBudget(),
		Desktop: func(context.Context, string, string) error { return nil },
	}
}
