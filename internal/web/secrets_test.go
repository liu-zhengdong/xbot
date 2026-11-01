package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/vault"
)

type secretRoundTrip func(*http.Request) (*http.Response, error)

func (f secretRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestGlobalAPIAndNeedsSecretCrossBotConsumers(t *testing.T) {
	t.Setenv("XBOT_VAULT_SOCKET", "")
	e := newAppsEnv(t)
	for _, payload := range []string{
		`{"name":"ARBITRARY_KEY","value":"fixture-api-egress","domains":["example.com"]}`,
		`{"name":"LOCAL_SUDO","value":"fixture-api-sudo","domains":[]}`,
	} {
		if code, _ := e.call("POST", "/api/secrets", payload, true); code != 200 {
			t.Fatal("global API write failed")
		}
	}
	for _, payload := range []string{`{"id":"telegram","secrets":["fixture-needssecret"]}`, `{"id":"context7","secrets":["fixture-mcp-needssecret"]}`} {
		if code, _ := e.call("POST", "/api/apps", payload, true); code != 200 {
			t.Fatal("NeedsSecret install failed")
		}
	}
	for _, name := range []string{"ada", "bob"} {
		b, err := bot.Load(e.root, name)
		if err != nil {
			t.Fatal(err)
		}
		backend, err := vault.OpenGlobal(b.Root)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range []struct{ name, value, host string }{{"ARBITRARY_KEY", "fixture-api-egress", "example.com"}, {"telegram.token", "fixture-needssecret", "api.telegram.org"}} {
			req := httptest.NewRequest("GET", "https://"+entry.host, nil)
			req.Header.Set("Authorization", "{{secret:"+entry.name+"}}")
			called := false
			base := secretRoundTrip(func(r *http.Request) (*http.Response, error) {
				called = true
				if r.Header.Get("Authorization") != entry.value {
					t.Error("Bot did not inject shared credential")
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: http.NoBody}, nil
			})
			if _, err := backend.Transport(base).RoundTrip(req); err != nil {
				t.Fatal(err)
			}
			if !called {
				t.Fatal("outbound consumer not reached")
			}
		}
		var out bytes.Buffer
		if err := vault.WriteLocalSudo(b.VaultDir(), b.VaultKeyPath(), &out); err != nil || out.String() != "fixture-api-sudo\n" {
			t.Fatal("Bot askpass did not read API value")
		}
		if value, ok := link.GlobalSecretLookup(b.Root)("CONTEXT7_API_KEY"); !ok || value != "fixture-mcp-needssecret" {
			t.Fatal("MCP did not read NeedsSecret global value")
		}
	}
	if code, raw := e.call("GET", "/api/secrets", "", true); code != 200 || bytes.Contains(raw, []byte("fixture")) || bytes.Contains(raw, []byte("value")) {
		t.Fatal("metadata leaked")
	}
}

func TestGlobalSecretsCRUDMetadataOnly(t *testing.T) {
	t.Setenv("XBOT_VAULT_SOCKET", "")
	e := newAppsEnv(t)
	path := "/api/secrets"
	for _, call := range []struct {
		method, path, body string
		code               int
	}{
		{"GET", path, "", 200},
		{"POST", path, `{"name":"LOCAL_SUDO","value":"fixture-local-password","domains":[]}`, 200},
		{"GET", path, "", 200},
		{"POST", path, `{"name":"","value":"fixture"}`, 400},
		{"POST", path, `{"name":"EMPTY","value":""}`, 400},
		{"POST", path, `{"name":"BAD","value":"fixture","unknown":true}`, 400},
		{"GET", "/api/bots/bob/secrets", "", 404},
	} {
		code, raw := e.call(call.method, call.path, call.body, true)
		if code != call.code {
			t.Fatalf("%s: %d %s", call.method, code, raw)
		}
		if bytes.Contains(bytes.ToLower(raw), []byte("value")) || bytes.Contains(raw, []byte("fixture")) {
			t.Fatal("response exposes secret field/value")
		}
	}
	b, err := bot.Load(e.root, "ada")
	if err != nil {
		t.Fatal(err)
	}
	backend, err := vault.OpenGlobal(e.root)
	if err != nil {
		t.Fatal(err)
	}
	// Domain-only updates never need to read a value over HTTP.
	if code, raw := e.call("POST", path, `{"name":"LOCAL_SUDO","domains":["example.com"]}`, true); code != 200 || bytes.Contains(raw, []byte("value")) {
		t.Fatal("metadata update failed/leaked")
	}
	var before bytes.Buffer
	if err := vault.WriteLocalSudo(b.VaultDir(), b.VaultKeyPath(), &before); err != nil || before.String() != "fixture-local-password\n" {
		t.Fatal("domain edit changed password")
	}
	if code, _ := e.call("POST", path, `{"name":"LOCAL_SUDO","value":"fixture-reset-password","domains":[]}`, true); code != 200 {
		t.Fatal("reset failed")
	}
	before.Reset()
	if err := vault.WriteLocalSudo(b.VaultDir(), b.VaultKeyPath(), &before); err != nil || before.String() != "fixture-reset-password\n" {
		t.Fatal("reset not persisted")
	}
	// Connector install and the settings API share this exact backend.
	if err := link.SetConnectorSecret(backend, "telegram", "fixture-telegram-token"); err != nil {
		t.Fatal(err)
	}
	code, raw := e.call("GET", path, "", true)
	var list struct {
		Secrets []secretJSON `json:"secrets"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatal(err)
	}
	if code != 200 || len(list.Secrets) != 2 || bytes.Contains(raw, []byte("fixture")) || bytes.Contains(raw, []byte("value")) {
		t.Fatal("shared vault metadata missing/leaked")
	}
	if code, _ := e.call("GET", path, "", false); code != 401 {
		t.Fatal("unauthenticated secrets")
	}
	if err := b.SetOwners([]string{"other"}); err != nil {
		t.Fatal(err)
	}
	if code, _ := e.call("GET", path, "", true); code != 200 {
		t.Fatal("global secrets incorrectly depend on Bot ownership")
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	if code, _ := e.call("DELETE", path+"/LOCAL_SUDO", "", true); code != 200 {
		t.Fatal("delete failed")
	}
	var out bytes.Buffer
	if err := vault.WriteLocalSudo(b.VaultDir(), b.VaultKeyPath(), &out); err == nil || out.Len() != 0 {
		t.Fatal("deleted password remains usable")
	}
}
