package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"xbot/internal/permissions"
)

func TestSettingsPermissionsShareCLIStorage(t *testing.T) {
	root := t.TempDir()
	s := &Server{Root: root, Owner: "owner"}
	token, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	handler := muxOf(s)
	call := func(method, body string, auth bool) (int, string) {
		t.Helper()
		req := httptest.NewRequest(method, "/api/permissions", strings.NewReader(body))
		if auth {
			req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}
	for _, method := range []string{"GET", "PUT"} {
		if code, _ := call(method, `{"confirm":"table"}`, false); code != 401 {
			t.Fatalf("%s auth: %d", method, code)
		}
	}
	assertState := func(want string) {
		t.Helper()
		code, raw := call("GET", "", true)
		var data struct{ Confirm string }
		if json.Unmarshal([]byte(raw), &data) != nil || code != 200 || data.Confirm != want {
			t.Fatalf("GET %d %s, want %s", code, raw, want)
		}
	}
	assertState("off") // missing file is off; GET must not create it.
	if _, err := os.Stat(permissions.Path(root)); !os.IsNotExist(err) {
		t.Fatalf("GET wrote permissions: %v", err)
	}
	for _, body := range []string{`{}`, `{"confirm":"ask"}`, `{"confirm":true}`, `{"confirm":"off","class":"delete"}`, `{"confirm":"off"} {}`} {
		if code, raw := call("PUT", body, true); code != 400 {
			t.Fatalf("bad input %s: %d %s", body, code, raw)
		}
	}
	if code, raw := call("PUT", `{"confirm":"table"}`, true); code != 200 {
		t.Fatalf("enable %d %s", code, raw)
	}
	if !permissions.On(root) {
		t.Fatal("Web toggle not visible to CLI storage")
	}
	assertState("table")
	if err := permissions.Set(root, false); err != nil {
		t.Fatal(err)
	} // same operation as CLI off
	assertState("off")
	if code, _ := call("PUT", `{"confirm":"off"}`, true); code != 200 {
		t.Fatal(code)
	}
	if err := os.WriteFile(permissions.Path(root), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	assertState("off")
	req := httptest.NewRequest("PUT", "/api/permissions", strings.NewReader(`{"confirm":"table"}`))
	req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
	req.Header.Set("Origin", "https://other.example")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("cross-origin %d", rec.Code)
	}
	if permissions.On(root) {
		t.Fatal("rejected mutation changed permissions")
	}
}
