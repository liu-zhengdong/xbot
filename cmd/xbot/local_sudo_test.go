package main

import (
	"bytes"
	"testing"
	"xbot/internal/bot"
	"xbot/internal/vault"
)

func TestSecretCLILocalSudoAndAskpassEntry(t *testing.T) {
	t.Setenv("XBOT_VAULT_SOCKET", "")
	root := t.TempDir()
	b, err := bot.Create(root, "ada", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	captureStdout(t, func() error {
		return withStdin(t, "fixture-local-password\n", func() error { return cmdSecret(root, []string{"set", "ada", "LOCAL_SUDO"}) })
	})
	list := captureStdout(t, func() error { return cmdSecret(root, []string{"list", "ada"}) })
	if bytes.Contains([]byte(list), []byte("fixture")) || !bytes.Contains([]byte(list), []byte("LOCAL_SUDO")) {
		t.Fatal("CLI metadata contract")
	}
	got := captureStdout(t, func() error { return run([]string{"--internal-sudo-askpass", b.VaultDir(), b.VaultKeyPath()}) })
	if got != "fixture-local-password\n" {
		t.Fatal("askpass entry failed")
	}
	captureStdout(t, func() error { return cmdSecret(root, []string{"rm", "ada", "LOCAL_SUDO"}) })
	var out bytes.Buffer
	if err := vault.WriteLocalSudo(b.VaultDir(), b.VaultKeyPath(), &out); err == nil || out.Len() != 0 {
		t.Fatal("CLI delete not observed")
	}
	if run([]string{"--internal-sudo-askpass", b.VaultDir(), b.VaultKeyPath(), "OTHER"}) == nil {
		t.Fatal("askpass accepted arbitrary name")
	}
}
