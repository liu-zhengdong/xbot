package web

import (
	"bytes"
	"encoding/json"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/vault"
)

func TestBotSecretsCRUDMetadataOnly(t *testing.T) {
	t.Setenv("XBOT_VAULT_SOCKET", "")
	e := newAppsEnv(t)
	path := "/api/bots/ada/secrets"
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
		{"GET", "/api/bots/bob/secrets", "", 200},
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
	backend, err := vault.OpenBackend(b.Name(), b.VaultDir(), b.VaultKeyPath())
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
	if code, _ := e.call("GET", path, "", true); code != 404 {
		t.Fatal("foreign secrets")
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
