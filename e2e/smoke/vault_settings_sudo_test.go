//go:build smoke

package smoke

import (
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
)

func TestVaultSettingsSudoBinary(t *testing.T) {
	t.Setenv("XBOT_VAULT_SOCKET", "")
	t.Setenv("XBOT_SMOKE_OAUTH", "")
	chrome := "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
	if _, err := os.Stat(chrome); err != nil {
		t.Skip("Chrome unavailable")
	}
	const password = "scripted-local-sudo-123456"
	var phase atomic.Int32
	var sudoPath string
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if strings.Contains(string(raw), password) {
			t.Error("password reached model")
		}
		switch phase.Add(1) {
		case 1:
			streamTool(w, "local_sudo", "sh", argsJSON(map[string]string{"description": "check configured local sudo", "command": shellQuote(sudoPath) + " -A"}))
		case 2:
			streamTool(w, "sudo_report", "send_message_to_user", argsJSON(map[string]string{"text": "检查完成"}))
		default:
			streamText(w, []string{"SILENT"})
		}
	}))
	defer model.Close()
	stub := &Stub{srv: model}
	srv := Start(t, stub)
	srv.CreateBot("ada", "Only follow the scripted test.")
	srv.CreateBot("bob", "A separate vault for scope checks.")
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
	srv.Send("ada", "CHECK_LOCAL_SUDO")
	rows := srv.Transcript("ada")
	encoded, _ := json.Marshal(rows)
	if strings.Contains(string(encoded), password) {
		t.Fatal("password reached transcript")
	}
	if !strings.Contains(string(encoded), "authenticated") || !strings.Contains(string(encoded), "[secret]") {
		t.Fatal("sudo -A did not authenticate/redact")
	}
	req, _ := http.NewRequest("DELETE", srv.URL+"/api/bots/ada/secrets/LOCAL_SUDO", nil)
	res, err := srv.client.Do(req)
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
