package providerauth

import (
	"io"
	"net/http"
	"net/url"
	"testing"
)

func TestOpenRouterDenialFinishesAttempt(t *testing.T) {
	f := newFakeAuth(t)
	m := &Manager{Root: t.TempDir(), Endpoints: map[string]Endpoints{"openrouter": f.endpoints("openrouter")}}
	defer m.Close()
	start, err := m.Begin("openrouter")
	if err != nil {
		t.Fatal(err)
	}
	auth, _ := url.Parse(start.URL)
	callback, _ := url.Parse(auth.Query().Get("callback_url"))
	q := callback.Query()
	q.Set("error", "access_denied")
	callback.RawQuery = q.Encode()
	// OpenRouter does not echo top-level state on denied authorization.
	res, err := http.Get(callback.String())
	if err != nil {
		t.Fatal("callback transport failed")
	}
	io.Copy(io.Discard, res.Body)
	res.Body.Close()
	if m.Status("openrouter").State != "failed" {
		t.Fatal("denied login remains pending")
	}
}
