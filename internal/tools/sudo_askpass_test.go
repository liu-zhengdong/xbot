package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"xbot/internal/vault"
)

func TestMain(m *testing.M) {
	// Test executable stands in for xbot's tiny, pre-config askpass dispatch.
	if len(os.Args) == 4 && os.Args[1] == "--internal-sudo-askpass" {
		if vault.WriteLocalSudo(os.Args[2], os.Args[3], os.Stdout) != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestBackgroundSudoHelperLifetime(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX sudo")
	}
	t.Setenv("XBOT_VAULT_SOCKET", "")
	base := t.TempDir()
	store, err := vault.Open(filepath.Join(base, "vault"), filepath.Join(base, "keys", "b.key"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set(vault.LocalSudo, "fixture-background-password", nil); err != nil {
		t.Fatal(err)
	}
	sh := NewShell()
	sh.Dir = base
	sh.BindLocalSudo(func() (vault.Backend, error) { return vault.Open(store.Dir(), store.KeyPath()) })
	jobs := NewJobs(filepath.Join(base, "jobs"))
	sh.Jobs = jobs
	defer jobs.Shutdown()
	done := make(chan Job, 1)
	jobs.OnDone = func(j Job) { done <- j }
	args := json.RawMessage(`{"description":"background helper lifetime","command":"sleep 0.1; test -x \"$SUDO_ASKPASS\" && printf '%s' \"$SUDO_ASKPASS\"","run_in_background":true}`)
	out, err := sh.Run(context.Background(), args)
	if err != nil || !strings.HasPrefix(out, "background ") {
		t.Fatal("not backgrounded")
	}
	select {
	case job := <-done:
		raw, err := os.ReadFile(job.Output)
		if err != nil || len(raw) == 0 {
			t.Fatal("helper removed before process exited")
		}
		if _, err := os.Stat(string(raw)); !os.IsNotExist(err) {
			t.Fatal("helper left after process exited")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("background process did not finish")
	}
}

func TestShellLocalSudoAssembly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX sudo")
	}
	t.Setenv("XBOT_VAULT_SOCKET", "")
	base := t.TempDir()
	dir := filepath.Join(base, "vault 'quoted")
	key := filepath.Join(base, "keys", "b.key")
	open := func() (vault.Backend, error) { return vault.OpenBackend("b", dir, key) }
	backend, err := open()
	if err != nil {
		t.Fatal(err)
	}
	const password = "fixture-sudo-password-1234"
	if err := backend.Set(vault.LocalSudo, password, nil); err != nil {
		t.Fatal(err)
	}
	for _, jobs := range []bool{false, true} {
		t.Run(map[bool]string{false: "direct", true: "jobs"}[jobs], func(t *testing.T) {
			sh := NewShell()
			sh.Dir = base
			sh.BindLocalSudo(open)
			if jobs {
				sh.Jobs = NewJobs(filepath.Join(base, "jobs"))
				sh.Jobs.Redact = vault.Redact
				sh.Jobs.Forms = vault.RegisteredForms
				defer sh.Jobs.Shutdown()
			}
			marker := filepath.Join(base, "helper-path")
			// Stand-in sudo consumes askpass stdout without exposing it. No real sudo
			// policy or machine password is touched by this test.
			command := `test "$SUDO_ASKPASS_REQUIRE" = 1 && test -x "$SUDO_ASKPASS" && printf '%s' "$SUDO_ASKPASS" > '` + marker + `' && ! env | grep -q '` + password + `' && test "$("$SUDO_ASKPASS")" = '` + password + `' && "$SUDO_ASKPASS" && echo authenticated`
			args, _ := json.Marshal(map[string]string{"description": "test local sudo assembly", "command": command})
			out, err := sh.Run(context.Background(), args)
			if err != nil || !strings.Contains(out, "authenticated") || strings.Contains(out, password) || !strings.Contains(out, "[secret]") {
				t.Fatalf("askpass/redaction failed: err=%v", err)
			}
			raw, err := os.ReadFile(marker)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(string(raw)); !os.IsNotExist(err) {
				t.Fatal("askpass helper not cleaned")
			}
		})
	}
	sh := NewShell()
	sh.Dir = base
	sh.BindLocalSudo(open)
	env, cleanup, _, err := sh.sudoEnv(sh.commandEnv(false))
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	helper := ""
	for _, e := range env {
		if strings.Contains(e, password) {
			t.Fatal("password in env")
		}
		if strings.HasPrefix(e, "SUDO_ASKPASS=") {
			helper = strings.TrimPrefix(e, "SUDO_ASKPASS=")
		}
	}
	raw, err := os.ReadFile(helper)
	if err != nil || strings.Contains(string(raw), password) {
		t.Fatal("helper contains password")
	}
	if err := backend.Remove(vault.LocalSudo); err != nil {
		t.Fatal(err)
	}
	// Even an already-created helper must reread disk and fail after deletion.
	args, _ := json.Marshal(map[string]string{"description": "deleted askpass", "command": "'" + helper + "'"})
	out, err := NewShell().Run(context.Background(), args)
	if err != nil || strings.Contains(out, "exit 0") || strings.Contains(out, password) {
		t.Fatal("deleted credential usable")
	}
	env, done, _, err := sh.sudoEnv(sh.commandEnv(false))
	defer done()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range env {
		if strings.HasPrefix(e, "SUDO_ASKPASS=") {
			t.Fatal("deleted key still assembled")
		}
	}
}
