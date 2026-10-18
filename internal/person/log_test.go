package person

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestIdentityLogNewestGrantWins(t *testing.T) {
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
	later := Person{ID: "u1", DisplayName: "Later", Timezone: "Asia/Shanghai", Accounts: []Account{{Link: "slack", ExternalID: "b"}}}
	if err := Record(root, "ada", "home-a", t0.Add(time.Hour), later); err != nil {
		t.Fatal(err)
	}
	got, err := Load(root, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if got.DisplayName != "Later" || got.Timezone != "Asia/Shanghai" {
		t.Fatalf("newest grant lost %+v", got)
	}
	if len(got.Accounts) != 1 || got.Accounts[0].Link != "slack" {
		t.Fatalf("accounts %+v", got.Accounts)
	}
	lines, err := ReadLog(root, "u1")
	if err != nil || len(lines) != 2 || !lines[1].Grant || lines[1].Device != "home-a" {
		t.Fatalf("log %+v %v", lines, err)
	}
	if err := Record(root, "ada", "home-a", t0.Add(-time.Hour), local); err != nil {
		t.Fatal(err)
	}
	got, err = Load(root, "u1")
	if err != nil || got.DisplayName != "Later" {
		t.Fatalf("older grant renamed the owner %+v %v", got, err)
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

func TestUnlinkRemovesAccount(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "node-id"), []byte("home-a\n"), 0600); err != nil {
		t.Fatal(err)
	}
	owner := Person{ID: "u1", DisplayName: "Ada", Accounts: []Account{
		{Link: "terminal", ExternalID: "a"},
		{Link: "telegram", ExternalID: "1"},
	}}
	if err := Record(root, "ada", "home-a", time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC), owner); err != nil {
		t.Fatal(err)
	}
	if err := Unlink(root, "u1", Account{Link: "telegram", ExternalID: "1"}); err != nil {
		t.Fatal(err)
	}
	got, err := Load(root, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Accounts) != 1 || got.Accounts[0] != (Account{Link: "terminal", ExternalID: "a"}) {
		t.Fatalf("after unlink %+v", got.Accounts)
	}
	lines, err := ReadLog(root, "u1")
	if err != nil || len(lines) != 2 || !lines[1].Grant {
		t.Fatalf("unlink line %+v %v", lines, err)
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
