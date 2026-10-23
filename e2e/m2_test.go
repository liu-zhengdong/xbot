package e2e

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/compact"
	"xbot/internal/inbox"
	"xbot/internal/kernel"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/mockopenai"
	"xbot/internal/notify"
	"xbot/internal/provider"
	"xbot/internal/serve"
)

func TestPersonFactCrossesBots(t *testing.T) {
	work := t.TempDir()
	srv := httptest.NewServer(mockopenai.NewMux(work))
	defer srv.Close()
	root := t.TempDir()
	ada, err := bot.Create(root, "ada", "You are Ada.", []string{link.NameTerminal, link.NameMemory, link.NameTools})
	if err != nil {
		t.Fatal(err)
	}
	if err := ada.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	store := memory.Open(root)
	client := &provider.Client{BaseURL: srv.URL + "/v1", APIKey: "test", Model: "mock", HTTPClient: srv.Client()}
	k := &kernel.Kernel{
		Bot: ada, Provider: client, Memory: store, MaxSteps: 4,
		Links:   []link.Link{link.Terminal{}, link.Memory{}, link.NewToolsLinkAt(root, "")},
		Approve: func(context.Context, string, string) (bool, error) { return true, nil },
		Budget:  compact.DefaultBudget(),
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "REMEMBER_PERSON", time.Now())); err != nil {
		t.Fatal(err)
	}
	entries, err := ada.Entries()
	if err != nil {
		t.Fatal(err)
	}
	var ownerRaw bool
	for _, e := range entries {
		if strings.Contains(e.Message.Text(), "REMEMBER_PERSON") && !strings.Contains(e.Message.Text(), "<untrusted_event") {
			ownerRaw = true
		}
	}
	if !ownerRaw {
		t.Fatal("owner terminal input was wrapped")
	}
	personFacts, err := store.Recall(context.Background(), memory.Query{Scope: memory.ScopePerson, PersonID: "u1", Tier: memory.TierProfile})
	if err != nil || len(personFacts) != 1 || personFacts[0].Text != "Ada likes tea" || personFacts[0].WrittenBy != "ada" {
		t.Fatalf("person %+v %v", personFacts, err)
	}
	own, err := store.Recall(context.Background(), memory.Query{Scope: memory.ScopeBot, BotName: "ada", Tier: memory.TierProfile})
	if err != nil || len(own) != 1 || own[0].Text != "secret handshake" {
		t.Fatalf("ada bot %+v %v", own, err)
	}
	bea, err := bot.Create(root, "bea", "You are Bea.", []string{link.NameTerminal, link.NameMemory})
	if err != nil {
		t.Fatal(err)
	}
	kb := &kernel.Kernel{
		Bot: bea, Provider: client, Memory: store, MaxSteps: 4,
		Links:   []link.Link{link.Terminal{}, link.Memory{}},
		Approve: func(context.Context, string, string) (bool, error) { return true, nil },
		Budget:  compact.DefaultBudget(),
	}
	if err := kb.Handle(context.Background(), link.TerminalEvent("u1", "RECALL_PERSON", time.Now())); err != nil {
		t.Fatal(err)
	}
	bent, err := bea.Entries()
	if err != nil {
		t.Fatal(err)
	}
	joined := ""
	for _, e := range bent {
		joined += e.Message.Text() + "\n"
	}
	if !strings.Contains(joined, "Ada likes tea") || strings.Contains(joined, "secret handshake") {
		t.Fatalf("bot B context:\n%s", joined)
	}
}

func TestServeRoutineNotifyAndSilence(t *testing.T) {
	root := t.TempDir()
	srv := httptest.NewServer(mockopenai.NewMux(t.TempDir()))
	defer srv.Close()
	client := &provider.Client{BaseURL: srv.URL + "/v1", APIKey: "test", Model: "mock", HTTPClient: srv.Client()}
	now := time.Date(2026, 10, 15, 8, 7, 0, 0, time.UTC)

	loud := mustBot(t, root, "loud")
	quiet := mustBot(t, root, "quiet")
	if err := (&link.Timer{Dir: loud.RoutineDir(), Sender: "u1"}).Save(link.Routine{
		ID: "mail", Name: "Mail", Prompt: "NOTIFY the owner", Cron: "7 * * * *", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := (&link.Timer{Dir: quiet.RoutineDir(), Sender: "u1"}).Save(link.Routine{
		ID: "nap", Name: "Nap", Prompt: "QUIET nothing to say", Cron: "7 * * * *", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	err := serve.Pass(context.Background(), root, now, func(b *bot.Bot) (*kernel.Kernel, error) {
		k := routineKernel(b, client)
		box, err := inbox.Open(b.InboxPath())
		if err != nil {
			return nil, err
		}
		k.Inbox = box
		return k, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	notes, err := notify.Open(loud.OutboxPath()).Unread()
	if err != nil || len(notes) != 1 || notes[0].Text != "mail is waiting" {
		t.Fatalf("loud %+v %v", notes, err)
	}
	silent, err := notify.Open(quiet.OutboxPath()).Unread()
	if err != nil || len(silent) != 0 {
		t.Fatalf("quiet %+v %v", silent, err)
	}
	qent, err := quiet.Entries()
	if err != nil {
		t.Fatal(err)
	}
	var sawSilent bool
	for _, e := range qent {
		if strings.TrimSpace(e.Message.Text()) == "SILENT" {
			sawSilent = true
		}
		if strings.Contains(e.Message.Text(), "<routine>") && !strings.Contains(e.Message.Text(), "<untrusted_event") {
			t.Fatal("routine wake was trusted")
		}
	}
	if !sawSilent {
		t.Fatal("quiet routine did not answer SILENT")
	}
}

func mustBot(t *testing.T, root, name string) *bot.Bot {
	t.Helper()
	b, err := bot.Create(root, name, "You are a test bot.", []string{link.NameTerminal, link.NameTimer, link.NameMemory})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	return b
}

func routineKernel(b *bot.Bot, client *provider.Client) *kernel.Kernel {
	return &kernel.Kernel{
		Bot: b, Provider: client, Memory: memory.Open(b.Root), MaxSteps: 4,
		Links: []link.Link{
			link.Terminal{},
			&link.Timer{Dir: b.RoutineDir(), Sender: "u1"},
		},
		Approve: func(context.Context, string, string) (bool, error) { return false, nil },
		Budget:  compact.DefaultBudget(),
		Desktop: func(context.Context, string, string) error { return nil },
	}
}
