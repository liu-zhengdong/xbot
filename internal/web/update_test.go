package web

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"xbot/internal/upgrade"
)

// The update endpoints need a session like every other API call.
func TestUpdateAPIRequiresAuth(t *testing.T) {
	s := &Server{Root: t.TempDir(), Owner: "u1", Updater: upgrade.NewUpdater(t.TempDir())}
	srv := httptest.NewServer(muxOf(s))
	defer srv.Close()
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/api/update"},
		{http.MethodPost, "/api/update/install"},
	} {
		req, _ := http.NewRequest(c.method, srv.URL+c.path, nil)
		res, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s %s = %d, want 401", c.method, c.path, res.StatusCode)
		}
	}
}

// With nothing downloaded, installing is refused with a reason the page can
// show, rather than a crash or a silent restart.
func TestUpdateInstallWithoutADownload(t *testing.T) {
	root := t.TempDir()
	s := &Server{Root: root, Owner: "u1", Updater: upgrade.NewUpdater(t.TempDir())}
	tok, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(muxOf(s))
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/update/install", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: tok})
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", res.StatusCode)
	}
	var payload map[string]string
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload["error"] == "" {
		t.Fatal("no reason given")
	}
}

// A process without an updater says so instead of pretending to be current.
func TestUpdateStatusWithoutAnUpdater(t *testing.T) {
	s := &Server{Root: t.TempDir(), Owner: "u1"}
	tok, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(muxOf(s))
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/update", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: tok})
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
	body, _ := io.ReadAll(res.Body)
	var payload map[string]string
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["state"] != "idle" {
		t.Fatalf("state = %q, want idle", payload["state"])
	}
}

// The status the page reads carries the version it is running, so the pill can
// name the new one.
func TestUpdateStatusCarriesVersions(t *testing.T) {
	s := &Server{Root: t.TempDir(), Owner: "u1", Updater: upgrade.NewUpdater(t.TempDir())}
	tok, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(muxOf(s))
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/update", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: tok})
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var st upgrade.Status
	if err := json.NewDecoder(res.Body).Decode(&st); err != nil {
		t.Fatal(err)
	}
	if st.Current == "" {
		t.Fatal("the status does not say which version is running")
	}
}
