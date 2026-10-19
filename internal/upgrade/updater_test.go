package upgrade

import (
	"context"
	"os"
	"testing"
)

// A check that finds a newer release downloads it and stops at ready: the
// owner's later click has nothing left to wait for.
func TestCheckDownloadsAndLeavesItReady(t *testing.T) {
	usePlatform(t, "darwin", "arm64")
	useVersion(t, "v0.1.0")
	rel := newRelease(t)
	rel.publish(t, "v0.2.0", "XBot-0.2.0-macos-arm64.zip", buildZip(t, "new-binary"))
	root := t.TempDir()

	u := NewUpdater(root)
	if err := u.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	st := u.Status()
	if st.State != StateReady || st.Latest != "v0.2.0" {
		t.Fatalf("state=%q latest=%q, want ready v0.2.0", st.State, st.Latest)
	}
	if st.Current != "v0.1.0" {
		t.Fatalf("Current = %q", st.Current)
	}
	if Pending(root) != "v0.2.0" {
		t.Fatalf("nothing staged: Pending = %q", Pending(root))
	}
}

// Nothing newer means nothing to do, and no download.
func TestCheckWithoutANewerReleaseStaysIdle(t *testing.T) {
	usePlatform(t, "darwin", "arm64")
	useVersion(t, "v0.2.0")
	rel := newRelease(t)
	rel.publish(t, "v0.2.0", "XBot-0.2.0-macos-arm64.zip", buildZip(t, "new-binary"))
	root := t.TempDir()

	u := NewUpdater(root)
	if err := u.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := u.Status().State; got != StateIdle {
		t.Fatalf("state = %q, want idle", got)
	}
	if _, err := os.Stat(root + "/" + StageDir); !os.IsNotExist(err) {
		t.Fatal("an up-to-date build downloaded a package")
	}
}

// A platform that installs with its own installer is told so, and offered the
// file rather than a restart.
func TestCheckReportsManualPlatform(t *testing.T) {
	usePlatform(t, "windows", "amd64")
	useVersion(t, "v0.1.0")
	rel := newRelease(t)
	rel.publish(t, "v0.2.0", "XBot-0.2.0-windows-amd64-setup.exe", []byte("setup"))

	u := NewUpdater(t.TempDir())
	if err := u.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	st := u.Status()
	if st.State != StateManual {
		t.Fatalf("state = %q, want manual", st.State)
	}
	if st.Download == "" {
		t.Fatal("no installer offered")
	}
}

// A failed check says why, and the next one may try again.
func TestFailedCheckReportsTheReason(t *testing.T) {
	usePlatform(t, "darwin", "arm64")
	useVersion(t, "v0.1.0")
	os.Setenv("XBOT_UPGRADE_URL", "http://127.0.0.1:1/releases")
	t.Cleanup(func() { os.Unsetenv("XBOT_UPGRADE_URL") })

	u := NewUpdater(t.TempDir())
	if err := u.Check(context.Background()); err == nil {
		t.Fatal("a check against a dead endpoint succeeded")
	}
	if got := u.Status(); got.State != StateError || got.Error == "" {
		t.Fatalf("state=%q error=%q, want an error with its reason", got.State, got.Error)
	}
}

// Installing with nothing downloaded is refused, not a panic.
func TestInstallWithoutADownload(t *testing.T) {
	usePlatform(t, "darwin", "arm64")
	u := NewUpdater(t.TempDir())
	if err := u.Install(); err != ErrNothingStaged {
		t.Fatalf("Install = %v, want ErrNothingStaged", err)
	}
}

// Installing what was downloaded starts the process again exactly once.
func TestInstallRelaunches(t *testing.T) {
	usePlatform(t, "darwin", "arm64")
	useVersion(t, "v0.1.0")
	rel := newRelease(t)
	rel.publish(t, "v0.2.0", "XBot-0.2.0-macos-arm64.zip", buildZip(t, "new"))
	root := t.TempDir()

	// The running binary sits in a bundle of its own, which the install
	// replaces.
	base := t.TempDir()
	app := base + "/XBot.app"
	if err := os.MkdirAll(app+"/Contents/MacOS", 0o755); err != nil {
		t.Fatal(err)
	}
	oldExe := app + "/Contents/MacOS/XBot"
	if err := os.WriteFile(oldExe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldExeFn := executable
	executable = func() (string, error) { return oldExe, nil }
	t.Cleanup(func() { executable = oldExeFn })

	u := NewUpdater(root)
	if err := u.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	relaunched := 0
	u.Relaunch = func() { relaunched++ }
	if err := u.Install(); err != nil {
		t.Fatal(err)
	}
	if relaunched != 1 {
		t.Fatalf("relaunch called %d times, want 1", relaunched)
	}
	if Pending(root) != "" {
		t.Fatal("the installed package was left on disk")
	}
}
