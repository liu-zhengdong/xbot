package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/fsutil"
	"xbot/internal/link"
	"xbot/internal/provider"
	"xbot/internal/web"
)

func TestRunServeRootLockedPreservesApproval(t *testing.T) {
	root := t.TempDir()
	b, err := bot.Create(root, "ada", "persona", []string{link.NameWeb})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	ui := &web.Server{Root: root, Owner: "u1"}
	if _, _, err := ui.EnsureToken(); err != nil {
		t.Fatal(err)
	}
	card := bot.ApprovalCardText("live", "shell", "run", "local", "mkdir a", bot.ApprovalPending)
	if err := b.Append(bot.Entry{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent(card)}}); err != nil {
		t.Fatal(err)
	}
	lock, ok, err := fsutil.TryLock(filepath.Join(root, "serve.lock"))
	if err != nil || !ok {
		t.Fatalf("hold serve lock: ok=%v, err=%v", ok, err)
	}
	defer lock.Release()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = runServe(ctx, root, serveConfig{
		// Invalid port bounds the old-code run after its approval sweep, without a listener.
		HTTPAddr: "127.0.0.1:-1", Interval: time.Hour,
	})
	entries, readErr := b.Entries()
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 1 || entries[0].Message.Text() != card {
		t.Errorf("live approval transcript changed: got %d entries, want unchanged pending card", len(entries))
	}
	if err == nil || err.Error() != "xbot serve is already running for this data root" {
		t.Errorf("runServe error = %v, want root lock rejection", err)
	}
}
