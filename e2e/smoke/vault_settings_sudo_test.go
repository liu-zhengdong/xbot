//go:build smoke

package smoke

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"xbot/internal/browsertest"
)

func TestVaultSettingsSudoBinary(t *testing.T) {
	t.Setenv("XBOT_VAULT_SOCKET", "")
	t.Setenv("XBOT_SMOKE_OAUTH", "")
	chrome := browsertest.Chrome(t)
	const password = "scripted-local-sudo-123456"
	var phase atomic.Int32
	var injected atomic.Int32
	outbound := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer scripted-global-token" {
			t.Error("global outbound injection missing")
		}
		injected.Add(1)
		io.WriteString(w, "shared outbound authenticated")
	}))
	defer outbound.Close()
	var sudoPath string
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if strings.Contains(string(raw), password) {
			t.Error("password reached model")
		}
		switch phase.Add(1) {
		case 1:
			streamTool(w, "shared_http", "http_request", argsJSON(map[string]any{"method": "GET", "url": outbound.URL, "headers": map[string]string{"Authorization": "Bearer {{secret:SHARED_TOKEN}}"}}))
		case 2:
			streamTool(w, "local_sudo", "sh", argsJSON(map[string]string{"description": "check configured local sudo", "command": shellQuote(sudoPath) + " -A"}))
		case 3:
			streamTool(w, "sudo_report", "send_message_to_user", argsJSON(map[string]string{"text": "检查完成"}))
		default:
			streamText(w, []string{"SILENT"})
		}
	}))
	defer model.Close()
	stub := &Stub{srv: model}
	srv := Start(t, stub)
	srv.CreateBot("ada", "Only follow the scripted test.")
	srv.CreateBot("bob", "A second Bot sharing the product vault.")
	sudoPath = filepath.Join(t.TempDir(), "sudo")
	script := `#!/bin/sh
[ "$1" = '-A' ] || exit 3
[ -n "$SUDO_ASKPASS" ] || exit 4
password=$("$SUDO_ASKPASS") || exit 5
[ "$password" = '` + password + `' ] || exit 6
printf '%s\n' "$password"
echo authenticated
`
	if err := os.WriteFile(sudoPath, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	shots := os.Getenv("XBOT_VAULT_EVIDENCE")
	if shots == "" {
		shots = filepath.Join(t.TempDir(), "shots")
	}
	u, _ := url.Parse(srv.URL)
	cfg := map[string]any{"chrome": chrome, "url": srv.URL, "password": password, "shots": shots, "cookies": srv.client.Jar.Cookies(u)}
	raw, _ := json.Marshal(cfg)
	cfgPath := filepath.Join(t.TempDir(), "browser.json")
	if err := os.WriteFile(cfgPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("node", filepath.Join("..", "..", "internal", "web", "testdata", "vault-settings.cjs"), cfgPath).CombinedOutput()
	if err != nil {
		t.Fatalf("browser: %v\n%s", err, out)
	}
	t.Log(strings.TrimSpace(string(out)))
	sharedURL, _ := url.Parse(outbound.URL)
	payload, _ := json.Marshal(map[string]any{"name": "SHARED_TOKEN", "value": "scripted-global-token", "domains": []string{sharedURL.Hostname()}})
	res, err := srv.client.Post(srv.URL+"/api/secrets", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatal("global API write failed")
	}
	for _, name := range []string{"ada", "bob"} {
		phase.Store(0)
		srv.Send(name, "CHECK_SHARED_VAULT")
		encoded, _ := json.Marshal(srv.Transcript(name))
		if strings.Contains(string(encoded), password) || strings.Contains(string(encoded), "scripted-global-token") {
			t.Fatal("secret reached transcript")
		}
		if !strings.Contains(string(encoded), "authenticated") || !strings.Contains(string(encoded), "[secret]") {
			t.Fatal("shared sudo/injection missing")
		}
	}
	if injected.Load() != 2 {
		t.Fatal("both Bot outbound paths did not use the global key")
	}
	rows := srv.Transcript("ada")
	encoded, _ := json.Marshal(rows)
	if strings.Contains(string(encoded), password) {
		t.Fatal("password reached transcript")
	}
	if !strings.Contains(string(encoded), "authenticated") || !strings.Contains(string(encoded), "[secret]") {
		t.Fatal("sudo -A did not authenticate/redact")
	}
	req, _ := http.NewRequest("DELETE", srv.URL+"/api/secrets/LOCAL_SUDO", nil)
	res, err = srv.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatal("delete failed")
	}
	phase.Store(0)
	srv.Send("ada", "CHECK_DELETED_LOCAL_SUDO")
	rows = srv.Transcript("ada")
	found := false
	for _, r := range rows {
		if r.Role == "tool" && strings.Contains(r.Content, "exit 4") {
			found = true
		}
	}
	if !found {
		t.Fatal("sudo succeeded after deletion")
	}
	t.Logf("real binary askpass + scripted sudo -A passed before deletion and failed after; screenshots=%s", shots)
}
