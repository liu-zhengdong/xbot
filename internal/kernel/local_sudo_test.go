package kernel

import (
	"context"
	"encoding/json"
	"runtime"
	"strings"
	"testing"

	"xbot/internal/link"
	"xbot/internal/tools"
	"xbot/internal/vault"
)

func TestShellFormsFollowReopenedSudoVault(t *testing.T) {
	k, _, _ := freshBot(t)
	open := func() *vault.Store {
		s, err := vault.Open(k.Bot.VaultDir(), k.Bot.VaultKeyPath())
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	first := open()
	if err := first.Set(vault.LocalSudo, "fixture-first-password", nil); err != nil {
		t.Fatal(err)
	}
	first = open()
	unregister := vault.Register(first)
	if !containsForm(k.shellForms(), "fixture-first-password") {
		t.Fatal("first forms missing")
	}
	unregister()
	second := open()
	if err := second.Set(vault.LocalSudo, "fixture-second-password", nil); err != nil {
		t.Fatal(err)
	}
	// Both fresh snapshots have the same store-local form generation.
	second = open()
	unregister = vault.Register(second)
	defer unregister()
	if !containsForm(k.shellForms(), "fixture-second-password") {
		t.Fatal("reopened sudo vault reused old forms")
	}
}

func TestLocalSudoMountedAndScopedShells(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX sudo")
	}
	t.Setenv("XBOT_VAULT_SOCKET", "")
	k, _, _ := freshBot(t)
	store, err := vault.Open(k.Bot.VaultDir(), k.Bot.VaultKeyPath())
	if err != nil {
		t.Fatal(err)
	}
	k.Vault = store
	k.Links = []link.Link{link.NewToolsLinkAt(k.WorkDir, "")}
	k.attachShell()
	defer k.shellJobs().Shutdown()
	var shells []*tools.Shell
	for _, l := range k.snapshotLinks() {
		for _, tool := range l.Tools() {
			if sh, ok := tool.(*tools.Shell); ok {
				shells = append(shells, sh)
			}
		}
	}
	for _, tool := range k.toolsForScope("sudo-scope") {
		if sh, ok := tool.(*tools.Shell); ok {
			shells = append(shells, sh)
		}
	}
	if len(shells) == 0 {
		t.Fatal("no actual mounted shells")
	}
	// Simulate a settings write after the kernel's store has already opened.
	writer, err := vault.OpenBackend(k.Bot.Name(), k.Bot.VaultDir(), k.Bot.VaultKeyPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Set(vault.LocalSudo, "fixture-kernel-password", nil); err != nil {
		t.Fatal(err)
	}
	args := json.RawMessage(`{"description":"verify askpass binding","command":"test -x \"$SUDO_ASKPASS\" && echo askpass-mounted"}`)
	for _, sh := range shells {
		out, err := sh.Run(context.Background(), args)
		if err != nil || !strings.Contains(out, "askpass-mounted") {
			t.Fatalf("%s missing askpass binding: %v", sh.Name(), err)
		}
	}
	if err := writer.Remove(vault.LocalSudo); err != nil {
		t.Fatal(err)
	}
	for _, sh := range shells {
		out, err := sh.Run(context.Background(), args)
		if err != nil || strings.Contains(out, "askpass-mounted") {
			t.Fatalf("%s ignored deletion: %v", sh.Name(), err)
		}
	}
}
