package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// The pipe authorizes many requests at once while the local page reads the
// session; AuthorizeOwner must only read the token loaded at startup
// (review1 P1-2: it used to rerun EnsureToken and race on owner/token).
func TestAuthorizeOwnerConcurrent(t *testing.T) {
	s := &Server{Root: t.TempDir(), Owner: "u1"}
	if err := s.AuthorizeOwner(httptest.NewRequest(http.MethodGet, "/api/session", nil)); err == nil {
		t.Fatal("authorized before the token was loaded")
	}
	if _, _, err := s.EnsureToken(); err != nil {
		t.Fatal(err)
	}
	mux := muxOf(s)
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				r := httptest.NewRequest(http.MethodGet, "/api/session", nil)
				if err := s.AuthorizeOwner(r); err != nil {
					t.Error(err)
					return
				}
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, r)
				if !strings.Contains(w.Body.String(), `"authed":true`) {
					t.Errorf("session: %d %s", w.Code, w.Body)
					return
				}
				// The local page, unauthenticated, at the same time.
				w = httptest.NewRecorder()
				mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/session", nil))
			}
		}()
	}
	wg.Wait()
}
