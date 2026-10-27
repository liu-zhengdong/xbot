package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"xbot/internal/inbox"
	"xbot/internal/link"
)

func TestInboxList(t *testing.T) {
	root := t.TempDir()
	s := &Server{Root: root, Owner: "u1"}
	token, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	b := mustWebBot(t, root, "ada", "u1")
	mustWebBot(t, root, "other", "u2")
	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)
	old := link.NewEvent("timer", "timer", link.Wrap(link.NewEvent("timer", "timer", "old body", "timer", now)), "timer", now)
	recent := link.NewEvent("webhook", "message", "new body", "hook", now.Add(time.Minute))
	owner := link.WebEvent("u1", "private chat", now.Add(2*time.Minute))
	for _, ev := range []link.Event{old, recent, owner} {
		if err := box.Post(ev); err != nil {
			t.Fatal(err)
		}
	}
	if err := box.Ack(old.ID); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(muxOf(s))
	defer srv.Close()
	get := func(path string, auth bool) *http.Response {
		t.Helper()
		req, _ := http.NewRequest("GET", srv.URL+path, nil)
		if auth {
			req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
		}
		res, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	for _, tc := range []struct {
		path   string
		auth   bool
		status int
	}{{"/api/bots/ada/inbox", false, 401}, {"/api/bots/other/inbox", true, 404}, {"/api/bots/ada/inbox?limit=101", true, 400}, {"/api/bots/ada/inbox?limit=0", true, 400}} {
		res := get(tc.path, tc.auth)
		res.Body.Close()
		if res.StatusCode != tc.status {
			t.Fatalf("%s: %d", tc.path, res.StatusCode)
		}
	}
	res := get("/api/bots/ada/inbox", true)
	defer res.Body.Close()
	var got struct {
		Pending int         `json:"pending_count"`
		Items   []inboxItem `json:"items"`
	}
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Pending != 1 || len(got.Items) != 2 || got.Items[0].ID != recent.ID || got.Items[1].ID != old.ID || got.Items[1].Status != "acked" || got.Items[1].Summary != "old body" {
		t.Fatalf("unexpected: %+v", got)
	}
	res = get("/api/bots/ada/inbox?limit=1", true)
	defer res.Body.Close()
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 1 || got.Pending != 1 {
		t.Fatalf("limited: %+v", got)
	}
}
