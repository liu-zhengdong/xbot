// Package upgrade checks GitHub releases for a newer xbot and swaps the running
// binary in place. It never downgrades, and a "dev" build only moves when the
// caller forces it (the manual `xbot upgrade` command does).
package upgrade

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// DefaultURL is the GitHub API endpoint for the newest stable release.
const DefaultURL = "https://api.github.com/repos/liu-zhengdong/xbot/releases/latest"

// MaxDownload caps one release asset. A binary never needs more.
const MaxDownload = 200 << 20

// ErrNotNewer means there is nothing newer to apply.
var ErrNotNewer = errors.New("already up to date")

// executable locates the running binary with symlinks resolved. Tests replace it.
var executable = func() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", err
	}
	return resolved, nil
}

// goos and goarch select the release asset. Tests replace them.
var (
	goos   = runtime.GOOS
	goarch = runtime.GOARCH
)

// Asset is one file attached to a release.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

// Info is the result of a release check.
type Info struct {
	Current   string  // the version the caller reported
	Latest    string  // the newest release tag
	Available bool    // a strictly newer release exists
	Downgrade bool    // the release is older than current
	URL       string  // release page
	Assets    []Asset // attached files
}

// AssetURL returns the download URL for a named asset, or "".
func (i Info) AssetURL(name string) string {
	for _, a := range i.Assets {
		if a.Name == name {
			return a.URL
		}
	}
	return ""
}

// Check reports whether a newer release exists. A "dev" build is never offered
// an automatic upgrade.
func Check(ctx context.Context, current string) (Info, error) {
	return CheckForce(ctx, current, false)
}

// CheckForce is Check with an explicit force: a forced check treats a "dev"
// build as upgradable so the manual command still works.
func CheckForce(ctx context.Context, current string, force bool) (Info, error) {
	url := strings.TrimSpace(os.Getenv("XBOT_UPGRADE_URL"))
	if url == "" {
		url = DefaultURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Info{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	client := &http.Client{Timeout: 30 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return Info{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return Info{}, fmt.Errorf("release check: %s", res.Status)
	}
	var body struct {
		TagName string  `json:"tag_name"`
		HTMLURL string  `json:"html_url"`
		Assets  []Asset `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(&body); err != nil {
		return Info{}, err
	}
	latest := strings.TrimSpace(body.TagName)
	if latest == "" {
		return Info{}, errors.New("release check: no tag_name")
	}
	info := Info{Current: current, Latest: latest, URL: body.HTMLURL, Assets: body.Assets}
	if isDev(current) {
		info.Available = force
		if force {
			info.Downgrade = false
		}
		return info, nil
	}
	cmp, err := Compare(latest, current)
	if err != nil {
		return Info{}, err
	}
	info.Available = cmp > 0
	info.Downgrade = cmp < 0
	return info, nil
}

func isDev(v string) bool {
	v = strings.TrimSpace(v)
	return v == "" || v == "dev"
}

// Apply downloads the asset matching the running os/arch, verifies it against
// the release's SHA256SUMS, and atomically replaces the running binary. It
// returns the path it wrote. Nothing is touched unless the checksum matches.
func Apply(ctx context.Context, info Info) (string, error) {
	if !info.Available {
		return "", ErrNotNewer
	}
	name := fmt.Sprintf("xbot_%s_%s", goos, goarch)
	binURL := info.AssetURL(name)
	if binURL == "" {
		return "", fmt.Errorf("release %s has no asset %s", info.Latest, name)
	}
	sumsURL := info.AssetURL("SHA256SUMS")
	if sumsURL == "" {
		return "", fmt.Errorf("release %s has no SHA256SUMS", info.Latest)
	}
	target, err := executable()
	if err != nil {
		return "", err
	}
	sums, err := fetch(ctx, sumsURL, 1<<20)
	if err != nil {
		return "", err
	}
	want := checksumFor(string(sums), name)
	if want == "" {
		return "", fmt.Errorf("SHA256SUMS has no entry for %s", name)
	}
	data, err := fetch(ctx, binURL, MaxDownload)
	if err != nil {
		return "", err
	}
	got := sha256.Sum256(data)
	if !strings.EqualFold(hex.EncodeToString(got[:]), want) {
		return "", fmt.Errorf("checksum mismatch for %s", name)
	}
	return writeBinary(target, data)
}

// writeBinary writes data beside target and renames it over the file.
func writeBinary(target string, data []byte) (string, error) {
	dir := filepath.Dir(target)
	f, err := os.CreateTemp(dir, ".xbot-upgrade-*")
	if err != nil {
		return "", err
	}
	tmp := f.Name()
	cleanup := func() {
		_ = f.Close()
		_ = os.Remove(tmp)
	}
	if _, err := f.Write(data); err != nil {
		cleanup()
		return "", err
	}
	if err := f.Chmod(0o755); err != nil {
		cleanup()
		return "", err
	}
	if err := f.Sync(); err != nil {
		cleanup()
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return target, nil
}

// fetch GETs url and returns at most max bytes, refusing anything larger.
func fetch(ctx context.Context, url string, max int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 10 * time.Minute}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: %s", url, res.Status)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("download %s exceeds %d bytes", url, max)
	}
	return data, nil
}

// checksumFor finds the hash for name in sha256sum(1) output.
func checksumFor(sums, name string) string {
	for _, line := range strings.Split(sums, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		file := strings.TrimPrefix(fields[len(fields)-1], "*")
		if file == name {
			return fields[0]
		}
	}
	return ""
}

// Compare orders two semver strings. err is non-nil when either is malformed.
func Compare(a, b string) (int, error) {
	x, err := parseSemver(a)
	if err != nil {
		return 0, err
	}
	y, err := parseSemver(b)
	if err != nil {
		return 0, err
	}
	if c := cmpInt(x.major, y.major); c != 0 {
		return c, nil
	}
	if c := cmpInt(x.minor, y.minor); c != 0 {
		return c, nil
	}
	if c := cmpInt(x.patch, y.patch); c != 0 {
		return c, nil
	}
	return comparePre(x.pre, y.pre), nil
}

type semver struct {
	major, minor, patch int
	pre                 string
}

func parseSemver(s string) (semver, error) {
	orig := s
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}
	var pre string
	if i := strings.IndexByte(s, '-'); i >= 0 {
		pre = s[i+1:]
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return semver{}, fmt.Errorf("invalid version %q", orig)
	}
	nums := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return semver{}, fmt.Errorf("invalid version %q", orig)
		}
		nums[i] = n
	}
	return semver{major: nums[0], minor: nums[1], patch: nums[2], pre: pre}, nil
}

func comparePre(a, b string) int {
	if a == b {
		return 0
	}
	if a == "" {
		return 1
	}
	if b == "" {
		return -1
	}
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		x, y := as[i], bs[i]
		xn, xerr := strconv.Atoi(x)
		yn, yerr := strconv.Atoi(y)
		switch {
		case xerr == nil && yerr == nil:
			if c := cmpInt(xn, yn); c != 0 {
				return c
			}
		case xerr == nil:
			return -1 // numeric identifiers rank lower than alphanumeric
		case yerr == nil:
			return 1
		default:
			if c := strings.Compare(x, y); c != 0 {
				return c
			}
		}
	}
	return cmpInt(len(as), len(bs))
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
