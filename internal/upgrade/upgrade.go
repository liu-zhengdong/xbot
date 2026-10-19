// Package upgrade keeps xbot current. The newest GitHub release of
// liu-zhengdong/xbot is the source: its assets carry one package per platform,
// each checksummed in that release's SHA256SUMS.
//
// A check looks up the newest release, including the pre-releases this project
// ships; a "dev" build only moves when the caller forces it, so a development
// machine does not replace itself. The package for this platform is downloaded
// beside what it replaces and unpacked there, so installing it is a rename on
// one volume. Nothing is written into place until the checksum matches and the
// package holds what the platform installs.
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
	"runtime"
	"strconv"
	"strings"
	"time"
)

// DefaultURL lists the newest releases, pre-releases included. The single
// "latest release" endpoint is not used: it never answers with a pre-release,
// and every xbot release so far is one.
const DefaultURL = "https://api.github.com/repos/liu-zhengdong/xbot/releases?per_page=30"

// MaxDownload caps one release asset. A desktop package never needs more.
const MaxDownload = 200 << 20

// StageDir is the workspace a package is unpacked in. It sits inside the data
// root, not beside the app: replacing an app bundle needs a rename, and a
// bundle in /Applications is usually not the user's to write, while the data
// root always is.
const StageDir = "update"

// ErrNotNewer means there is nothing newer to apply.
var ErrNotNewer = errors.New("already up to date")

// goos and goarch select the package. Tests replace them.
var (
	goos   = runtime.GOOS
	goarch = runtime.GOARCH
)

// Asset is one file attached to a release.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

