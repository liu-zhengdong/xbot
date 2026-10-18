package web

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// The portrait whitelist, the pinned DiceBear versions and the vendored bundle
// decide every bot's face. Changing any of them re-rolls all faces, so they are
// frozen here. To change them on purpose: get the owner's approval, rebuild
// with scripts/avatar (npm ci && npm run build && node golden.js --update) and
// update these hashes in the same PR.
const (
	avatarBundleSHA  = "fc2f09ce119fd3810162ad3e96579de30f53bcea7b26cdfb07d307ec59a64d72"
	avatarOptionsSHA = "5e00525b7d85eca4da9786894f9d6916a75480925bd6c1e73747e29015c64cc2"
	avatarGoldenSHA  = "3e5d4960ac82908827ba6a8a9fcc1e0a631bcb2475cfb9534b5024cfa82e754e"
)

func fileSHA(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func TestAvatarBundleAndWhitelistAreFrozen(t *testing.T) {
	for path, want := range map[string]string{
		"static/vendor/xbot-avatar.min.js": avatarBundleSHA,
		"../../scripts/avatar/options.js":  avatarOptionsSHA,
		"../../scripts/avatar/golden.json": avatarGoldenSHA,
	} {
		if got := fileSHA(t, path); got != want {
			t.Errorf("%s changed (sha256 %s, frozen %s): this re-rolls every bot's face", path, got, want)
		}
	}
}

func TestAvatarDependenciesArePinned(t *testing.T) {
	raw, err := os.ReadFile("../../scripts/avatar/package.json")
	if err != nil {
		t.Fatal(err)
	}
	var pkg struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(raw, &pkg); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"@dicebear/core": "9.4.2", "@dicebear/notionists": "9.4.2"}
	if len(pkg.Dependencies) != len(want) {
		t.Fatalf("dependencies = %v, want exactly %v (no @dicebear/collection)", pkg.Dependencies, want)
	}
	for name, v := range want {
		if pkg.Dependencies[name] != v {
			t.Errorf("%s = %q, want exact %q", name, pkg.Dependencies[name], v)
		}
	}
	for name, v := range pkg.DevDependencies {
		if strings.ContainsAny(v, "^~<>*x ") {
			t.Errorf("devDependency %s = %q is not pinned", name, v)
		}
	}
	lock, err := os.ReadFile("../../scripts/avatar/package-lock.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{`"node_modules/@dicebear/core": {
      "version": "9.4.2"`, `"node_modules/@dicebear/notionists": {
      "version": "9.4.2"`} {
		if !strings.Contains(string(lock), s) {
			t.Errorf("package-lock.json does not pin %s", s)
		}
	}
}

func TestAvatarBundleIsServedWithLicenses(t *testing.T) {
	mux := http.NewServeMux()
	(&Server{}).Register(mux)
	for path, ctype := range map[string]string{
		"/vendor/xbot-avatar.min.js": "text/javascript",
		"/vendor/LICENSES.txt":       "text/plain",
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), ctype) {
			t.Fatalf("GET %s = %d %q", path, rec.Code, rec.Header().Get("Content-Type"))
		}
	}
	lic, err := staticFS.ReadFile("static/vendor/LICENSES.txt")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"MIT License", "Florian Körner", "CC0 1.0", "Zoish", "heyzoish.gumroad.com/l/notionists"} {
		if !strings.Contains(string(lic), s) {
			t.Errorf("LICENSES.txt is missing %q", s)
		}
	}
}
