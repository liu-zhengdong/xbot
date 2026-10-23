package e2e

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/compact"
	"xbot/internal/kernel"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/mockopenai"
	"xbot/internal/provider"
)

func TestMultiStepAgainstMock(t *testing.T) {
	work := t.TempDir()
	if err := os.WriteFile(filepath.Join(work, "a.txt"), []byte("alpha\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "b.txt"), []byte("beta\n"), 0644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(mockopenai.NewMux(work))
	defer srv.Close()

	root := t.TempDir()
	b, err := bot.Create(root, "demo", "You are a test bot.", []string{link.NameTerminal, link.NameTools})
	if err != nil {
		t.Fatal(err)
	}
	client := &provider.Client{
		BaseURL:    srv.URL + "/v1",
		APIKey:     "test",
		Model:      "mock",
		HTTPClient: srv.Client(),
	}
	k := &kernel.Kernel{
		Bot:       b,
		Provider:  client,
		Links:     []link.Link{link.Terminal{}, link.NewToolsLink()},
		Compactor: compact.LLM{P: client},
		Approve:   func(context.Context, string, string) (bool, error) { return true, nil },
		Memory:    memory.NopStore{},
		MaxSteps:  6,
		Budget:    compact.DefaultBudget(),
	}
	ev := link.TerminalEvent("u1", "List this directory and write a summary to summary.txt", time.Now())
	if err := k.Handle(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(work, "summary.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "a.txt") || !strings.Contains(string(got), "b.txt") {
		t.Fatalf("summary:\n%s", got)
	}
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	assistants := 0
	sawFinal := false
	for _, e := range entries {
		if e.Message.Role == provider.RoleAssistant {
			assistants++
			if strings.Contains(e.Message.Text(), "Summary written") {
				sawFinal = true
			}
		}
	}
	if assistants < 2 || !sawFinal {
		t.Fatalf("expected a multi-step transcript, assistants=%d final=%v entries=%d", assistants, sawFinal, len(entries))
	}
	again, err := bot.Load(root, "demo")
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := again.Entries()
	if err != nil || len(loaded) != len(entries) {
		t.Fatalf("reload %d/%d err=%v", len(loaded), len(entries), err)
	}
}
