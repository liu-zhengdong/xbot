package kernel

import (
	"context"
	"strings"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/memory"
)

func TestPrimaryCanManageBots(t *testing.T) {
	root := t.TempDir()
	primary, _, err := bot.EnsurePrimary(root, bot.Seed{Links: []string{link.NamePeers}, Owner: "u1"})
	if err != nil {
		t.Fatal(err)
	}
	k := &Kernel{Bot: primary, Memory: memory.Open(root), Links: []link.Link{link.Peers{}}}
	tools, err := k.modelTools()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"create_bot", "delete_bot", "pin_bot", "set_primary", "fork_bot"} {
		if !hasTool(tools, name) {
			t.Fatalf("missing %s", name)
		}
	}
	out, err := (createBotTool{k}).Run(context.Background(), []byte(`{"name":"ada","persona":"Take notes."}`))
	if err != nil {
		t.Fatal(err)
	}
	if out != "created ada" {
		t.Fatalf("%q", out)
	}
	ada, err := bot.Load(root, "ada")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := ada.Entries()
	if err != nil || len(entries) != 0 {
		t.Fatalf("entries %d %v", len(entries), err)
	}
	persona, err := ada.ReadPersona()
	if err != nil || strings.TrimSpace(persona) != "Take notes." {
		t.Fatalf("persona %q %v", persona, err)
	}
	if name, err := bot.Primary(root); err != nil || name != bot.SeedName {
		t.Fatalf("primary moved to %q %v", name, err)
	}
	if _, err := (deleteBotTool{k}).Run(context.Background(), []byte(`{"name":"`+bot.SeedName+`"}`)); err == nil {
		t.Fatal("deleted the primary")
	}
}
