package upgrade

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// newBundle serves a release with one binary asset and SHA256SUMS.
func newBundle(t *testing.T, tag string, binary []byte) (*httptest.Server, Info) {
	t.Helper()
	sum := sha256.Sum256(binary)
	sums := fmt.Sprintf("%s  xbot_linux_amd64\n", hex.EncodeToString(sum[:]))
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		d := fmt.Sprintf(`{"tag_name":%q,"html_url":%q,"assets":[
			{"name":"xbot_linux_amd64","browser_download_url":%q},
			{"name":"SHA256SUMS","browser_download_url":%q}]}`,
			tag, srv.URL, srv.URL+"/bin", srv.URL+"/sums")
		fmt.Fprint(w, d)
	})
	mux.HandleFunc("/bin", func(w http.ResponseWriter, r *http.Request) { w.Write(binary) })
	mux.HandleFunc("/sums", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, sums) })
	return srv, Info{Latest: tag, URL: srv.URL}
}

func targetBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "xbot")
	if err := os.WriteFile(p, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := executable
	executable = func() (string, error) { return p, nil }
	oldOS, oldArch := goos, goarch
	goos, goarch = "linux", "amd64"
	t.Cleanup(func() { executable = old; goos, goarch = oldOS, oldArch })
	return p
}

func TestCheckAndApplyUpgrade(t *testing.T) {
	srv, _ := newBundle(t, "v0.2.0", []byte("new-binary"))
	t.Setenv("XBOT_UPGRADE_URL", srv.URL+"/releases/latest")
	target := targetBinary(t)

	info, err := Check(context.Background(), "v0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if !info.Available || info.Downgrade {
		t.Fatalf("Available=%v Downgrade=%v, want true/false", info.Available, info.Downgrade)
	}
	path, err := Apply(context.Background(), info)
	if err != nil {
		t.Fatal(err)
	}
	if path != target {
		t.Fatalf("applied %q, want %q", path, target)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new-binary" {
		t.Fatalf("binary = %q, want new-binary", got)
	}
}

func TestApplyChecksumMismatchLeavesBinary(t *testing.T) {
	// The advertised checksum does not match the served bytes.
	mux := http.NewServeMux()
	bad := httptest.NewServer(mux)
	t.Cleanup(bad.Close)
	mux.HandleFunc("/bin", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("new-binary")) })
	mux.HandleFunc("/sums", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%064d  xbot_linux_amd64\n", 1)
	})
	target := targetBinary(t)

	info := Info{Latest: "v0.2.0", Available: true, Assets: []Asset{
		{Name: "xbot_linux_amd64", URL: bad.URL + "/bin"},
		{Name: "SHA256SUMS", URL: bad.URL + "/sums"},
	}}
	if _, err := Apply(context.Background(), info); err == nil {
		t.Fatal("Apply accepted a bad checksum")
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old" {
		t.Fatalf("binary touched: %q", got)
	}
	// No stray temp files beside the target.
	entries, err := os.ReadDir(filepath.Dir(target))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("left %d files in target dir, want 1", len(entries))
	}
}

func TestOlderAndEqualAreNotApplied(t *testing.T) {
	srv, _ := newBundle(t, "v0.1.0", []byte("new-binary"))
	t.Setenv("XBOT_UPGRADE_URL", srv.URL+"/releases/latest")
	target := targetBinary(t)

	for _, current := range []string{"v0.1.0", "v0.2.0"} {
		info, err := Check(context.Background(), current)
		if err != nil {
			t.Fatal(err)
		}
		if info.Available {
			t.Fatalf("current %s: Available=true, want false", current)
		}
		if info.Downgrade != (current == "v0.2.0") {
			t.Fatalf("current %s: Downgrade=%v", current, info.Downgrade)
		}
		if _, err := Apply(context.Background(), info); err == nil {
			t.Fatalf("current %s: Apply applied a non-newer release", current)
		}
	}
	got, _ := os.ReadFile(target)
	if string(got) != "old" {
		t.Fatalf("binary touched: %q", got)
	}
}

func TestDevNeverAutoUpgradesUnlessForced(t *testing.T) {
	srv, _ := newBundle(t, "v0.2.0", []byte("new-binary"))
	t.Setenv("XBOT_UPGRADE_URL", srv.URL+"/releases/latest")

	info, err := Check(context.Background(), "dev")
	if err != nil {
		t.Fatal(err)
	}
	if info.Available {
		t.Fatal("dev build auto-upgraded")
	}
	forced, err := CheckForce(context.Background(), "dev", true)
	if err != nil {
		t.Fatal(err)
	}
	if !forced.Available {
		t.Fatal("forced dev check did not offer the release")
	}
}

func TestMissingAssetIsRefused(t *testing.T) {
	srv, _ := newBundle(t, "v0.2.0", []byte("new-binary"))
	t.Setenv("XBOT_UPGRADE_URL", srv.URL+"/releases/latest")
	target := targetBinary(t)

	info := Info{Latest: "v0.2.0", Available: true}
	if _, err := Apply(context.Background(), info); err == nil {
		t.Fatal("Apply accepted a release with no assets")
	}
	got, _ := os.ReadFile(target)
	if string(got) != "old" {
		t.Fatalf("binary touched: %q", got)
	}
}

func TestFetchRefusesOversize(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	mux.HandleFunc("/big", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(make([]byte, 64))
	})
	if _, err := fetch(context.Background(), srv.URL+"/big", 32); err == nil {
		t.Fatal("fetch accepted a body larger than the cap")
	}
	if _, err := fetch(context.Background(), srv.URL+"/big", 64); err != nil {
		t.Fatalf("fetch refused a body at the cap: %v", err)
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
