package kernel

import (
	"context"
	"slices"
	"strings"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/config"
	"xbot/internal/link"
	"xbot/internal/memory"
)

func TestPrimaryCanManageBots(t *testing.T) {
	root := t.TempDir()
	primary, _, err := bot.EnsurePrimary(root, bot.Seed{Links: []string{link.NamePeers}, Owner: "u1"})
	if err != nil {
		t.Fatal(err)
	}
	defaults := primary.Config()
	if err := primary.SetOwners([]string{"u1", "u2"}); err != nil {
		t.Fatal(err)
	}
	if err := config.Add(root, config.Provider{Name: "source-only", BaseURL: "https://example.invalid/v1", APIKeyEnv: "XBOT_DRIVER_SOURCE_TEST_KEY"}, config.ModelSpec{Name: "source-model", Context: 8192}); err != nil {
		t.Fatal(err)
	}
	if err := primary.SetBinding("source-only", "source-model"); err != nil {
		t.Fatal(err)
	}
	k := &Kernel{Bot: primary, Memory: memory.Open(root), Links: []link.Link{link.Peers{}}}
	tools, err := k.modelTools()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"fork_bot", "message_bot"} {
		if !hasTool(tools, name) {
			t.Fatalf("missing %s", name)
		}
	}
	childTools, err := k.mountedTools()
	if err != nil {
		t.Fatal(err)
	}
	door := toolDoor{k}
	for _, name := range []string{"list_groups", "get_group_message_history", "create_group", "post_group", "bot_propose_persona", "create_bot", "delete_bot", "list_bots", "bot_status", "pin_bot", "set_primary"} {
		if hasTool(tools, name) || hasTool(childTools, name) {
			t.Fatalf("retired tool %s remains callable", name)
		}
		if _, err := door.Run(context.Background(), []byte(`{"action":"describe","id":"`+name+`"}`)); err == nil {
			t.Fatalf("retired tool %s remains describable", name)
		}
		if tool, _, err := door.Forward(context.Background(), []byte(`{"action":"call","id":"`+name+`","args":{}}`)); err == nil || tool != nil {
			t.Fatalf("retired tool %s still forwards", name)
		}
	}
	handlers := k.BotHandlers()
	out, err := handlers.Create(context.Background(), cliops.BotCreate{Name: "ada", Persona: "Take notes."})
	if err != nil || out.Name != "ada" || !out.Applied || out.Stage != "complete" || out.Error != "" {
		t.Fatalf("create %+v: %v", out, err)
	}
	ada, err := bot.Load(root, "ada")
	if err != nil {
		t.Fatal(err)
	}
	created, source := ada.Config(), primary.Config()
	if !slices.Equal(created.Owners, source.Owners) || !slices.Equal(created.Links, source.Links) || created.Provider != defaults.Provider || created.Model != defaults.Model {
		t.Fatalf("create did not retain source owners/links and global defaults: %+v", created)
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
	if reply, err := handlers.Delete(context.Background(), bot.SeedName); err == nil || reply.Removed {
		t.Fatalf("deleted the primary: %+v %v", reply, err)
	}
}
