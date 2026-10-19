package upgrade

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

// A check that fails after the update was downloaded must not take the
// install button away: the package is still there and still good.
func TestFailedCheckKeepsTheDownloadedUpdate(t *testing.T) {
	usePlatform(t, "darwin", "arm64")
	useVersion(t, "v0.1.0")
	rel := newRelease(t)
	rel.publish(t, "v0.2.0", "XBot-0.2.0-macos-arm64.zip", buildZip(t, "new"))
	root := t.TempDir()

	u := NewUpdater(root)
	if err := u.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	// The network goes away after the download.
	os.Setenv("XBOT_UPGRADE_URL", "http://127.0.0.1:1/releases")
	if err := u.Check(context.Background()); err == nil {
		t.Fatal("the check against a dead endpoint succeeded")
	}
	st := u.Status()
	if st.State != StateReady {
		t.Fatalf("state = %q, want ready: a downloaded update stays installable", st.State)
	}
	if st.Latest != "v0.2.0" {
		t.Fatalf("Latest = %q, want v0.2.0", st.Latest)
	}
}

// A second click while a check is running is refused instead of racing it.
func TestInstallRefusesWhileChecking(t *testing.T) {
	usePlatform(t, "darwin", "arm64")
	u := NewUpdater(t.TempDir())
	u.mu.Lock()
	u.state = StateDownloading
	u.staged = "/tmp/whatever"
	u.mu.Unlock()
	if err := u.Install(); err == nil {
		t.Fatal("Install started while a download was under way")
	}
}

// A macOS build outside an app bundle gets its binary from the package, not
// the bundle directory.
func TestInstallOnMacWithoutABundle(t *testing.T) {
	usePlatform(t, "darwin", "arm64")
	base := t.TempDir()
	exe := filepath.Join(base, "xbot")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldExecutable := executable
	executable = func() (string, error) { return exe, nil }
	t.Cleanup(func() { executable = oldExecutable })

	staged := filepath.Join(base, "stage", "XBot.app")
	if err := os.MkdirAll(filepath.Join(staged, "Contents", "MacOS"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staged, "Contents", "MacOS", "XBot"), []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Install(staged); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(exe)
	if err != nil || string(got) != "new" {
		t.Fatalf("binary = %q, %v", got, err)
	}
}

// A newer release whose download fails leaves nothing to install, even though
// a package was staged a moment ago: the failure must not offer a button that
// cannot work.
func TestFailedRedownloadDoesNotClaimAReadyUpdate(t *testing.T) {
	usePlatform(t, "darwin", "arm64")
	useVersion(t, "v0.1.0")
	rel := newRelease(t)
	rel.publish(t, "v0.2.0", "XBot-0.2.0-macos-arm64.zip", buildZip(t, "new"))
	root := t.TempDir()

	u := NewUpdater(root)
	if err := u.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if u.Status().State != StateReady {
		t.Fatalf("state = %q, want ready", u.Status().State)
	}
	// A newer release appears, but its package cannot be downloaded: the
	// checksum entry points at bytes the server does not serve.
	rel.publish(t, "v0.3.0", "XBot-0.3.0-macos-arm64.zip", buildZip(t, "newer"))
	rel.assets["SHA256SUMS"] = []byte(strings.Repeat("0", 64) + "  XBot-0.3.0-macos-arm64.zip\n")
	if err := u.Check(context.Background()); err == nil {
		t.Fatal("the check accepted a package with a bad checksum")
	}
	st := u.Status()
	if st.State == StateReady {
		t.Fatalf("state = ready with nothing on disk (staged = %q)", st.Latest)
	}
	if Pending(root) != "" {
		t.Fatalf("a package survived: %q", Pending(root))
	}
	// And the button's action tells the truth instead of failing later.
	if err := u.Install(); err == nil {
		t.Fatal("Install claimed to work with nothing staged")
	}
}

// A check that finds a newer release whose package has no asset for this
// platform leaves the older staged package in place. The page must keep
// naming what it can install, not the release that failed.
func TestFailedCheckKeepsTheStagedVersionName(t *testing.T) {
	usePlatform(t, "darwin", "arm64")
	useVersion(t, "v0.1.0")
	rel := newRelease(t)
	rel.publish(t, "v0.2.0", "XBot-0.2.0-macos-arm64.zip", buildZip(t, "new"))
	root := t.TempDir()

	u := NewUpdater(root)
	if err := u.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if u.Status().Latest != "v0.2.0" {
		t.Fatalf("Latest = %q, want v0.2.0", u.Status().Latest)
	}
	// A newer release appears without a package this platform can install.
	rel.publish(t, "v0.3.0", "XBot-0.3.0-windows-amd64-setup.exe", []byte("nope"))
	if err := u.Check(context.Background()); err == nil {
		t.Fatal("the check accepted a release with no macOS asset")
	}
	st := u.Status()
	if st.State != StateReady {
		t.Fatalf("state = %q, want ready: the older package is still there", st.State)
	}
	if st.Latest != "v0.2.0" {
		t.Fatalf("Latest = %q, want v0.2.0: the page must name what it can install", st.Latest)
	}
}
