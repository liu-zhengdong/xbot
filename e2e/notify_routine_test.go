package e2e

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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
	"xbot/internal/web"
)

func TestRoutineLowImportanceReachesNotes(t *testing.T) {
	root := t.TempDir()
	srv := httptest.NewServer(mockopenai.NewMux(t.TempDir()))
	defer srv.Close()
	client := &provider.Client{BaseURL: srv.URL + "/v1", APIKey: "test", Model: "mock", HTTPClient: srv.Client()}
	now := time.Date(2026, 10, 15, 8, 7, 0, 0, time.UTC)

	b := mustBot(t, root, "ada")
	if err := (&link.Timer{Dir: b.RoutineDir(), Sender: "u1"}).Save(link.Routine{
		ID: "water", Name: "Water", Prompt: "DRINK_WATER remind the owner", Cron: "7 * * * *", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := passAt(root, now, client); err != nil {
		t.Fatal(err)
	}
	notes, err := notify.Open(b.OutboxPath()).Unread()
	if err != nil || len(notes) != 1 || notes[0].Text != "drink water" {
		t.Fatalf("outbox %+v %v", notes, err)
	}
	body := notesJSON(t, root)
	if !strings.Contains(body, "drink water") || strings.Count(body, `"id"`) != 1 {
		t.Fatalf("notes %s", body)
	}
}

func TestRoutineLowImportanceHeldThenReleased(t *testing.T) {
	root := t.TempDir()
	srv := httptest.NewServer(mockopenai.NewMux(t.TempDir()))
	defer srv.Close()
	client := &provider.Client{BaseURL: srv.URL + "/v1", APIKey: "test", Model: "mock", HTTPClient: srv.Client()}
	b := mustBot(t, root, "ada")
	start, end, utc := "22:00", "07:00", "UTC"
	if err := b.ApplyProfile(bot.ProfileUpdate{QuietStart: &start, QuietEnd: &end, Timezone: &utc}); err != nil {
		t.Fatal(err)
	}
	if err := (&link.Timer{Dir: b.RoutineDir(), Sender: "u1"}).Save(link.Routine{
		ID: "water", Name: "Water", Prompt: "DRINK_WATER remind the owner", Cron: "7 23 * * *", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	night := time.Date(2026, 10, 15, 23, 7, 0, 0, time.UTC)
	if err := passAt(root, night, client); err != nil {
		t.Fatal(err)
	}
	unread, err := notify.Open(b.OutboxPath()).Unread()
	if err != nil || len(unread) != 0 {
		t.Fatalf("outbox during quiet hours %+v %v", unread, err)
	}
	heldRaw, err := os.ReadFile(b.HeldPath())
	held := string(heldRaw)
	if err != nil || !strings.Contains(held, "drink water") {
		t.Fatalf("held %s %v", held, err)
	}
	morning := time.Date(2026, 10, 16, 8, 0, 0, 0, time.UTC)
	if err := passAt(root, morning, client); err != nil {
		t.Fatal(err)
	}
	unread, err = notify.Open(b.OutboxPath()).Unread()
	if err != nil || len(unread) != 1 || !strings.Contains(unread[0].Text, "drink water") {
		t.Fatalf("released %+v %v", unread, err)
	}
	body := notesJSON(t, root)
	if !strings.Contains(body, "drink water") {
		t.Fatalf("notes %s", body)
	}
}

func passAt(root string, now time.Time, client *provider.Client) error {
	return serve.Pass(context.Background(), root, now, func(b *bot.Bot) (*kernel.Kernel, error) {
		box, err := inbox.Open(b.InboxPath())
		if err != nil {
			return nil, err
		}
		return &kernel.Kernel{
			Bot: b, Provider: client, Memory: memory.Open(b.Root), MaxSteps: 4,
			Now:   func() time.Time { return now },
			Inbox: box,
			Links: []link.Link{
				link.Terminal{},
				&link.Timer{Dir: b.RoutineDir(), Sender: "u1"},
			},
			Approve: func(context.Context, string, string) (bool, error) { return false, nil },
			Budget:  compact.DefaultBudget(),
		}, nil
	})
}

func notesJSON(t *testing.T, root string) string {
	t.Helper()
	s := &web.Server{Root: root, Owner: "u1"}
	tok, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(func() http.Handler {
		mux := http.NewServeMux()
		s.Register(mux)
		return mux
	}())
	defer srv.Close()
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/login", strings.NewReader(`{"token":"`+tok+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("login %d", res.StatusCode)
	}
	req, err = http.NewRequest(http.MethodGet, srv.URL+"/api/notes", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range res.Cookies() {
		req.AddCookie(c)
	}
	res, err = srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 {
		t.Fatalf("notes %d %s", res.StatusCode, raw)
	}
	var payload struct {
		Notes []map[string]any `json:"notes"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Notes) == 0 {
		t.Fatalf("empty notes %s", raw)
	}
	return string(raw)
}
