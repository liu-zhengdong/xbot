package web

import (
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"xbot/internal/browsertest"
)

// resourcesScript is the shipped resources.js, prepended to harness scripts
// that cut a slice out of app.js: appendInline and pathLabel both ask
// window.XBotResources whether a string is a readable path, so the slice needs
// the real thing rather than a stub.
func resourcesScript(t *testing.T) string {
	t.Helper()
	raw, err := staticFS.ReadFile("static/resources.js")
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// runPageInChrome writes the shipped page into a temp dir with the scripted
// chat fixture (chatStreamFixture) and the given driver appended before
// </body>, runs headless Chrome over it and fails unless the driver reports
// data-test-result="passed". Same harness as TestComposerLinksBrowser.
func runPageInChrome(t *testing.T, driver string) {
	t.Helper()
	chrome := browsertest.Chrome(t)
	var err error
	dir := t.TempDir()
	err = fs.WalkDir(staticFS, "static", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		dst := filepath.Join(dir, strings.TrimPrefix(path, "static"))
		if d.IsDir() {
			return os.MkdirAll(dst, 0700)
		}
		raw, err := staticFS.ReadFile(path)
		if err != nil {
			return err
		}
		if path == "static/index.html" {
			page := strings.ReplaceAll(string(raw), `src="/`, `src="./`)
			page = strings.ReplaceAll(page, `href="/`, `href="./`)
			page = strings.Replace(page, "<head>", "<head><script>"+chatStreamFixture+"</script>", 1)
			page = strings.Replace(page, "</body>", "<script>"+driver+"</script></body>", 1)
			raw = []byte(page)
		}
		return os.WriteFile(dst, raw, 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(chrome, "--headless=new", "--disable-gpu", "--no-sandbox", "--no-first-run", "--no-default-browser-check", "--allow-file-access-from-files", "--user-data-dir="+filepath.Join(t.TempDir(), "chrome"), "--virtual-time-budget=15000", "--dump-dom", (&url.URL{Scheme: "file", Path: filepath.Join(dir, "index.html")}).String()).CombinedOutput()
	if err != nil {
		t.Fatalf("Chrome: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), `data-test-result="passed"`) {
		result := string(out)
		if i := strings.Index(result, "data-test-result="); i >= 0 {
			result = result[i:min(i+1200, len(result))]
		} else if len(result) > 1500 {
			result = result[:1500]
		}
		t.Fatalf("page driver failed: %s", result)
	}
}
