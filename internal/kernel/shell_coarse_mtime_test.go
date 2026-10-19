package kernel

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestShellWriteWithCoarseMtimeIsSeen covers Linux, where a file's mtime comes
// from a coarse clock and can sit a few milliseconds before the precise
// time.Now() taken when the command started. Such a write used to be read as
// "not this command": skipped when its stamp equalled the recorded one, or
// when it fell just before the start. The bot's own edit then came back on a
// later turn as someone else's change.
func TestShellWriteWithCoarseMtimeIsSeen(t *testing.T) {
	start := time.Now()
	end := start.Add(50 * time.Millisecond)
	cases := map[string]struct{ read, write time.Time }{
		// Read and rewritten inside one clock tick: same stamp, same size.
		"same tick": {read: start.Add(-2 * time.Millisecond), write: start.Add(-2 * time.Millisecond)},
		// Read long ago, rewritten in the tick that holds start.
		"tick before start": {read: start.Add(-time.Second), write: start.Add(-3 * time.Millisecond)},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "note.txt")
			if err := os.WriteFile(path, []byte("old\n"), 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(path, c.read, c.read); err != nil {
				t.Fatal(err)
			}
			snap, err := snapshotFile(path)
			if err != nil {
				t.Fatal(err)
			}
			k := &Kernel{}
			k.storeFile(path, snap, 1, true, "note.txt", nil)
			if err := os.WriteFile(path, []byte("new\n"), 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(path, c.write, c.write); err != nil {
				t.Fatal(err)
			}
			got := k.noteShellWrites(context.Background(), start, end)
			if len(got) != 1 || got[0] != "note.txt" {
				t.Fatalf("changed %v, want the command's own write to note.txt", got)
			}
		})
	}
}

// TestShellWindowSkipsOlderWrites keeps the slack small: a change stamped well
// before the command is still not attributed to it.
func TestShellWindowSkipsOlderWrites(t *testing.T) {
	start := time.Now()
	if inCallWindow(start.Add(-time.Second), start, start.Add(time.Millisecond)) {
		t.Fatal("a write a second before the command counted as the command's")
	}
}
