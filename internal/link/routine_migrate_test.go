package link

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	_ "time/tzdata"

	"xbot/internal/cron"
)

func TestMigrateRoutines(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	dir := filepath.Join(t.TempDir(), "routines")
	specs := map[string]string{
		"a": "0 13 * * *", "b": "0 4 * * *", "c": "30 0 * * *",
		"d": "0 1,3,5,7,9 * * *", "e": "30 13 * * *", "f": "0 1 * * *",
	}
	want := map[string]string{
		"a": "0 21 * * *", "b": "0 12 * * *", "c": "30 8 * * *",
		"d": "0 9,11,13,15,17 * * *", "e": "30 21 * * *", "f": "0 9 * * *",
	}
	orig := map[string][]byte{}
	for id, expr := range specs {
		writeRoutine(t, dir, Routine{ID: id, Name: id, Prompt: id, Cron: expr, Enabled: true, LastSlot: "20261017T0500"})
		raw, err := os.ReadFile(filepath.Join(dir, id+".json"))
		if err != nil {
			t.Fatal(err)
		}
		orig[id] = raw
	}
	writeRoutine(t, dir, Routine{ID: "once", Name: "once", Prompt: "once", At: "2026-10-18T09:00:00Z", Enabled: true})
	writeRoutine(t, dir, Routine{ID: "done", Name: "done", Prompt: "done", Cron: "0 9 * * *", Zone: "local", Enabled: true})
	backup := dir + "-backup-" + now.UTC().Format("20060102T150405Z")
	before, _ := os.ReadDir(dir)
	if _, err := MigrateRoutines(dir, loc, now, true); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.ReadDir(dir); len(after) != len(before) {
		t.Fatalf("dry run touched the dir: %d -> %d entries", len(before), len(after))
	}
	for id := range specs {
		if raw, _ := os.ReadFile(filepath.Join(dir, id+".json")); string(raw) != string(orig[id]) {
			t.Fatalf("dry run changed %s", id)
		}
	}
	migs, err := MigrateRoutines(dir, loc, now, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range migs {
		switch m.ID {
		case "done":
			if m.Note != "already migrated" || m.NewZone != "local" {
				t.Fatalf("done %+v", m)
			}
		case "once":
			if m.NewZone != "local" || m.At != "2026-10-18T09:00:00Z" {
				t.Fatalf("one-shot %+v", m)
			}
		default:
			if m.NewCron != want[m.ID] || m.NewZone != "local" {
				t.Fatalf("%s -> %+v", m.ID, m)
			}
		}
	}
	for id := range specs {
		if raw, err := os.ReadFile(filepath.Join(backup, id+".json")); err != nil || string(raw) != string(orig[id]) {
			t.Fatalf("backup %s: %v", id, err)
		}
	}
	list, err := (&Timer{Dir: dir, Loc: loc}).List()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range list {
		expr, ok := specs[r.ID]
		if !ok {
			continue
		}
		if r.LastSlot != "20261017T0500" {
			t.Fatalf("%s lost slot: %+v", r.ID, r)
		}
		oldSched, _ := cron.Parse(expr)
		newSched, _ := cron.Parse(r.Cron)
		at := now
		for i := 0; i < 20; i++ {
			a, ok := oldSched.Next(at, time.UTC)
			if !ok {
				t.Fatalf("%s old: no next", r.ID)
			}
			b, ok := newSched.Next(at, loc)
			if !ok || !a.Equal(b) {
				t.Fatalf("%s #%d old %s new %s", r.ID, i, a, b)
			}
			at = a
		}
	}
	if migs, err = MigrateRoutines(dir, loc, now, false); err != nil || len(migs) != len(specs)+2 {
		t.Fatalf("second run %d %v", len(migs), err)
	}
	entries, err := os.ReadDir(filepath.Dir(dir))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("%d dirs, want routines + one backup", n)
	}
}

func TestMigrateRoutinesDSTPinsUTC(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "routines")
	writeRoutine(t, dir, Routine{ID: "noon", Name: "noon", Prompt: "noon", Cron: "0 12 * * *", Enabled: true})
	migs, err := MigrateRoutines(dir, loc, time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(migs) != 1 || migs[0].NewZone != "UTC" || migs[0].NewCron != "0 12 * * *" {
		t.Fatalf("%+v", migs)
	}
}

