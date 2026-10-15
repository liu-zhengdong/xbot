package memory

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPersonSyncNewestWinsAndKeepsBots(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "node-id"), []byte("home-a"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "bots", "ada"), 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "bots", "ada", "keep.txt")
	if err := os.WriteFile(marker, []byte("stay"), 0600); err != nil {
		t.Fatal(err)
	}
	s := Open(root)
	t0 := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	if err := s.Remember(context.Background(), Fact{
		Key: "city", Scope: ScopePerson, PersonID: "u1", Tier: TierProfile,
		WrittenBy: "ada", Text: "Shanghai", Time: t0,
	}); err != nil {
		t.Fatal(err)
	}
	lines, err := ReadSync(root, "u1")
	if err != nil || len(lines) != 1 || lines[0].Bot != "ada" || lines[0].Device != "home-a" {
		t.Fatalf("%+v %v", lines, err)
	}
	same := lines[0]
	same.Device = "home-b"
	same.Bot = "bob"
	same.Fact.Text = "Beijing"
	same.Fact.WrittenBy = "bob"
	if err := MergeSync(root, []SyncLine{same}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Recall(context.Background(), Query{Scope: ScopePerson, PersonID: "u1"})
	if err != nil || len(got) != 1 || got[0].Text != "Shanghai" {
		t.Fatalf("equal time %+v %v", got, err)
	}
	newer := same
	newer.Time = t0.Add(time.Hour)
	newer.Fact.Time = newer.Time
	newer.Fact.Text = "Beijing"
	if err := MergeSync(root, []SyncLine{newer}); err != nil {
		t.Fatal(err)
	}
	got, err = s.Recall(context.Background(), Query{Scope: ScopePerson, PersonID: "u1"})
	if err != nil || len(got) != 1 || got[0].Text != "Beijing" {
		t.Fatalf("newer %+v %v", got, err)
	}
	older := newer
	older.Time = t0.Add(-time.Hour)
	older.Fact.Time = older.Time
	older.Fact.Text = "Nanjing"
	older.Device = "home-c"
	if err := MergeSync(root, []SyncLine{older}); err != nil {
		t.Fatal(err)
	}
	got, err = s.Recall(context.Background(), Query{Scope: ScopePerson, PersonID: "u1"})
	if err != nil || len(got) != 1 || got[0].Text != "Beijing" {
		t.Fatalf("older %+v %v", got, err)
	}
	raw, err := os.ReadFile(marker)
	if err != nil || string(raw) != "stay" {
		t.Fatalf("bot moved %q %v", raw, err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "bots"))
	if err != nil || len(entries) != 1 || entries[0].Name() != "ada" {
		t.Fatalf("bots %+v %v", entries, err)
	}
}

func TestBotMemoryIsNotSynced(t *testing.T) {
	root := t.TempDir()
	s := Open(root)
	if err := s.Remember(context.Background(), Fact{
		Key: "style", Scope: ScopeBot, BotName: "ada", Text: "terse",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "people")); !os.IsNotExist(err) {
		t.Fatalf("person log created: %v", err)
	}
}

func TestForgetSyncDropsOlderFact(t *testing.T) {
	home := t.TempDir()
	other := t.TempDir()
	t0 := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	fact := Fact{
		Key: "city", Scope: ScopePerson, PersonID: "u1", Tier: TierLog,
		WrittenBy: "ada", Text: "Shanghai", Time: t0,
	}
	if err := Open(home).Remember(context.Background(), fact); err != nil {
		t.Fatal(err)
	}
	if err := Open(other).Remember(context.Background(), fact); err != nil {
		t.Fatal(err)
	}
	if err := Open(home).Forget(context.Background(), Query{Scope: ScopePerson, PersonID: "u1"}, "Shanghai"); err != nil {
		t.Fatal(err)
	}
	remote, err := ReadSync(home, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if err := MergeSync(other, remote); err != nil {
		t.Fatal(err)
	}
	got, err := Open(other).Recall(context.Background(), Query{Scope: ScopePerson, PersonID: "u1"})
	if err != nil || len(got) != 0 {
		t.Fatalf("forgot %+v %v", got, err)
	}
}
