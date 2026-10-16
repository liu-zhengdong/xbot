package main

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/vault"
)

// startMigrateDaemon runs a vault daemon on a short t.TempDir socket path.
func startMigrateDaemon(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	socket := filepath.Join(base, "v.sock")
	if len(socket) > 100 {
		t.Skipf("socket path too long: %s", socket)
	}
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Skipf("unix socket unavailable: %v", err)
	}
	srv := &vault.Server{DataDir: filepath.Join(base, "data"), AllowUIDs: []int{os.Getuid()}}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = ln.Close() })
	waitMigrateSocket(t, socket)
	return socket
}

func waitMigrateSocket(t *testing.T, socket string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.Dial("unix", socket)
		if err == nil {
			_ = c.Close()
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("socket %s never became ready", socket)
}

func TestSecretMigrateMovesSecretsToDaemon(t *testing.T) {
	root := t.TempDir()
	if _, err := bot.Create(root, "ada", "persona", []string{link.NameTerminal}); err != nil {
		t.Fatal(err)
	}
	const value = "migrate-secret-value-9"
	if err := withStdin(t, value+"\n", func() error {
		return cmdSecret(root, []string{"set", "ada", "GITHUB_TOKEN", "--domains", "a.com,b.com"})
	}); err != nil {
		t.Fatal(err)
	}
	b, err := bot.Load(root, "ada")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(b.VaultDir()); err != nil {
		t.Fatalf("local vault missing before migrate: %v", err)
	}

	socket := startMigrateDaemon(t)
	t.Setenv("XBOT_VAULT_SOCKET", socket)

	out := captureStdout(t, func() error { return cmdSecret(root, []string{"migrate", "ada"}) })
	if !strings.Contains(out, "migrated 1 secrets") {
		t.Fatalf("migrate output %q", out)
	}

	// The local vault dir and key file are gone.
	if _, err := os.Stat(b.VaultDir()); !os.IsNotExist(err) {
		t.Fatalf("local vault dir still present: %v", err)
	}
	if _, err := os.Stat(b.VaultKeyPath()); !os.IsNotExist(err) {
		t.Fatalf("local key still present: %v", err)
	}

	// The daemon serves the name and domains, without the value.
	client, err := vault.OpenClient(socket, "ada")
	if err != nil {
		t.Fatal(err)
	}
	list := client.List()
	if len(list) != 1 || list[0].Name != "GITHUB_TOKEN" || list[0].Value != "" {
		t.Fatalf("daemon list %+v", list)
	}
	if got := strings.Join(list[0].Domains, ","); got != "a.com,b.com" {
		t.Fatalf("daemon domains %q", got)
	}

	// The CLI now reads through the daemon.
	out = captureStdout(t, func() error { return cmdSecret(root, []string{"list", "ada"}) })
	if !strings.Contains(out, "GITHUB_TOKEN\ta.com,b.com") || strings.Contains(out, value) {
		t.Fatalf("list after migrate %q", out)
	}
}

func TestSecretMigrateNeedsSocket(t *testing.T) {
	root := t.TempDir()
	if _, err := bot.Create(root, "ada", "persona", []string{link.NameTerminal}); err != nil {
		t.Fatal(err)
	}
	os.Unsetenv("XBOT_VAULT_SOCKET")
	if err := cmdSecret(root, []string{"migrate", "ada"}); err == nil {
		t.Fatal("migrate without a socket was accepted")
	}
}
