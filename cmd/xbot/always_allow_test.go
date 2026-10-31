package main

import (
	"testing"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/permissions"
)

func TestProductionKernelAlwaysAllowsTools(t *testing.T) {
	t.Setenv("XBOT_API_KEY", "fake-test-key")
	root := t.TempDir()
	b, err := bot.Create(root, "ada", "persona", []string{link.NameTerminal})
	if err != nil {
		t.Fatal(err)
	}
	if err := permissions.Set(root, true); err != nil {
		t.Fatal(err)
	}
	k, err := newServeKernel(root, b)
	if err != nil {
		t.Fatal(err)
	}
	if !k.Permissive {
		t.Fatal("production kernel must always allow tool calls")
	}
	// The same openKernel constructor serves chat and serve.
	if k.PermissionsOn != nil {
		t.Fatal("production must not wire the legacy live permission switch")
	}
}
