package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"xbot/internal/person"
)

// #210: on Windows the person id carries the account SID; the window must show the user name.
func TestSessionOwnerNameNotSID(t *testing.T) {
	cases := []struct{ display, want string }{
		{`RUNNERVM\runneradmin`, "runneradmin"},
		{"刘政东", "刘政东"},
		{"", ""},
		{"S-1-5-21-1-2-3-1001", ""},
	}
	for _, c := range cases {
		root := t.TempDir()
		p, err := person.Identify(root, person.Account{Link: "terminal", ExternalID: "uid:S-1-5-21-1-2-3-1001"}, c.display)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(p.ID, "uS-1-5-21") {
			t.Fatalf("id %q: the fixture needs a SID id", p.ID)
		}
		s := &Server{Root: root, Owner: p.ID}
		token, _, err := s.EnsureToken()
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodGet, "/api/session", nil)
		r.AddCookie(&http.Cookie{Name: cookieName, Value: token})
		w := httptest.NewRecorder()
		muxOf(s).ServeHTTP(w, r)
		var got struct {
			Owner     string `json:"owner"`
			OwnerName string `json:"owner_name"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.Owner != p.ID || got.OwnerName != c.want {
			t.Errorf("display %q: owner %q owner_name %q, want owner_name %q", c.display, got.Owner, got.OwnerName, c.want)
		}
	}
	// A plain id (Linux, macOS) still shows when the display name is empty.
	s := &Server{Root: t.TempDir(), owner: "u501"}
	if got := s.ownerName(); got != "u501" {
		t.Errorf("plain id: %q", got)
	}
	js, err := os.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(js), `textContent = state.owner`) || !strings.Contains(string(js), "state.ownerName = (data && data.owner_name)") {
		t.Error("app.js must show owner_name, not the raw owner id")
	}
}
