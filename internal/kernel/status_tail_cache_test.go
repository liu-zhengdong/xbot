package kernel

import (
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/provider"
)

func tailBot(t *testing.T) *bot.Bot {
	t.Helper()
	b, err := bot.Create(t.TempDir(), "tails", "p", nil)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func say(t *testing.T, b *bot.Bot, role, text string) {
	t.Helper()
	if err := b.Append(bot.Entry{Time: time.Now(), Message: provider.Message{Role: role, Content: text}}); err != nil {
		t.Fatal(err)
	}
}

// TestTailCacheFollowsAppends pins the invalidation: the cached preview is
// only reused while the transcript is unchanged, so the sidebar never shows a
// stale last line after anyone appends.
func TestTailCacheFollowsAppends(t *testing.T) {
	b := tailBot(t)
	say(t, b, provider.RoleUser, "first question")
	if got := IdleStatus(b, time.Now()).LastMessage; got != "first question" {
		t.Fatalf("preview %q", got)
	}
	if got := IdleStatus(b, time.Now()).LastMessage; got != "first question" {
		t.Fatalf("cached preview %q", got)
	}
	say(t, b, provider.RoleAssistant, "the answer")
	if got := IdleStatus(b, time.Now()).LastMessage; got != "the answer" {
		t.Fatalf("after append %q, want the new line", got)
	}
}

// TestTailCacheRedactsOnEveryRead keeps a secret added to the vault after the
// line was cached out of the preview: the cache holds the raw line and the
// redaction runs on each call.
func TestTailCacheRedactsOnEveryRead(t *testing.T) {
	b := tailBot(t)
	say(t, b, provider.RoleUser, "token sk-later-added")
	if got := IdleStatus(b, time.Now()).LastMessage; !strings.Contains(got, "sk-later-added") {
		t.Fatalf("preview %q", got)
	}
	restore := bot.SetRedactText(func(s string) string { return strings.ReplaceAll(s, "sk-later-added", "[x]") })
	defer restore()
	if got := IdleStatus(b, time.Now()).LastMessage; strings.Contains(got, "sk-later-added") {
		t.Fatalf("cached preview kept the secret: %q", got)
	}
}
