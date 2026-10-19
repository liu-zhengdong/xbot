package upgrade

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
		// The package is the bundle beside the app being replaced, so the
		// swap stays inside one filesystem.
		local, err := bundleBeside(staged, app)
		if err != nil {
			return err
		}
		defer os.RemoveAll(filepath.Dir(local))
		return installBundle(local, app)
	}
	exe, err := executable()
	if err != nil {
		return err
	}
	if goos == "darwin" {
		// A macOS build outside a bundle was made from source or installed
		// by hand: the release ships an app bundle, and only its binary is
		// any use here.
		bin := filepath.Join(staged, "Contents", "MacOS", "XBot")
		if !exists(bin) {
			return fmt.Errorf("%s is not an app bundle; download the release from %s", filepath.Base(exe), releasePage)
		}
		return installBinary(bin, exe)
	}
	return installBinary(staged, exe)
}

// releasePage is where a build that cannot install the package itself sends
// the owner.
const releasePage = "https://github.com/liu-zhengdong/xbot/releases"

// bundleBeside copies the staged bundle into a directory beside the app being
// replaced, so the rename onto it stays inside one filesystem: the data root
// may well sit on another volume.
func bundleBeside(staged, app string) (string, error) {
	dir := filepath.Dir(app)
	tmp, err := os.MkdirTemp(dir, ".xbot-update-")
	if err != nil {
		return "", fmt.Errorf("cannot write beside %s: %w", app, err)
	}
	local := filepath.Join(tmp, filepath.Base(app))
	if err := copyTree(staged, local); err != nil {
		os.RemoveAll(tmp)
		return "", err
	}
	return local, nil
}

// copyTree copies a directory, keeping each file's mode.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(src, p)
		if rerr != nil {
			return rerr
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		f, oerr := os.Open(p)
		if oerr != nil {
			return oerr
		}
		defer f.Close()
		out, cerr := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
		if cerr != nil {
			return cerr
		}
		if _, werr := io.Copy(out, f); werr != nil {
			out.Close()
			return werr
		}
		return out.Close()
	})
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
	// A rename over a running executable works on Unix, and the running
	// process keeps the version it started with. Windows never takes this
	// path: it has no asset xbot installs by itself (see AssetName).
	if err := os.Rename(tmp, exe); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("cannot replace %s: %w", filepath.Base(exe), err)
	}
	return nil
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
