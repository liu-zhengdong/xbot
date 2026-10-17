package link

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"xbot/internal/cron"
	"xbot/internal/fsutil"
)

// RoutineMigration describes one routine seen by MigrateRoutines.
type RoutineMigration struct {
	ID      string
	OldCron string
	NewCron string
	At      string
	OldZone string
	NewZone string
	Note    string
	// Changed is true when the file is (or in a dry run would be) rewritten.
	Changed bool
}

// MigrateRoutines rewrites legacy routines so their firing instants do not
// change. A routine with Zone "" was written before C1 and is UTC. Cron
// routines get an exact local equivalent when one exists; otherwise they stay
// pinned to UTC. One-shots keep their absolute At and become local. A routine
// that already has a zone is skipped, which makes a second run a no-op. The
// zone index is checked first: a file rewritten by an older xbot loses its
// zone but keeps the migrated cron, so it is restored instead of converted.
func MigrateRoutines(dir string, loc *time.Location, now time.Time, dryRun bool) ([]RoutineMigration, error) {
	if loc == nil {
		loc = time.Local
	}
	tm := &Timer{Dir: dir}
	// A bot without routines has nothing to migrate; do not create the
	// directory. A dry run writes nothing, not even the lock file.
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if !dryRun {
		lock, err := fsutil.Lock(filepath.Join(dir, ".lock"))
		if err != nil {
			return nil, err
		}
		defer lock.Release()
	}
	list, err := tm.listRaw()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	zones, err := tm.loadZones()
	if err != nil {
		return nil, err
	}
	out := make([]RoutineMigration, 0, len(list))
	var changed []Routine
	for _, r := range list {
		m := RoutineMigration{ID: r.ID, OldCron: r.Cron, NewCron: r.Cron, At: r.At, OldZone: r.Zone, NewZone: r.Zone}
		if r.Zone != "" {
			m.Note = "already migrated"
			out = append(out, m)
			continue
		}
		if ent, ok := zones[r.ID]; ok && ent.Cron == r.Cron {
			r.Zone = ent.Zone
			m.Note = "zone restored (file was rewritten by an older xbot)"
		} else if r.At != "" {
			r.Zone = "local"
			m.Note = "one-shot zone set to local"
		} else if newCron, ok, err := cron.Convert(r.Cron, time.UTC, loc, now); err != nil {
			m.Note = "skipped: " + err.Error()
			out = append(out, m)
			continue
		} else if ok {
			r.Cron, r.Zone = newCron, "local"
			m.Note = "cron converted to local"
		} else {
			r.Zone = "UTC"
			m.Note = "kept UTC (no exact local equivalent)"
		}
		m.NewCron, m.NewZone, m.Changed = r.Cron, r.Zone, true
		out = append(out, m)
		changed = append(changed, r)
	}
	if dryRun || len(changed) == 0 {
		return out, nil
	}
	backup, err := backupDir(filepath.Clean(dir) + "-backup-" + now.UTC().Format("20060102T150405Z"))
	if err != nil {
		return out, err
	}
	if err := copyRoutines(dir, backup); err != nil {
		return out, err
	}
	for _, r := range changed {
		if err := tm.saveLocked(r); err != nil {
			return out, err
		}
	}
	return out, nil
}

// backupDir creates a fresh directory; an existing name gets "-2", "-3", ...
func backupDir(path string) (string, error) {
	for i := 1; i < 1000; i++ {
		try := path
		if i > 1 {
			try = fmt.Sprintf("%s-%d", path, i)
		}
		err := os.Mkdir(try, 0700)
		if err == nil {
			return try, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return "", err
		}
	}
	return "", fmt.Errorf("timer: no free backup directory for %s", path)
}

// copyRoutines copies every routine file into a fresh backup directory.
func copyRoutines(dir, backup string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, ent.Name()))
		if err != nil {
			return err
		}
		if err := fsutil.WriteAtomic(filepath.Join(backup, ent.Name()), raw, 0600); err != nil {
			return err
		}
	}
	return nil
}