// An old xbot rewrites a routine without its zone field. The index restores
// the zone instead of converting the already-local cron a second time.
func TestMigrateRestoresZoneAfterOldRewrite(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	dir := filepath.Join(t.TempDir(), "routines")
	writeRoutine(t, dir, Routine{ID: "a", Name: "a", Prompt: "p", Cron: "0 13 * * *", Enabled: true})
	writeRoutine(t, dir, Routine{ID: "b", Name: "b", Prompt: "p", Cron: "0 4 * * *", Enabled: true})
	if _, err := MigrateRoutines(dir, loc, now, false); err != nil {
		t.Fatal(err)
	}
	a, err := (&Timer{Dir: dir}).Load("a")
	if err != nil || a.Cron != "0 21 * * *" || a.Zone != "local" {
		t.Fatalf("migrated a %+v %v", a, err)
	}
	// Simulate a pre-C1 binary: same cron, no zone.
	old := `{"id":"a","name":"a","prompt":"p","cron":"0 21 * * *","enabled":true}
`
	if err := os.WriteFile(filepath.Join(dir, "a.json"), []byte(old), 0600); err != nil {
		t.Fatal(err)
	}
	// Before any migration runs, a reader already sees the dropped zone.
	if got, err := (&Timer{Dir: dir}).Load("a"); err != nil || got.Zone != "local" {
		t.Fatalf("zone not restored on read: %+v %v", got, err)
	}
	// A rewrite that also changed the cron is a real edit and gets converted.
	oldB := `{"id":"b","name":"b","prompt":"p","cron":"0 5 * * *","enabled":true}
`
	if err := os.WriteFile(filepath.Join(dir, "b.json"), []byte(oldB), 0600); err != nil {
		t.Fatal(err)
	}
	migs, err := MigrateRoutines(dir, loc, now, false)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]RoutineMigration{}
	for _, m := range migs {
		byID[m.ID] = m
	}
	if m := byID["a"]; m.NewZone != "local" || m.NewCron != "0 21 * * *" || !strings.Contains(m.Note, "zone restored") {
		t.Fatalf("a should be restored: %+v", m)
	}
	if m := byID["b"]; m.NewZone != "local" || m.NewCron != "0 13 * * *" || m.Note != "cron converted to local" {
		t.Fatalf("b should be converted from 05:00Z: %+v", m)
	}
}

func TestMigrateSkipsBadCronAndContinues(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	dir := filepath.Join(t.TempDir(), "routines")
	bad := filepath.Join(dir, "bad.json")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bad, []byte(`{"id":"bad","name":"bad","prompt":"p","cron":"nope","enabled":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	writeRoutine(t, dir, Routine{ID: "ok", Name: "ok", Prompt: "p", Cron: "0 13 * * *", Enabled: true})
	migs, err := MigrateRoutines(dir, loc, now, false)
	if err != nil {
		t.Fatal(err)
	}
	var sawBad, sawOK bool
	for _, m := range migs {
		switch m.ID {
		case "bad":
			sawBad = strings.HasPrefix(m.Note, "skipped: ")
		case "ok":
			sawOK = m.NewCron == "0 21 * * *"
		}
	}
	if !sawBad || !sawOK {
		t.Fatalf("bad skipped %v, ok migrated %v: %+v", sawBad, sawOK, migs)
	}
	if raw, err := os.ReadFile(bad); err != nil || !strings.Contains(string(raw), `"nope"`) {
		t.Fatalf("bad file changed: %s %v", raw, err)
	}
}

func TestMigrateBackupNameIsFresh(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	dir := filepath.Join(t.TempDir(), "routines")
	writeRoutine(t, dir, Routine{ID: "a", Name: "a", Prompt: "p", Cron: "0 13 * * *", Enabled: true})
	base := dir + "-backup-" + now.UTC().Format("20060102T150405Z")
	if err := os.MkdirAll(base, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := MigrateRoutines(dir, loc, now, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(base + "-2"); err != nil {
		t.Fatalf("second backup dir: %v", err)
	}
}

func TestMigrateDryRunWritesNothing(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "none", "routines")
	if migs, err := MigrateRoutines(missing, time.FixedZone("CST", 8*3600), time.Now(), false); err != nil || len(migs) != 0 {
		t.Fatalf("missing dir %v %v", migs, err)
	}
	if _, err := os.Stat(missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("migrate created %s: %v", missing, err)
	}
}
