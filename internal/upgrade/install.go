package upgrade

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// Install puts a staged package in place of what is running. The running copy
// keeps working: on macOS the bundle is renamed aside and the new one renamed
// in, and elsewhere the binary is written over the running one, which Unix
// allows. The new version takes effect on the next start.
//
// A failure leaves the old version exactly where it was.
func Install(staged string) error {
	if staged == "" {
		return errors.New("nothing staged")
	}
	if app := Bundle(); app != "" {
		return installBundle(staged, app)
	}
	exe, err := executable()
	if err != nil {
		return err
	}
	return installBinary(staged, exe)
}

// installBundle swaps a whole .app: the old one is renamed beside itself, the
// new one is renamed in, and the old one removed. If the second rename fails
// the old bundle goes straight back.
func installBundle(staged, app string) error {
	dir := filepath.Dir(app)
	aside := filepath.Join(dir, "."+filepath.Base(app)+".old")
	// Whatever an earlier run left behind is gone by now: the app that
	// created it has exited.
	if err := os.RemoveAll(aside); err != nil {
		return err
	}
	if err := os.Rename(app, aside); err != nil {
		return fmt.Errorf("cannot move %s aside: %w", app, err)
	}
	if err := os.Rename(staged, app); err != nil {
		if back := os.Rename(aside, app); back != nil {
			return fmt.Errorf("cannot install the new version (%v), and putting the old one back failed too: %w", err, back)
		}
		return fmt.Errorf("cannot install the new version: %w", err)
	}
	_ = os.RemoveAll(aside)
	return nil
}

// movingWaits are the pauses between tries at replacing a running binary. A
// virus scanner or a sync client can hold it open for a moment.
var movingWaits = []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond, 800 * time.Millisecond}

// installBinary writes the staged binary over the running one. Unix lets a
// running executable be replaced; Windows does not, and there the write fails
// and the caller is told to use the installer.
func installBinary(staged, exe string) error {
	data, err := os.ReadFile(staged)
	if err != nil {
		return err
	}
	tmp := filepath.Join(filepath.Dir(exe), "."+filepath.Base(exe)+".new")
	if err := os.WriteFile(tmp, data, 0o755); err != nil {
		return err
	}
	var rerr error
	for i := 0; ; i++ {
		if rerr = os.Rename(tmp, exe); rerr == nil {
			return nil
		}
		if runtime.GOOS == "windows" || i == len(movingWaits) {
			break
		}
		time.Sleep(movingWaits[i])
	}
	_ = os.Remove(tmp)
	if runtime.GOOS == "windows" {
		return fmt.Errorf("%s is in use by the running XBot: quit it first, or run the installer", filepath.Base(exe))
	}
	return rerr
}

// Pending returns the version staged and ready to install, or "".
func Pending(root string) string {
	dir := filepath.Join(root, StageDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name(), "manifest.json"))
		if err != nil {
			continue
		}
		var m manifest
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		if m.Version != "" {
			return m.Version
		}
	}
	return ""
}

// StagedPath returns the path to install from for the version staged under
// root, or "".
func StagedPath(root, version string) string {
	dir := stagePath(root, version)
	if _, err := os.Stat(filepath.Join(dir, "manifest.json")); err != nil {
		return ""
	}
	if app := filepath.Join(dir, "XBot.app"); exists(app) {
		return app
	}
	// Linux keeps the binary in a directory named after the release.
	var found string
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || found != "" {
			return nil
		}
		if d.Name() == "xbot" {
			found = p
		}
		return nil
	})
	return found
}

// ClearStaged removes every staged package under root.
func ClearStaged(root string) {
	_ = os.RemoveAll(filepath.Join(root, StageDir))
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
