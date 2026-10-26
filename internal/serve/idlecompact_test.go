package serve

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/inbox"
	"xbot/internal/kernel"
	"xbot/internal/provider"
)

type idleCompactor struct{ entered, release, done chan struct{} }

func (c *idleCompactor) Compact(context.Context, string, []provider.Message) (string, error) {
	close(c.entered)
	<-c.release
	close(c.done)
	return "summary", nil
}

func TestPassStartsIdleCompactWithoutHoldingHandle(t *testing.T) {
	t.Setenv("XBOT_IDLE_COMPACT_MIN_TOKENS", "1000")
	t.Setenv("XBOT_IDLE_COMPACT_AFTER", "1m")
	b := timerBot(t)
	now := time.Now()
	for i := 0; i < 20; i++ {
		if err := b.Append(bot.Entry{Time: now.Add(-time.Hour), Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent(strings.Repeat("old ", 800))}}); err != nil {
			t.Fatal(err)
		}
	}
	c := &idleCompactor{entered: make(chan struct{}), release: make(chan struct{}), done: make(chan struct{})}
	factory := func(b *bot.Bot) (*kernel.Kernel, error) {
		box, err := inbox.Open(b.InboxPath())
		return &kernel.Kernel{Bot: b, Inbox: box, Compactor: c}, err
	}
	if err := Pass(context.Background(), b.Root, now, factory); err != nil {
		t.Fatal(err)
	}
	select {
	case <-c.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not start idle summary")
	}
	lock, ok, err := b.TryLockHandle()
	if err != nil || !ok {
		t.Fatal("idle summary held foreground lock")
	}
	lock.Release()
	close(c.release)
	<-c.done
	// Wait for the durable ready write, not merely the model return.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		raw, _ := os.ReadFile(filepath.Join(bot.Dir(b.Root, b.Name()), "compact-job.json"))
		if strings.Contains(string(raw), `"ready"`) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("idle job did not become ready")
}
