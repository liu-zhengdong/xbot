package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/memory"
)

func TestSyncExportMerge(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()
	if err := os.WriteFile(filepath.Join(a, "node-id"), []byte("home-a"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(b, "bots", "bob"), 0700); err != nil {
		t.Fatal(err)
	}
	when := time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)
	if err := memory.Open(a).Remember(context.Background(), memory.Fact{
		Key: "city", Scope: memory.ScopePerson, PersonID: "u1", Tier: memory.TierProfile,
		WrittenBy: "ada", Text: "Shanghai", Time: when,
	}); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "bundle.json")
	if err := run([]string{"--root", a, "sync", "export", "--out", out}); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(out)
	if err != nil || fi.Mode().Perm() != 0600 {
		t.Fatalf("bundle mode %v %v", fi, err)
	}
	if err := run([]string{"--root", b, "sync", "merge", "--file", out}); err != nil {
		t.Fatal(err)
	}
	got, err := memory.Open(b).Recall(context.Background(), memory.Query{Scope: memory.ScopePerson, PersonID: "u1"})
	if err != nil || len(got) != 1 || !strings.HasPrefix(got[0].Text, "Shanghai (untrusted, recorded by ada") || !got[0].Untrusted {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(b, "bots", "bob")); err != nil {
		t.Fatal(err)
	}
}
