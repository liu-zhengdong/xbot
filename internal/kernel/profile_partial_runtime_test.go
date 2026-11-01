package kernel

import (
	"context"
	"os"
	"slices"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/mcp"
)

func TestProfileCommittedReceiptAndNoticeSurviveClosedInboxRoute(t *testing.T) {
	k, _, _ := freshBot(t)
	b := k.Bot
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager := mcp.NewManager(ctx, mcp.Options{})
	defer manager.Close()
	resources, err := NewBotResources(ctx, b, manager)
	if err != nil {
		t.Fatal(err)
	}
	defer resources.Close()
	k.Resources = resources
	notice := make(chan ProfileNotice, 1)
	turn := &turn{}
	k.openInterject(turn, &Sink{OnProfile: func(value ProfileNotice) { notice <- value }})
	defer k.closeInterject(turn, true)
	previous := b.Name()
	// This log is written after the original Inbox was opened. Physical rename
	// and config save succeed; reopening this moved log is the real late error.
	if err := os.WriteFile(b.InboxPath(), []byte("{not valid json}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	next, description := "committed-name", "committed description"
	change, err := bot.ValidateProfileChange(bot.ProfileChange{Name: &next, Description: &description})
	if err != nil {
		t.Fatal(err)
	}
	value, err := k.ApplyProfileChange(change)
	if err == nil || value.Error == "" {
		t.Fatalf("late failure lost: %+v %v", value, err)
	}
	if value.Name != next || value.PreviousName != previous || !value.Moved || !value.Applied || len(value.AppliedFields) != 2 || !slices.Contains(value.AppliedFields, "name") || !slices.Contains(value.AppliedFields, "description") {
		t.Fatalf("commit facts lost: %+v %v", value, err)
	}
	if !resources.closed || resources.Name() != next {
		t.Fatalf("route did not retain committed name: %s", resources.Name())
	}
	select {
	case got := <-notice:
		if got.Name != next || got.PreviousName != previous {
			t.Fatal(got)
		}
	default:
		t.Fatal("committed change notice was blocked by snapshot failure")
	}
	if _, err := os.Stat(bot.Dir(b.Root, previous)); !os.IsNotExist(err) {
		t.Fatalf("former directory revived: %v", err)
	}
	saved, err := bot.Load(b.Root, next)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Config().Description != description {
		t.Fatal(saved.Config())
	}
}
