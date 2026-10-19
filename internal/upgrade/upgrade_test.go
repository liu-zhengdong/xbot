package upgrade

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"xbot/internal/version"
)

// useVersion makes the running build report v, which decides whether a staged
// package is newer than it.
func useVersion(t *testing.T, v string) {
	t.Helper()
	old := version.Version
	version.Version = v
	t.Cleanup(func() { version.Version = old })
}

// buildZip makes a macOS-style package holding XBot.app/Contents/MacOS/XBot.
func buildZip(t *testing.T, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range map[string]string{
		"XBot.app/Contents/MacOS/XBot": body,
		"XBot.app/Contents/Info.plist": "plist",
	} {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate}
		h.SetMode(0o755)
		f, err := w.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// buildTarGz makes a Linux-style package holding xbot-<v>-linux-amd64/xbot.
func buildTarGz(t *testing.T, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	h := &tar.Header{Name: "xbot-0.2.0-linux-amd64/xbot", Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(h); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// release serves the feed and its assets, and points XBOT_UPGRADE_URL at them.
type release struct {
	server *httptest.Server
	assets map[string][]byte
	tags   []string
}

func newRelease(t *testing.T) *release {
	t.Helper()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	r := &release{server: srv, assets: map[string][]byte{}}
	mux.HandleFunc("/releases", func(w http.ResponseWriter, req *http.Request) {
		var out []string
		for _, tag := range r.tags {
			var assets []string
			for name, content := range r.assets {
				if name == "SHA256SUMS" {
					assets = append(assets, fmt.Sprintf(`{"name":%q,"browser_download_url":%q,"size":%d}`,
						name, srv.URL+"/asset/"+name, len(content)))
					continue
				}
				assets = append(assets, fmt.Sprintf(`{"name":%q,"browser_download_url":%q,"size":%d}`,
					name, srv.URL+"/asset/"+name, len(content)))
			}
			out = append(out, fmt.Sprintf(`{"tag_name":%q,"html_url":%q,"body":"notes for %s","draft":false,"prerelease":true,"assets":[%s]}`,
				tag, srv.URL+"/page", tag, strings.Join(assets, ",")))
		}
		fmt.Fprint(w, "["+strings.Join(out, ",")+"]")
	})
	mux.HandleFunc("/asset/", func(w http.ResponseWriter, req *http.Request) {
		name := strings.TrimPrefix(req.URL.Path, "/asset/")
		data, ok := r.assets[name]
		if !ok {
			http.NotFound(w, req)
			return
		}
		_, _ = w.Write(data)
	})
	mux.HandleFunc("/page", func(w http.ResponseWriter, req *http.Request) { fmt.Fprint(w, "release page") })
	t.Cleanup(func() { os.Unsetenv("XBOT_UPGRADE_URL") })
	return r
}

// publish adds a tag with one package asset and the release's SHA256SUMS.
func (r *release) publish(t *testing.T, tag, asset string, data []byte) {
	t.Helper()
	r.tags = append(r.tags, tag)
	r.assets[asset] = data
	sums := ""
	for name, content := range r.assets {
		digest := sha256.Sum256(content)
		sums += fmt.Sprintf("%s  %s\n", hex.EncodeToString(digest[:]), name)
	}
	r.assets["SHA256SUMS"] = []byte(sums)
	os.Setenv("XBOT_UPGRADE_URL", r.server.URL+"/releases")
}

// usePlatform pins the package selection for one test.
func usePlatform(t *testing.T, g, a string) {
	t.Helper()
	oldOS, oldArch := goos, goarch
	goos, goarch = g, a
	t.Cleanup(func() { goos, goarch = oldOS, oldArch })
}

// The single "latest release" endpoint never answers with a pre-release, and
// every xbot release is one: the feed has to be the release list.
func TestDefaultFeedIsTheReleaseList(t *testing.T) {
	if !strings.Contains(DefaultURL, "/releases?") {
		t.Fatalf("DefaultURL = %q, want the release list: the latest-release endpoint skips pre-releases", DefaultURL)
	}
}

func TestCheckOffersPrereleases(t *testing.T) {
	usePlatform(t, "darwin", "arm64")
	rel := newRelease(t)
	rel.publish(t, "v0.3.1-beta.6", "XBot-0.3.1-beta.6-macos-arm64.zip", buildZip(t, "new"))

	info, err := Check(context.Background(), "v0.3.1-beta.5")
	if err != nil {
		t.Fatal(err)
	}
	if !info.Available || info.Latest != "v0.3.1-beta.6" {
		t.Fatalf("Available=%v Latest=%q, want a newer pre-release", info.Available, info.Latest)
	}
	if info.Notes == "" {
		t.Fatal("release notes were dropped")
	}
}

// GitHub lists releases by creation time; the newest tag is not the first one.
func TestNewestIsHighestTag(t *testing.T) {
	got, ok := newest([]Release{
		{Tag: "v0.3.1-beta.6"},
		{Tag: "v0.3.1-beta.10"},
		{Tag: "v0.3.1-beta.2"},
	})
	if !ok || got.Tag != "v0.3.1-beta.10" {
		t.Fatalf("newest = %q, want v0.3.1-beta.10", got.Tag)
	}
}

func TestDraftAndUnparsableTagsSkipped(t *testing.T) {
	got, ok := newest([]Release{
		{Tag: "v0.3.1", Draft: true},
		{Tag: "nightly"},
		{Tag: "v0.2.0"},
	})
	if !ok || got.Tag != "v0.2.0" {
		t.Fatalf("newest = %q, want v0.2.0", got.Tag)
	}
}

func TestAssetNameFollowsTheRelease(t *testing.T) {
	for _, c := range []struct{ goos, goarch, want string }{
		{"darwin", "arm64", "XBot-0.3.1-macos-arm64.zip"},
		{"darwin", "amd64", "XBot-0.3.1-macos-amd64.zip"},
		{"linux", "amd64", "xbot-0.3.1-linux-amd64.tar.gz"},
		{"linux", "arm64", ""},
		{"windows", "amd64", ""},
	} {
		goos, goarch = c.goos, c.goarch
		if got := AssetName("v0.3.1"); got != c.want {
			t.Errorf("%s/%s: AssetName = %q, want %q", c.goos, c.goarch, got, c.want)
		}
	}
	if got := DownloadAsset("v0.3.1"); got != "XBot-0.3.1-windows-amd64-setup.exe" {
		t.Errorf("windows: DownloadAsset = %q", got)
	}
}

func TestStageUnpacksTheAppBundle(t *testing.T) {
	usePlatform(t, "darwin", "arm64")
	rel := newRelease(t)
	rel.publish(t, "v0.2.0", "XBot-0.2.0-macos-arm64.zip", buildZip(t, "new-binary"))
	root := t.TempDir()

	info, err := Check(context.Background(), "v0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	staged, err := Stage(context.Background(), info, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(staged, "Contents", "MacOS", "XBot"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new-binary" {
		t.Fatalf("staged binary = %q", got)
	}
	if v := Pending(root); v != "v0.2.0" {
		t.Fatalf("Pending = %q, want v0.2.0", v)
	}
}

func TestStageUnpacksTheLinuxTarball(t *testing.T) {
	usePlatform(t, "linux", "amd64")
	rel := newRelease(t)
	rel.publish(t, "v0.2.0", "xbot-0.2.0-linux-amd64.tar.gz", buildTarGz(t, "new-binary"))
	root := t.TempDir()

	info, err := Check(context.Background(), "v0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	staged, err := Stage(context.Background(), info, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(staged) != "xbot" {
		t.Fatalf("staged = %q, want the xbot binary", staged)
	}
	if got := StagedPath(root, "v0.2.0"); got != staged {
		t.Fatalf("StagedPath = %q, want %q", got, staged)
	}
}

func TestStageRefusesABadChecksum(t *testing.T) {
	usePlatform(t, "darwin", "arm64")
	rel := newRelease(t)
	rel.publish(t, "v0.2.0", "XBot-0.2.0-macos-arm64.zip", buildZip(t, "new-binary"))
	// A release that advertises a hash its package does not have.
	rel.assets["SHA256SUMS"] = []byte(strings.Repeat("0", 64) + "  XBot-0.2.0-macos-arm64.zip\n")
	root := t.TempDir()

	info, _ := Check(context.Background(), "v0.1.0")
	if _, err := Stage(context.Background(), info, root, nil); err == nil {
		t.Fatal("Stage accepted a package whose checksum does not match")
	}
	if entries, err := os.ReadDir(filepath.Join(root, StageDir)); err == nil && len(entries) != 0 {
		t.Fatalf("a refused package was left behind: %v", entries)
	}
}

func TestStageRefusesAnUnsafePath(t *testing.T) {
	usePlatform(t, "linux", "amd64")
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := "evil"
	h := &tar.Header{Name: "../../etc/xbot", Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(h); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gz.Close()

	rel := newRelease(t)
	rel.publish(t, "v0.2.0", "xbot-0.2.0-linux-amd64.tar.gz", buf.Bytes())
	root := t.TempDir()
	info, _ := Check(context.Background(), "v0.1.0")
	if _, err := Stage(context.Background(), info, root, nil); err == nil {
		t.Fatal("Stage accepted an entry outside its own directory")
	}
}

func TestInstallReplacesTheBundle(t *testing.T) {
	usePlatform(t, "darwin", "arm64")
	base := t.TempDir()
	app := filepath.Join(base, "XBot.app")
	if err := os.MkdirAll(filepath.Join(app, "Contents", "MacOS"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldExe := filepath.Join(app, "Contents", "MacOS", "XBot")
	if err := os.WriteFile(oldExe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldExecutable := executable
	executable = func() (string, error) { return oldExe, nil }
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
	got, err := os.ReadFile(oldExe)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Fatalf("bundle not replaced: %q", got)
	}
	// Nothing of the swap is left beside the app.
	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			t.Fatalf("left %s beside the app", e.Name())
		}
	}
}

// A swap that cannot finish must put the old bundle back where it was.
func TestInstallPutsTheBundleBackOnFailure(t *testing.T) {
	usePlatform(t, "darwin", "arm64")
	base := t.TempDir()
	app := filepath.Join(base, "XBot.app")
	if err := os.MkdirAll(filepath.Join(app, "Contents", "MacOS"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldExe := filepath.Join(app, "Contents", "MacOS", "XBot")
	if err := os.WriteFile(oldExe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldExecutable := executable
	executable = func() (string, error) { return oldExe, nil }
	t.Cleanup(func() { executable = oldExecutable })

	// A staged bundle that does not exist makes the second rename fail.
	if err := Install(filepath.Join(base, "missing", "XBot.app")); err == nil {
		t.Fatal("Install succeeded with nothing staged")
	}
	got, err := os.ReadFile(oldExe)
	if err != nil || string(got) != "old" {
		t.Fatalf("the old bundle was not put back: %q, %v", got, err)
	}
}

func TestNewUpdaterRestoresAPendingVersion(t *testing.T) {
	usePlatform(t, "darwin", "arm64")
	root := t.TempDir()
	rel := newRelease(t)
	rel.publish(t, "v9.9.9", "XBot-9.9.9-macos-arm64.zip", buildZip(t, "new"))
	info, _ := Check(context.Background(), "v9.9.8")
	if _, err := Stage(context.Background(), info, root, nil); err != nil {
		t.Fatal(err)
	}

	useVersion(t, "v9.9.8")
	u := NewUpdater(root)
	st := u.Status()
	if st.State != StateReady || st.Latest != "v9.9.9" {
		t.Fatalf("restored state = %q latest = %q, want ready v9.9.9", st.State, st.Latest)
	}
}

func TestNewUpdaterDropsAnOlderStagedVersion(t *testing.T) {
	usePlatform(t, "darwin", "arm64")
	root := t.TempDir()
	rel := newRelease(t)
	rel.publish(t, "v0.0.1", "XBot-0.0.1-macos-arm64.zip", buildZip(t, "old"))
	info, _ := Check(context.Background(), "v0.0.0")
	if _, err := Stage(context.Background(), info, root, nil); err != nil {
		t.Fatal(err)
	}

	// The running build is newer than what a previous run left staged.
	useVersion(t, "v0.0.2")
	u := NewUpdater(root)
	if got := u.Status().State; got != StateIdle {
		t.Fatalf("state = %q, want idle: a staged version older than the running one is no use", got)
	}
	if Pending(root) != "" {
		t.Fatal("the stale package was kept")
	}
}

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v0.2.0", "v0.1.0", 1},
		{"v0.1.0", "v0.2.0", -1},
		{"v1.0.0", "v1.0.0", 0},
		{"v1.0.0", "v1.0.0-rc1", 1},
		{"v1.0.0-rc2", "v1.0.0-rc1", 1},
		{"v0.3.1-beta.10", "v0.3.1-beta.6", 1},
		{"v0.3.1-beta.2", "v0.3.1-beta.10", -1},
		{"0.1.0", "v0.1.0", 0},
	}
	for _, c := range cases {
		got, err := Compare(c.a, c.b)
		if err != nil {
			t.Fatalf("Compare(%q,%q): %v", c.a, c.b, err)
		}
		if got != c.want {
			t.Fatalf("Compare(%q,%q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
	if _, err := Compare("dev", "v0.1.0"); err == nil {
		t.Fatal("Compare accepted dev")
	}
}

func TestDevNeverAutoUpgradesUnlessForced(t *testing.T) {
	usePlatform(t, "darwin", "arm64")
	rel := newRelease(t)
	rel.publish(t, "v0.2.0", "XBot-0.2.0-macos-arm64.zip", buildZip(t, "new"))

	info, err := Check(context.Background(), "dev")
	if err != nil {
		t.Fatal(err)
	}
	if info.Available {
		t.Fatal("a dev build was offered an automatic upgrade")
	}
	forced, err := CheckForce(context.Background(), "dev", true)
	if err != nil {
		t.Fatal(err)
	}
	if !forced.Available {
		t.Fatal("a forced check did not offer the release")
	}
}

func TestOlderAndEqualAreNotOffered(t *testing.T) {
	usePlatform(t, "darwin", "arm64")
	rel := newRelease(t)
	rel.publish(t, "v0.1.0", "XBot-0.1.0-macos-arm64.zip", buildZip(t, "new"))

	for _, current := range []string{"v0.1.0", "v0.2.0"} {
		info, err := Check(context.Background(), current)
		if err != nil {
			t.Fatal(err)
		}
		if info.Available {
			t.Fatalf("current %s: offered %s", current, info.Latest)
		}
		if info.Downgrade != (current == "v0.2.0") {
			t.Fatalf("current %s: Downgrade=%v", current, info.Downgrade)
		}
	}
}

func TestStageRefusesUnsupportedPlatforms(t *testing.T) {
	usePlatform(t, "windows", "amd64")
	rel := newRelease(t)
	rel.publish(t, "v0.2.0", "XBot-0.2.0-windows-amd64-setup.exe", []byte("setup"))
	info, _ := Check(context.Background(), "v0.1.0")
	if _, err := Stage(context.Background(), info, t.TempDir(), nil); err != ErrUnsupported {
		t.Fatalf("Stage on Windows = %v, want ErrUnsupported", err)
	}
	if got := info.AssetURL(DownloadAsset(info.Latest)); got == "" {
		t.Fatalf("Windows has no installer to offer: %q", DownloadAsset(info.Latest))
	}
}
