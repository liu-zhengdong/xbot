package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"xbot/internal/link"
	"xbot/internal/machine"
	"xbot/internal/permissions"
)

func TestRoutinePrecheckAPIRequiresShellApproval(t *testing.T) {
	s, tok, bots := statusFixture(t, 1)
	b := bots[0]
	tm := &link.Timer{Dir: b.RoutineDir()}
	mux := muxOf(s)
	for _, tc := range []struct {
		name, command, mode string
		on                  bool
	}{
		{"protected", "cat '" + filepath.Join(b.Root, "env") + "'", machine.Allow, false},
		{"ask", "true", machine.Ask, true},
		{"off", "true", machine.Off, true},
		{"delete", "rm -f disposable", machine.Allow, true},
		{"send", "curl -X POST https://example.invalid", machine.Allow, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := permissions.Set(b.Root, tc.on); err != nil {
				t.Fatal(err)
			}
			if err := machine.Set(b.Root, machine.Local, tc.mode); err != nil {
				t.Fatal(err)
			}
			if err := tm.Save(link.Routine{ID: "check", Prompt: "p", Cron: "0 * * * *", Enabled: true}); err != nil {
				t.Fatal(err)
			}
			patch := func(body []byte, want int) {
				t.Helper()
				req := httptest.NewRequest("PATCH", "/api/bots/"+b.Name()+"/routines/check", strings.NewReader(string(body)))
				req.AddCookie(&http.Cookie{Name: cookieName, Value: tok})
				rec := httptest.NewRecorder()
				mux.ServeHTTP(rec, req)
				if rec.Code != want {
					t.Fatalf("code %d, want %d: %s", rec.Code, want, rec.Body.String())
				}
			}
			body, _ := json.Marshal(map[string]string{"precheck": tc.command})
			patch(body, 403)
			r, err := tm.Load("check")
			if err != nil || r.Precheck != "" {
				t.Fatalf("rejected write persisted: %+v %v", r, err)
			}
			// A pre-existing command is reviewed on enable, too; clearing it
			// remains possible without asking permission to run anything.
			r.Precheck, r.Enabled = tc.command, false
			if err := tm.Save(r); err != nil {
				t.Fatal(err)
			}
			patch([]byte(`{"enabled":true}`), 403)
			patch([]byte(`{"precheck":""}`), 200)
			patch([]byte(`{"enabled":true}`), 200)
		})
	}
}

func TestRoutinePrecheckAPI(t *testing.T) {
	s, tok, bots := statusFixture(t, 1)
	b := bots[0]
	tm := &link.Timer{Dir: b.RoutineDir()}
	if err := tm.Save(link.Routine{ID: "check", Prompt: "p", Cron: "0 * * * *", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	mux := muxOf(s)
	for _, tc := range []struct {
		body, want string
		code       int
	}{
		{`{"precheck":"false"}`, "false", 200},
		{`{"enabled":false}`, "false", 200},
		{`{"precheck":true}`, "false", 400},
		{`{"precheck":"` + strings.Repeat("x", (16<<10)+1) + `"}`, "false", 400},
		{`{"precheck":"true","enabled":true}`, "true", 200},
		{`{"precheck":""}`, "", 200},
	} {
		req := httptest.NewRequest("PATCH", "/api/bots/"+b.Name()+"/routines/check", strings.NewReader(tc.body))
		req.AddCookie(&http.Cookie{Name: cookieName, Value: tok})
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != tc.code {
			t.Fatalf("code %d, want %d: %s", rec.Code, tc.code, rec.Body.String())
		}
		r, err := tm.Load("check")
		if err != nil || r.Precheck != tc.want {
			t.Fatalf("precheck %q, want %q: %v", r.Precheck, tc.want, err)
		}
		if tc.code == 200 {
			var response link.Routine
			if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil || response.Precheck != tc.want {
				t.Fatalf("response %s: %v", rec.Body.String(), err)
			}
		}
		req = httptest.NewRequest("GET", "/api/bots/"+b.Name(), nil)
		req.AddCookie(&http.Cookie{Name: cookieName, Value: tok})
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		var detail struct {
			Routines []link.Routine `json:"routines"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &detail); rec.Code != 200 || err != nil || len(detail.Routines) != 1 || detail.Routines[0].Precheck != tc.want {
			t.Fatalf("GET: %d %s %v", rec.Code, rec.Body.String(), err)
		}
	}
}

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
