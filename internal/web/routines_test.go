package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xbot/internal/link"
)

func TestRoutineSwitchUsesTimerUpdate(t *testing.T) {
	s, tok, bots := statusFixture(t, 1)
	b := bots[0]
	timer := &link.Timer{Dir: b.RoutineDir()}
	if err := timer.Save(link.Routine{ID: "morning", Name: "早报", Prompt: "看看新闻", Cron: "0 8 * * *", Zone: "local", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	mux := muxOf(s)
	for _, tc := range []struct {
		body, id string
		auth     bool
		code     int
	}{
		{`{"enabled":false}`, "morning", false, 401},
		{`{"enabled":false}`, "missing", true, 404},
		{`{}`, "morning", true, 400},
		{`{"enabled":"yes"}`, "morning", true, 400},
		{`{"enabled":false,"prompt":"changed"}`, "morning", true, 400},
		{`{"enabled":false}`, "morning", true, 200},
		{`{"enabled":true}`, "morning", true, 200},
	} {
		req := httptest.NewRequest("PATCH", "/api/bots/"+b.Name()+"/routines/"+tc.id, strings.NewReader(tc.body))
		if tc.auth {
			req.AddCookie(&http.Cookie{Name: cookieName, Value: tok})
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != tc.code {
			t.Fatalf("%s %s: %d %s", tc.id, tc.body, rec.Code, rec.Body.String())
		}
		rt, err := timer.Load("morning")
		if err != nil {
			t.Fatal(err)
		}
		if rt.Prompt != "看看新闻" || rt.Cron != "0 8 * * *" {
			t.Fatal("switch changed the routine body")
		}
		if tc.code == 200 && rt.Enabled != strings.Contains(tc.body, "true") {
			t.Fatal("enabled did not persist")
		}
	}
}
