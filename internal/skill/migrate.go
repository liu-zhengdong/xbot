package skill

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"xbot/internal/fsutil"
)

// Migrate flattens only legacy owner/slug directories in this data root.
// SKILL.md mtime chooses the primary copy; ties keep an existing flat copy,
// then the lexically first owner. Nothing is overwritten or recursively removed.
// Failed entries stay available for a later retry; other slugs still migrate.
func Migrate(root string) error {
	dir := Dir(root)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	lock, err := fsutil.Lock(filepath.Join(dir, ".write.lock"))
	if err != nil {
		return err
	}
	defer lock.Release()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	type candidate struct {
		path, owner string
		mtime       time.Time
	}
	groups := map[string][]candidate{}
	var failures []error
	var owners []string
	for _, owner := range entries {
		// Never traverse symlinked owner directories or hidden metadata.
		if !owner.IsDir() || !ValidOwner(owner.Name()) {
			continue
		}
		path := filepath.Join(dir, owner.Name())
		if _, err := os.Lstat(filepath.Join(path, "SKILL.md")); err == nil {
			continue // Already flat, including any bundled nested skills.
		} else if !os.IsNotExist(err) {
			failures = append(failures, err)
			continue
		}
		children, err := os.ReadDir(path)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		legacy := false
		for _, child := range children {
			if !ValidSlug(child.Name()) {
				continue
			}
			src := filepath.Join(path, child.Name())
			fi, err := os.Stat(filepath.Join(src, "SKILL.md"))
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				failures = append(failures, err)
				continue
			}
			if !fi.Mode().IsRegular() {
				failures = append(failures, fmt.Errorf("skill migrate: not a regular SKILL.md: %s", src))
				continue
			}
			legacy = true
			groups[child.Name()] = append(groups[child.Name()], candidate{src, owner.Name(), fi.ModTime()})
		}
		if legacy {
			owners = append(owners, path)
		}
	}
	if len(groups) == 0 {
		return errors.Join(failures...)
	}
	journal, err := os.OpenFile(filepath.Join(dir, ".migration.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return errors.Join(append(failures, err)...)
	}
	defer journal.Close()
	move := func(src, dst string) error {
		// Record the intent durably before changing paths, so even a process
		// interruption leaves evidence. Only paths are logged, never skill text.
		if _, err := fmt.Fprintf(journal, "%s move %q -> %q\n", time.Now().UTC().Format(time.RFC3339Nano), src, dst); err != nil {
			return err
		}
		if err := journal.Sync(); err != nil {
			return err
		}
		if err := moveSkill(src, dst); err != nil {
			return err
		}
		_, err := fmt.Fprintf(journal, "done %q -> %q\n", src, dst)
		return err
	}
	slugs := make([]string, 0, len(groups))
	for slug := range groups {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)
	for _, slug := range slugs {
		candidates := groups[slug]
		dest := filepath.Join(dir, slug)
		occupied := false
		if _, err := os.Lstat(dest); err == nil {
			fi, err := os.Stat(filepath.Join(dest, "SKILL.md"))
			if err == nil && fi.Mode().IsRegular() {
				candidates = append([]candidate{{dest, "local", fi.ModTime()}}, candidates...)
			} else {
				occupied = true // Unrelated directory/file: keep it intact.
			}
		} else if !os.IsNotExist(err) {
			failures = append(failures, err)
			continue
		}
		sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].mtime.After(candidates[j].mtime) })
		ok := true
		// Move losers first. If interrupted, the winner is still at its old
		// path and the completed backups are ordinary flat skills on retry.
		for i := len(candidates) - 1; i >= 0; i-- {
			c := candidates[i]
			if i == 0 && !occupied {
				if c.path != dest && ok {
					if err := move(c.path, dest); err != nil {
						failures = append(failures, err)
					}
				}
				continue
			}
			backup, err := migrationBackup(dir, slug, c.owner)
			if err == nil {
				err = move(c.path, backup)
			}
			if err != nil {
				failures = append(failures, err)
				ok = false
			}
		}
	}
	for _, owner := range owners {
		// Remove only empty containers, never supporting files or old locks.
		_ = os.Remove(owner)
	}
	if err := journal.Sync(); err != nil {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}

func migrationBackup(dir, slug, owner string) (string, error) {
	base := slug + "__from_" + owner
	for i := 1; ; i++ {
		name := base
		if i > 1 {
			name = fmt.Sprintf("%s__%d", base, i)
		}
		if !ValidSlug(name) {
			return "", fmt.Errorf("skill migrate: backup name too long: %s", name)
		}
		path := filepath.Join(dir, name)
		if _, err := os.Lstat(path); os.IsNotExist(err) {
			return path, nil
		} else if err != nil {
			return "", err
		}
	}
}

func moveSkill(src, dst string) error {
	if _, err := os.Lstat(dst); err == nil {
		return fmt.Errorf("skill migrate: destination exists: %s", dst)
	} else if !os.IsNotExist(err) {
		return err
	}
	fi, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		// Relative directory links must still refer to the same external skill
		// after promotion. Do not copy or modify their targets.
		target, err := filepath.EvalSymlinks(src)
		if err != nil {
			return err
		}
		target, err = filepath.Abs(target)
		if err != nil {
			return err
		}
		if err := os.Symlink(target, dst); err != nil {
			return err
		}
		if err := os.Remove(src); err != nil {
			_ = os.Remove(dst)
			return err
		}
		return nil
	}
	return os.Rename(src, dst)
}
