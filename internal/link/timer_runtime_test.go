package link

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRoutineCommitErrorAfterDefinition(t *testing.T) {
	tm := &Timer{Dir: t.TempDir()}
	zonePath := filepath.Join(tm.Dir, ".zones")
	if err := os.WriteFile(zonePath, []byte("broken index"), 0600); err != nil {
		t.Fatal(err)
	}
	err := tm.Save(Routine{ID: "saved", Prompt: "persisted", Cron: "0 9 * * *", Zone: "local", Enabled: true})
	var partial *RoutineCommitError
	if !errors.As(err, &partial) || !partial.Applied || partial.Stage != "zone_index" || partial.Err == nil {
		t.Fatalf("save partial: %#v %v", partial, err)
	}
	if err.Error() != partial.Err.Error() || !errors.Is(err, partial.Err) {
		t.Fatalf("original diagnostic/cause lost: %v", err)
	}
	if got, loadErr := tm.Load("saved"); loadErr != nil || got.Prompt != "persisted" || got.Zone != "local" {
		t.Fatalf("definition not saved: %+v %v", got, loadErr)
	}
	for _, id := range []string{"saved", "already_missing"} {
		err := tm.Delete(id)
		var partial *RoutineCommitError
		if !errors.As(err, &partial) || !partial.Applied || partial.Stage != "zone_index" {
			t.Fatalf("delete %s partial: %#v %v", id, partial, err)
		}
		if _, err := os.Stat(filepath.Join(tm.Dir, id+".json")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("definition still present: %v", err)
		}
	}
}

func TestRoutineCommitErrorExcludesPreDefinitionFailures(t *testing.T) {
	tm := &Timer{Dir: t.TempDir()}
	for _, r := range []Routine{
		{ID: "invalid", Prompt: "", Cron: "0 9 * * *", Zone: "local"},
		{ID: "invalid", Prompt: "prompt", Cron: "* * * * *", Zone: "local"},
	} {
		err := tm.Save(r)
		var partial *RoutineCommitError
		if err == nil || errors.As(err, &partial) {
			t.Fatalf("validation reported applied: %v", err)
		}
	}
	// A directory at the definition path makes atomic replacement fail before
	// the index is touched; its nonempty contents also make Delete fail.
	path := filepath.Join(tm.Dir, "blocked.json")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "child"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, err := range []error{
		tm.Save(Routine{ID: "blocked", Prompt: "prompt", Cron: "0 9 * * *", Zone: "local"}),
		tm.Delete("blocked"),
	} {
		var partial *RoutineCommitError
		if err == nil || errors.As(err, &partial) {
			t.Fatalf("definition failure reported applied: %v", err)
		}
	}
	if err := tm.Delete("missing"); err != nil {
		t.Fatalf("desired absence: %v", err)
	}
}