// Info is the result of a release check.
type Info struct {
	Current   string  // the version the caller reported
	Latest    string  // the newest release tag
	Available bool    // a strictly newer release exists
	Downgrade bool    // the release is older than current
	URL       string  // release page
	Notes     string  // release notes in Markdown
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

// AssetSize returns the announced size of a named asset, or 0 when the
// release does not say.
func (i Info) AssetSize(name string) int64 {
	for _, a := range i.Assets {
		if a.Name == name {
			return a.Size
		}
	}
	return 0
}

// Check reports whether a newer release exists. A "dev" build is never offered
// an automatic upgrade.
func Check(ctx context.Context, current string) (Info, error) {
	return CheckForce(ctx, current, false)
}

// CheckForce is Check with an explicit force: a forced check treats a "dev"
// build as upgradable so the manual command still works.
func CheckForce(ctx context.Context, current string, force bool) (Info, error) {
	rels, err := listReleases(ctx)
	if err != nil {
		return Info{}, err
	}
	info := Info{Current: current}
	rel, ok := newest(rels)
	if !ok {
		return info, errors.New("release check: no published release")
	}
	info.Latest, info.URL, info.Notes, info.Assets = rel.Tag, rel.URL, rel.Notes, rel.Assets
	if isDev(current) {
		info.Available = force
		return info, nil
	}
	cmp, err := Compare(rel.Tag, current)
	if err != nil {
		return Info{}, err
	}
	info.Available = cmp > 0
	info.Downgrade = cmp < 0
	return info, nil
}

// Release is one published release, as the check needs it.
type Release struct {
	Tag      string
	URL      string
	Notes    string
	Assets   []Asset
	Draft    bool
	Prelease bool
}

// newest picks the highest semver tag among the published releases. GitHub
// lists releases by creation time, which is not the same order: a patch cut
// after a beta sorts before it.
func newest(rels []Release) (Release, bool) {
	var best Release
	found := false
	for _, r := range rels {
		if r.Draft {
			continue
		}
		if _, err := parseSemver(r.Tag); err != nil {
			continue
		}
		if !found {
			best, found = r, true
			continue
		}
		if cmp, err := Compare(r.Tag, best.Tag); err == nil && cmp > 0 {
			best = r
		}
	}
	return best, found
}

// listReleases gets the release feed. XBOT_UPGRADE_URL points it elsewhere,
// which tests use.
func listReleases(ctx context.Context) ([]Release, error) {
	url := strings.TrimSpace(os.Getenv("XBOT_UPGRADE_URL"))
	if url == "" {
		url = DefaultURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	client := &http.Client{Timeout: 30 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("release check: %s", res.Status)
	}
	var body []struct {
		TagName    string  `json:"tag_name"`
		HTMLURL    string  `json:"html_url"`
		Body       string  `json:"body"`
		Draft      bool    `json:"draft"`
		Prerelease bool    `json:"prerelease"`
		Assets     []Asset `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(&body); err != nil {
		return nil, err
	}
	out := make([]Release, 0, len(body))
	for _, b := range body {
		tag := strings.TrimSpace(b.TagName)
		if tag == "" {
			continue
		}
		out = append(out, Release{Tag: tag, URL: b.HTMLURL, Notes: b.Body, Assets: b.Assets, Draft: b.Draft, Prelease: b.Prerelease})
	}
	return out, nil
}

func isDev(v string) bool {
	v = strings.TrimSpace(v)
	return v == "" || v == "dev"
}

// AssetName is the release asset this platform installs from, or "" when xbot
// cannot replace itself here: Windows has an installer that does the job, and
// no package is built for other architectures.
func AssetName(tag string) string {
	short := strings.TrimPrefix(strings.TrimSpace(tag), "v")
	switch {
	case goos == "darwin" && (goarch == "arm64" || goarch == "amd64"):
		// The app bundle. The disk image only serves a first install:
		// unpacking a zip needs no hdiutil.
		return fmt.Sprintf("XBot-%s-macos-%s.zip", short, goarch)
	case goos == "linux" && goarch == "amd64":
		return fmt.Sprintf("xbot-%s-linux-amd64.tar.gz", short)
	}
	return ""
}

// DownloadAsset is the file a platform that cannot install by itself offers
// instead: the Windows installer.
func DownloadAsset(tag string) string {
	short := strings.TrimPrefix(strings.TrimSpace(tag), "v")
	switch {
	case goos == "windows" && (goarch == "arm64" || goarch == "amd64"):
		return fmt.Sprintf("XBot-%s-windows-%s-setup.exe", short, goarch)
	}
	return ""
}

// download fetches url into dst, refusing more than max bytes and reporting
// progress as it reads.
func download(ctx context.Context, url, dst string, max int64, progress func(done, total int64)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 10 * time.Minute}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: %s", url, res.Status)
	}
	total := res.ContentLength
	if total > max {
		return fmt.Errorf("download %s exceeds %d bytes", url, max)
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()
	var done int64
	buf := make([]byte, 64<<10)
	for {
		n, rerr := res.Body.Read(buf)
		if n > 0 {
			done += int64(n)
			if done > max {
				return fmt.Errorf("download %s exceeds %d bytes", url, max)
			}
			if _, werr := f.Write(buf[:n]); werr != nil {
				return werr
			}
			if progress != nil {
				progress(done, total)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return rerr
		}
	}
	return f.Sync()
}

// fetchBytes gets url and returns at most max bytes, refusing anything larger.
func fetchBytes(ctx context.Context, url string, max int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 2 * time.Minute}
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

// sumAsset is the checksum list every release carries.
const sumAsset = "SHA256SUMS"

// verifyChecksum checks file against the release's SHA256SUMS entry for name.
func verifyChecksum(ctx context.Context, info Info, name, file string) error {
	sumsURL := info.AssetURL(sumAsset)
	if sumsURL == "" {
		return fmt.Errorf("release %s has no %s", info.Latest, sumAsset)
	}
	sums, err := fetchBytes(ctx, sumsURL, 1<<20)
	if err != nil {
		return err
	}
	want := checksumFor(string(sums), name)
	if want == "" {
		return fmt.Errorf("%s has no entry for %s", sumAsset, name)
	}
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, want) {
		return fmt.Errorf("checksum mismatch for %s", name)
	}
	return nil
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
