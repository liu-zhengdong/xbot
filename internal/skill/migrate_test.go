package skill_test

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"xbot/internal/fsutil"
	"xbot/internal/skill"
)

func legacySkill(t *testing.T, dir, body string, stamp int64) {
	t.Helper()
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(file, []byte("---\nname: foo\ndescription: test\n---\n"+body), 0600); err != nil {
		t.Fatal(err)
	}
	at := time.Unix(stamp, 0)
	if err := os.Chtimes(file, at, at); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateFlatLibrary(t *testing.T) {
	for _, dataRoot := range []string{".xbot", ".xbot-dev", "custom"} {
		t.Run(dataRoot, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), dataRoot)
			dir := skill.Dir(root)
			legacySkill(t, filepath.Join(dir, "u1", "foo"), "PRIVATE_OLDER_BODY_9", 10)
			legacySkill(t, filepath.Join(dir, "u2", "foo"), "newer", 20)
			legacySkill(t, filepath.Join(dir, "bar"), "flat", 5)
			// A nested bundled skill is not an owner/slug layout.
			legacySkill(t, filepath.Join(dir, "bar", "nested"), "bundle", 30)
			if err := os.WriteFile(filepath.Join(dir, "u2", "foo", "helper.py"), []byte("helper"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := skill.Migrate(root); err != nil {
				t.Fatal(err)
			}
			for slug, want := range map[string]string{"foo": "newer", "foo__from_u1": "PRIVATE_OLDER_BODY_9", "bar": "flat"} {
				if got, err := skill.Read(dir, slug); err != nil || got != want {
					t.Fatalf("%s: %q %v", slug, got, err)
				}
			}
			if fi, err := os.Stat(filepath.Join(dir, "bar", "SKILL.md")); err != nil || fi.ModTime().Unix() != 5 {
				t.Fatalf("flat changed: %v %v", fi, err)
			}
			for _, path := range []string{"foo/helper.py", "bar/nested/SKILL.md"} {
				if _, err := os.Stat(filepath.Join(dir, path)); err != nil {
					t.Fatal(err)
				}
			}
			infos, err := skill.List(dir)
			if err != nil || len(infos) != 3 {
				t.Fatalf("list: %+v %v", infos, err)
			}
			before, err := os.ReadFile(filepath.Join(dir, ".migration.log"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(before), "foo__from_u1") || strings.Contains(string(before), "PRIVATE_OLDER_BODY_9") {
				t.Fatal("missing path log or leaked body")
			}
			if err := skill.Migrate(root); err != nil {
				t.Fatal(err)
			}
			after, _ := os.ReadFile(filepath.Join(dir, ".migration.log"))
			if string(before) != string(after) {
				t.Fatal("repeat migration wrote log")
			}
		})
	}
}

func TestMigrateFlatConflictAndBackupCollision(t *testing.T) {
	for _, stamp := range []int64{5, 10, 20} {
		t.Run(time.Unix(stamp, 0).String(), func(t *testing.T) {
			root := t.TempDir()
			dir := skill.Dir(root)
			legacySkill(t, filepath.Join(dir, "foo"), "flat", 10)
			legacySkill(t, filepath.Join(dir, "u1", "foo"), "legacy", stamp)
			origin := "u1"
			primary, backup := "flat", "legacy"
			if stamp > 10 {
				origin = "local"
				primary, backup = backup, primary
			}
			legacySkill(t, filepath.Join(dir, "foo__from_"+origin), "existing-backup", 1)
			if err := skill.Migrate(root); err != nil {
				t.Fatal(err)
			}
			for slug, want := range map[string]string{"foo": primary, "foo__from_" + origin: "existing-backup", "foo__from_" + origin + "__2": backup} {
				if got, err := skill.Read(dir, slug); err != nil || got != want {
					t.Fatalf("%s: %q %v", slug, got, err)
				}
			}
			if err := skill.Migrate(root); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMigrateSymlinksAndPartialFailure(t *testing.T) {
	root := t.TempDir()
	dir := skill.Dir(root)
	legacySkill(t, filepath.Join(dir, "u1", "good"), "good", 1)
	external := filepath.Join(t.TempDir(), "external")
	legacySkill(t, external, "external", 1)
	// A symlink used as owner is not traversed.
	if err := os.Symlink(filepath.Dir(external), filepath.Join(dir, "other-owner")); err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(filepath.Join(dir, "u1"), external)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(rel, filepath.Join(dir, "u1", "linked")); err != nil {
		t.Fatal(err)
	}
	// A malformed SKILL.md fails only this entry, and remains for retry.
	if err := os.MkdirAll(filepath.Join(dir, "u1", "bad", "SKILL.md"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := skill.Migrate(root); err == nil {
		t.Fatal("missing partial failure")
	}
	for slug, want := range map[string]string{"good": "good", "linked": "external"} {
		if got, err := skill.Read(dir, slug); err != nil || got != want {
			t.Fatalf("%s: %q %v", slug, got, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "external")); !os.IsNotExist(err) {
		t.Fatal("traversed owner symlink")
	}
	if err := os.Remove(filepath.Join(dir, "u1", "bad", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	legacySkill(t, filepath.Join(dir, "u1", "bad"), "repaired", 1)
	if err := skill.Migrate(root); err != nil {
		t.Fatal(err)
	}
	if got, err := skill.Read(dir, "bad"); err != nil || got != "repaired" {
		t.Fatalf("retry: %q %v", got, err)
	}
}

func TestMigrateAndImportShareWriteLock(t *testing.T) {
	root := t.TempDir()
	dir := skill.Dir(root)
	legacySkill(t, filepath.Join(dir, "u1", "foo"), "legacy", 1)
	lock, err := fsutil.Lock(filepath.Join(dir, ".write.lock"))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 2)
	go func() { done <- skill.Migrate(root) }()
	go func() {
		_, err := skill.ImportData(root, "bar", []byte("---\nname: bar\ndescription: test\n---\nbody"), false)
		done <- err
	}()
	select {
	case err := <-done:
		lock.Release()
		t.Fatalf("bypassed lock: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	lock.Release()
	for i := 0; i < 2; i++ {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("deadlock")
		}
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := skill.Migrate(root); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	infos, err := skill.List(dir)
	if err != nil || len(infos) != 2 {
		t.Fatalf("%+v %v", infos, err)
	}
}
