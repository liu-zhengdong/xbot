package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/notify"
)

func TestNotificationUnreadMessagesAndSession(t *testing.T) {
	s := &Server{Root: t.TempDir(), Owner: "owner"}
	token, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := bot.Create(s.Root, "ada", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"owner"}); err != nil {
		t.Fatal(err)
	}
	if _, err := notify.Open(b.OutboxPath()).Append("unread notification", time.Now()); err != nil {
		t.Fatal(err)
	}
	mux := muxOf(s)
	request := func(path string, authed bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		if authed {
			r.AddCookie(&http.Cookie{Name: cookieName, Value: token})
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	w := request("/api/bots", true)
	var list struct {
		Bots []botItem `json:"bots"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &list) != nil || len(list.Bots) != 1 {
		t.Fatalf("unexpected bot list: %d %s", w.Code, w.Body.String())
	}
	if list.Bots[0].Unread || list.Bots[0].Notes != 1 {
		t.Fatalf("notifications must not light message unread dot: %+v", list.Bots[0])
	}
	for _, authed := range []bool{false, true} {
		w = request("/api/session", authed)
		var session struct {
			Authed bool   `json:"authed"`
			Owner  string `json:"owner"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &session) != nil || session.Authed != authed || (authed && session.Owner != "owner") {
			t.Fatalf("unexpected session: %d %s", w.Code, w.Body.String())
		}
	}
	if w := request("/api/bots", false); w.Code != http.StatusUnauthorized {
		t.Fatalf("protected endpoint returned %d", w.Code)
	}
}
