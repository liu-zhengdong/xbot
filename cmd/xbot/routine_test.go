package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/link"
)

func TestRoutineMigrateUnknownBot(t *testing.T) {
	root := t.TempDir()
	if _, err := bot.Create(root, "ada", "Be brief.", nil); err != nil {
		t.Fatal(err)
	}
	err := cmdRoutine(root, []string{"migrate", "--bot", "nope"})
	if err == nil || !strings.Contains(err.Error(), "unknown bot") {
		t.Fatalf("err %v", err)
	}
}

func TestRoutineMigrateContinuesAfterBadBot(t *testing.T) {
	root := t.TempDir()
	good, err := bot.Create(root, "ada", "Be brief.", nil)
	if err != nil {
		t.Fatal(err)
	}
	bad, err := bot.Create(root, "bad", "Be brief.", nil)
	if err != nil {
		t.Fatal(err)
	}
	tm := &link.Timer{Dir: good.RoutineDir()}
	if err := tm.Save(link.Routine{ID: "a", Name: "a", Prompt: "p", Cron: "0 13 * * *", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	// A malformed routine file makes that bot fail; the next bot still migrates.
	if err := os.MkdirAll(bad.RoutineDir(), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad.RoutineDir(), "x.json"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	err = cmdRoutine(root, []string{"migrate"})
	if err == nil || !strings.Contains(err.Error(), "failed") {
		t.Fatalf("expected a failure summary, got %v", err)
	}
	got, err := (&link.Timer{Dir: good.RoutineDir()}).Load("a")
	if err != nil {
		t.Fatal(err)
	}
	if got.Zone == "" {
		t.Fatalf("good bot was not migrated: %+v", got)
	}
}

func TestRoutineSkillsCLI(t *testing.T) {
	root := t.TempDir()
	b, err := bot.Create(root, "ada", "Be brief.", nil)
	if err != nil {
		t.Fatal(err)
	}
	tm := &link.Timer{Dir: b.RoutineDir()}
	if err := tm.Save(link.Routine{ID: "morning", Prompt: "inspect", Cron: "0 * * * *", Enabled: true, Precheck: "false"}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		refs string
		want int
	}{{"ship, atlas", 2}, {"", 0}} {
		if err := cmdRoutine(root, []string{"update", "morning", "--bot", "ada", "--skills", tc.refs}); err != nil {
			t.Fatal(err)
		}
		r, err := tm.Load("morning")
		if err != nil || len(r.Skills) != tc.want || r.Precheck != "false" || !r.Enabled {
			t.Fatalf("routine %+v %v", r, err)
		}
		if tc.want > 0 && (r.Skills[0] != "ship" || r.Skills[1] != "atlas") {
			t.Fatalf("order/trim %+v", r.Skills)
		}
	}
	if err := cmdRoutine(root, []string{"update", "morning", "--bot", "ada"}); err == nil {
		t.Fatal("omitting --skills must not clear bindings")
	}
	if err := cmdRoutine(root, []string{"list", "--bot", "ada"}); err != nil {
		t.Fatal(err)
	}
}
