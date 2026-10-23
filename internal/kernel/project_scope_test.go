package kernel

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/provider"
)

// A project set recorded against a line this turn's context does not hold,
// as a background branch's injection or a compacted one is, is not "already
// sent": the foreground still gets the instructions (#26).
func TestProjectSetOutsideContextIsSentAgain(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "AGENTS.md"), "FROM-ROOT\n")
	k, p, now := workBot(t, dir)
	note := k.projectNote(nil)
	if note == "" {
		t.Fatal("no project note to record")
	}
	// Bound to a line the foreground never assembles.
	if err := k.reminders().Record(99_999, KindProject, k.reminders().log.prepare(note), *now); err != nil {
		t.Fatal(err)
	}
	runTurn(t, k, now, "look", provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("ok")})
	if !strings.Contains(newest(t, p), "FROM-ROOT") {
		t.Fatalf("instructions missing: %q", newest(t, p))
	}
	// Once it is in the context, the next turn does not send it again.
	advance(now, time.Second)
	runTurn(t, k, now, "again", provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("still")})
	if projectBlock(newest(t, p)) != "" {
		t.Fatalf("sent twice: %q", newest(t, p))
	}
}
