package main

import (
	"os"
	"testing"
	"xbot/internal/bot"
)

func TestSeedDoesNotCopyKernelRules(t *testing.T) {
	root := t.TempDir()
	if err := ensureSeed(root); err != nil {
		t.Fatal(err)
	}
	bots, err := bot.List(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(bots) != 1 {
		t.Fatalf("bots: %d", len(bots))
	}
	b, err := bot.Load(root, bots[0].Name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(b.RulesPath()); !os.IsNotExist(err) {
		t.Fatalf("copied defaults: %v", err)
	}
}
