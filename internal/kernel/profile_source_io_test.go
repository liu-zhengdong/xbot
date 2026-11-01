package kernel

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/inbox"
	"xbot/internal/mcp"
	"xbot/internal/notify"
)

func TestProfileOriginalSourceKeepsPersonaRulesAndNotificationAfterRename(t *testing.T) {
	k, _, _ := freshBot(t)
	original := k.Bot
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager := mcp.NewManager(ctx, mcp.Options{})
	defer manager.Close()
	resources, err := NewBotResources(ctx, original, manager)
	if err != nil {
		t.Fatal(err)
	}
	defer resources.Close()
	k.Resources = resources
	next := "source-io-renamed"
	if err := resources.RenameTo(next); err != nil {
		t.Fatal(err)
	}
	if k.Bot != original {
		t.Fatal("test no longer exercises the original source handle")
	}
	raw, _ := json.Marshal(map[string]string{"text": "Keep the same original source duties after rename."})
	if _, err := (personaTool{k: k}).Run(ctx, raw); err != nil {
		t.Fatal(err)
	}
	seen := false
	k.Desktop = func(_ context.Context, name, text string) error {
		if !resources.route.TryLock() {
			t.Fatal("notification callback holds current route")
		}
		resources.route.Unlock()
		if name != next {
			t.Fatalf("notification identity %s", name)
		}
		seen = true
		return nil
	}
	if err := deliverNote(ctx, k, "source after rename"); err != nil {
		t.Fatal(err)
	}
	if !seen {
		t.Fatal("notification omitted")
	}
	if _, err := k.botRules(); err != nil {
		t.Fatal(err)
	}
	_ = k.logCurrentTrace(bot.TraceStop, "source resumed")
	if err := resources.WithCurrentIO(func(current *bot.Bot, _ *inbox.Inbox) error {
		persona, err := current.ReadPersona()
		if err != nil {
			return err
		}
		if persona != "Keep the same original source duties after rename." {
			t.Fatal(persona)
		}
		notes, err := notify.Open(current.OutboxPath()).Unread()
		if err != nil {
			return err
		}
		if len(notes) != 1 || notes[0].Text != "source after rename" {
			t.Fatal(notes)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(bot.Dir(original.Root, next), "trace.jsonl")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(bot.Dir(original.Root, original.Name())); !os.IsNotExist(err) {
		t.Fatalf("former directory revived: %v", err)
	}
}
