package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
	recent := link.NewEvent("webhook:foo", "message", "<webhook name=\"foo\" count=\"1\" dropped=\"0\">\ndelivery: 123\ntype: push\n<delivery id=\"123\">\nhello webhook\n</delivery>\n</webhook>\n", "hook", now.Add(time.Minute))
	watch := link.NewEvent("watch:abc", "changed", "<watch id=\"abc\" kind=\"changed\" count=\"1\">\npath: /tmp/file\nkind: changed\n</watch>\n", "watch:abc", now.Add(-time.Minute))
	owner := link.WebEvent("u1", "private chat", now.Add(2*time.Minute))
	for _, ev := range []link.Event{old, recent, watch, owner} {
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
	if got.Pending != 2 || len(got.Items) != 3 || got.Items[0].ID != recent.ID || got.Items[1].ID != old.ID || got.Items[1].Status != "acked" || got.Items[1].Summary != "old body" {
		t.Fatalf("unexpected: %+v", got)
	}
	if got.Items[0].Source != "webhook:foo" || strings.Contains(got.Items[0].Summary, "<webhook") || !strings.Contains(got.Items[0].Summary, "hello webhook") {
		t.Fatalf("webhook summary: %+v", got.Items[0])
	}
	if got.Items[2].Summary != "path: /tmp/file\nkind: changed" {
		t.Fatalf("watch summary: %+v", got.Items[2])
	}
	res = get("/api/bots/ada/inbox?limit=1", true)
	defer res.Body.Close()
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 1 || got.Pending != 2 {
		t.Fatalf("limited: %+v", got)
	}
}
