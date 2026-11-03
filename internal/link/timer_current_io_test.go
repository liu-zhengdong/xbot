package link

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"xbot/internal/inbox"
)

func TestTimerCurrentDirectoryCommitsAfterEmitRename(t *testing.T) {
	root := t.TempDir()
	oldBot, nextBot := filepath.Join(root, "before"), filepath.Join(root, "after")
	dir := filepath.Join(oldBot, "routines")
	var route sync.RWMutex
	tm := &Timer{Dir: dir, Sender: "original-sender", Loc: time.UTC}
	tm.WithDirectoryIO = func(fn func(string) error) error { route.RLock(); defer route.RUnlock(); return fn(dir) }
	at := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	if err := tm.Save(Routine{ID: "once", Prompt: "wake", At: at.Format(time.RFC3339), Zone: "local", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	due, err := tm.Due(at)
	if err != nil || len(due) != 1 {
		t.Fatalf("due=%+v err=%v", due, err)
	}
	if err := tm.Deliver(context.Background(), due[0], func(ev inbox.Event) error {
		if ev.ID != due[0].ID || ev.Sender != "original-sender" {
			t.Fatalf("identity changed: %+v", ev)
		}
		route.Lock()
		defer route.Unlock()
		if err := os.Rename(oldBot, nextBot); err != nil {
			return err
		}
		dir = filepath.Join(nextBot, "routines")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	got, err := tm.Load("once")
	if err != nil {
		t.Fatal(err)
	}
	if got.Enabled || got.LastSlot == "" || got.Zone != "local" {
		t.Fatalf("commit used stale directory: %+v", got)
	}
	if again, err := tm.Due(at); err != nil || len(again) != 0 {
		t.Fatalf("duplicate slot: %+v %v", again, err)
	}
	if err := tm.Update("once", func(r *Routine) error { r.Prompt = "changed"; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := tm.Save(Routine{ID: "expired", Prompt: "late", At: at.Add(-time.Hour).Format(time.RFC3339), Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := tm.Expire(at); err != nil {
		t.Fatal(err)
	}
	if expired, err := tm.Load("expired"); err != nil || expired.Enabled {
		t.Fatalf("expire=%+v %v", expired, err)
	}
	if err := tm.Delete("expired"); err != nil {
		t.Fatal(err)
	}
	list, err := tm.List()
	if err != nil || len(list) != 1 || list[0].Prompt != "changed" {
		t.Fatalf("list=%+v %v", list, err)
	}
	if _, err := os.Stat(oldBot); !os.IsNotExist(err) {
		t.Fatalf("old directory recreated: %v", err)
	}
}
