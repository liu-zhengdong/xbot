package person

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestIdentityLogNewestWinsAndKeepsBots(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "node-id"), []byte("home-a\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "bots", "ada"), 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "bots", "ada", "keep.txt")
	if err := os.WriteFile(marker, []byte("stay"), 0600); err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)
	local := Person{ID: "u1", DisplayName: "Local", Accounts: []Account{{Link: "terminal", ExternalID: "a"}}}
	if err := Record(root, "ada", "home-a", t0, local); err != nil {
		t.Fatal(err)
	}
	same := LogEntry{
		Time: t0, Bot: "bob", Device: "home-b",
		Person: Person{ID: "u1", DisplayName: "Remote", Accounts: []Account{{Link: "slack", ExternalID: "b"}}},
	}
	if err := MergeLog(root, []LogEntry{same}); err != nil {
		t.Fatal(err)
	}
	got, err := Load(root, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if got.DisplayName != "Local" {
		t.Fatalf("equal time kept %q", got.DisplayName)
	}
	if len(got.Accounts) != 2 {
		t.Fatalf("accounts %+v", got.Accounts)
	}
	newer := same
	newer.Time = t0.Add(time.Hour)
	newer.Person.DisplayName = "Later"
	newer.Person.Timezone = "Asia/Shanghai"
	if err := MergeLog(root, []LogEntry{newer}); err != nil {
		t.Fatal(err)
	}
	got, err = Load(root, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if got.DisplayName != "Later" || got.Timezone != "Asia/Shanghai" {
		t.Fatalf("%+v", got)
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

func TestIdentifyWritesLog(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "node-id"), []byte("box-a"), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := Identify(root, Account{Link: "terminal", ExternalID: "uid:7"}, "Ada")
	if err != nil {
		t.Fatal(err)
	}
	lines, err := ReadLog(root, p.ID)
	if err != nil || len(lines) != 1 {
		t.Fatalf("%+v %v", lines, err)
	}
	if lines[0].Device != "box-a" || lines[0].Person.DisplayName != "Ada" {
		t.Fatalf("%+v", lines[0])
	}
}
