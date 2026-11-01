package bot

import (
	"os"
	"path/filepath"
	"testing"
	"xbot/internal/vault"
)

func writeKey(t *testing.T, path, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestVaultKeyPathOutsideBotDir(t *testing.T) {
	b, err := Create(t.TempDir(), "ada", "p", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(VaultKeysDir(b.Root), "_product_vault.key")
	got := b.VaultKeyPath()
	if got != want {
		t.Fatalf("VaultKeyPath() = %q, want %q", got, want)
	}
	if filepath.Dir(got) == b.VaultDir() {
		t.Fatal("key still lives next to secrets.enc")
	}
	if wantDir := filepath.Join(b.Root, "keys", "vault"); VaultKeysDir(b.Root) != wantDir {
		t.Fatalf("VaultKeysDir = %q, want %q", VaultKeysDir(b.Root), wantDir)
	}
}

func TestRenameMovesVaultKey(t *testing.T) {
	root := t.TempDir()
	_, err := Create(root, "ada", "p", nil)
	if err != nil {
		t.Fatal(err)
	}
	writeKey(t, vaultKeyPath(root, "ada"), "key-material")

	if err := Rename(root, "ada", "ada2"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(vaultKeyPath(root, "ada")); !os.IsNotExist(err) {
		t.Fatalf("old key still present: %v", err)
	}
	moved := filepath.Join(VaultKeysDir(root), "ada2.key")
	raw, err := os.ReadFile(moved)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "key-material" {
		t.Fatalf("moved key = %q", raw)
	}
	info, err := os.Stat(moved)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("moved key mode %v", info.Mode().Perm())
	}
}

func TestRenameWithoutKeySucceeds(t *testing.T) {
	root := t.TempDir()
	if _, err := Create(root, "ada", "p", nil); err != nil {
		t.Fatal(err)
	}
	if err := Rename(root, "ada", "ada2"); err != nil {
		t.Fatalf("rename without a key: %v", err)
	}
}

func TestRenameRefusesToClobberDestinationKey(t *testing.T) {
	root := t.TempDir()
	_, err := Create(root, "ada", "p", nil)
	if err != nil {
		t.Fatal(err)
	}
	writeKey(t, vaultKeyPath(root, "ada"), "ada-key")
	// A stale key for the target name must not be adopted by the rename.
	writeKey(t, filepath.Join(VaultKeysDir(root), "bob.key"), "bob-key")

	if err := Rename(root, "ada", "bob"); err == nil {
		t.Fatal("rename overwrote an existing destination key")
	}
	if _, err := Load(root, "ada"); err != nil {
		t.Fatalf("bot dir not left in place: %v", err)
	}
	if raw, err := os.ReadFile(vaultKeyPath(root, "ada")); err != nil || string(raw) != "ada-key" {
		t.Fatalf("source key changed: %q %v", raw, err)
	}
	if raw, err := os.ReadFile(filepath.Join(VaultKeysDir(root), "bob.key")); err != nil || string(raw) != "bob-key" {
		t.Fatalf("destination key changed: %q %v", raw, err)
	}
}

func TestDeleteRemovesVaultKey(t *testing.T) {
	root := t.TempDir()
	if _, err := Create(root, "ada", "p", nil); err != nil {
		t.Fatal(err)
	}
	_, err := Create(root, "bob", "p", nil)
	if err != nil {
		t.Fatal(err)
	}
	writeKey(t, vaultKeyPath(root, "bob"), "bob-key")

	if err := Delete(root, "bob"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(vaultKeyPath(root, "bob")); !os.IsNotExist(err) {
		t.Fatalf("key not removed: %v", err)
	}
}

func TestGlobalVaultSurvivesBotRenameAndDelete(t *testing.T) {
	root := t.TempDir()
	a, err := Create(root, "alpha", "p", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Create(root, "beta", "p", nil)
	if err != nil {
		t.Fatal(err)
	}
	if a.VaultDir() != b.VaultDir() || a.VaultKeyPath() != b.VaultKeyPath() {
		t.Fatal("Bot still partitions product vault")
	}
	writeKey(t, a.VaultKeyPath(), "0123456789abcdef0123456789abcdef")
	if err := Rename(root, "beta", "renamed"); err != nil {
		t.Fatal(err)
	}
	if err := Delete(root, "renamed"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(a.VaultKeyPath())
	if err != nil || string(raw) != "0123456789abcdef0123456789abcdef" {
		t.Fatal("Bot lifecycle changed global key")
	}
}

func TestDeleteMigratesLegacyVaultBeforeRemovingBot(t *testing.T) {
	t.Setenv("XBOT_VAULT_SOCKET", "")
	root := t.TempDir()
	if _, err := Create(root, "keeper", "p", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := Create(root, "retired", "p", nil); err != nil {
		t.Fatal(err)
	}
	old, err := vault.Open(filepath.Join(Dir(root, "retired"), "vault"), vaultKeyPath(root, "retired"))
	if err != nil {
		t.Fatal(err)
	}
	if err := old.Set("RETAINED", "fixture-retained", nil); err != nil {
		t.Fatal(err)
	}
	if err := Delete(root, "retired"); err != nil {
		t.Fatal(err)
	}
	backend, err := vault.OpenGlobal(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(backend.Names()) != 1 || backend.Names()[0] != "RETAINED" {
		t.Fatal("Bot deletion lost legacy product credential")
	}
}

func TestDeleteWithoutKeySucceeds(t *testing.T) {
	root := t.TempDir()
	if _, err := Create(root, "ada", "p", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := Create(root, "bob", "p", nil); err != nil {
		t.Fatal(err)
	}
	if err := Delete(root, "bob"); err != nil {
		t.Fatalf("delete without a key: %v", err)
	}
}
