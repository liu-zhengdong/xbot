package web

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// TestCJKFullFontFallback pins the two-face arrangement: a full Noto Sans SC
// face named "XBot SC" so rarer characters (names, user text, Traditional)
// never fall back to the OS CJK font, declared BEFORE the subset face so the
// browser tries the subset first and only downloads the full file on a gap.
func TestCJKFullFontFallback(t *testing.T) {
	mux := http.NewServeMux()
	(&Server{}).Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/fonts/NotoSansSC-full.woff2", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("full font route: status %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "font/woff2" {
		t.Fatalf("full font Content-Type = %q, want font/woff2", ct)
	}
	if body := rec.Body.Bytes(); len(body) == 0 || string(body[:4]) != "wOF2" {
		t.Fatalf("full font body: %d bytes, want a non-empty woff2", len(body))
	}

	faces := fontFaces(t)
	full, subset := -1, -1
	for i, f := range faces {
		if !strings.Contains(f.src, `"/fonts/`) {
			continue
		}
		if strings.Contains(f.src, "NotoSansSC-full.woff2") {
			full = i
		}
		if strings.Contains(f.src, "NotoSansSC-subset.woff2") {
			subset = i
		}
	}
	if full < 0 || subset < 0 {
		t.Fatalf("app.css must declare both Noto Sans SC faces (full=%d subset=%d)", full, subset)
	}
	if full > subset {
		t.Error("the full face must be declared before the subset face; browsers try overlapping faces in reverse declaration order")
	}
	for _, i := range []int{full, subset} {
		f := faces[i]
		if f.family != `"XBot SC"` {
			t.Errorf("face %d family = %s, want \"XBot SC\"", i, f.family)
		}
		if f.weight != "400 700" {
			t.Errorf("face %d font-weight = %q, both faces need 400 700 so weight matching treats them alike", i, f.weight)
		}
		if !strings.Contains(f.body, "font-display: swap") {
			t.Errorf("face %d needs font-display: swap", i)
		}
	}
	for _, want := range []string{"U+2E80-4DBF", "U+4E00-9FFF", "U+F900-FAFF", "U+20000-2FA1F"} {
		if !strings.Contains(faces[full].body, want) {
			t.Errorf("full face unicode-range missing %s", want)
		}
	}
}

func TestCJKFontCSSNeverUsesLocalOrYaHei(t *testing.T) {
	mux := http.NewServeMux()
	(&Server{}).Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/app.css", nil))
	css := rec.Body.String()

	if strings.Contains(css, "YaHei") {
		t.Error("app.css must never name Microsoft YaHei")
	}
	if strings.Contains(css, "Noto Sans Backtick Fix") {
		t.Error("app.css still has the placeholder \"Noto Sans Backtick Fix\" entry")
	}
	for _, f := range fontFacesBody(css) {
		if strings.Contains(f, "local(") {
			t.Errorf("@font-face with local() serves an installed static weight instead of the bundled variable font: %s", f)
		}
	}
	if !strings.Contains(css, `.avatar text { font-family: "Mona Sans VF", "XBot SC",`) {
		t.Error(".avatar text must start with \"Mona Sans VF\", \"XBot SC\"")
	}
}

func TestIndexPreloadsSubsetFont(t *testing.T) {
	mux := http.NewServeMux()
	(&Server{}).Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	html := rec.Body.String()
	if !strings.Contains(html, `<link rel="preload" href="/fonts/NotoSansSC-subset.woff2" as="font" type="font/woff2" crossorigin>`) {
		t.Error("index.html must preload the CJK subset font")
	}
	if strings.Contains(html, "NotoSansSC-full.woff2") {
		t.Error("the full font is large and on-demand only; do not preload it")
	}

	sw, err := staticFS.ReadFile("static/sw.js")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(sw), "NotoSansSC-full.woff2") {
		t.Error("sw.js must not precache the full font")
	}
}

type fontFace struct {
	body, family, src, weight string
}

// fontFaces returns every @font-face block in the served app.css.
func fontFaces(t *testing.T) []fontFace {
	t.Helper()
	raw, err := staticFS.ReadFile("static/app.css")
	if err != nil {
		t.Fatal(err)
	}
	faces := fontFacesBody(string(raw))
	if len(faces) == 0 {
		t.Fatal("app.css has no @font-face rules")
	}
	out := make([]fontFace, 0, len(faces))
	reFamily := regexp.MustCompile(`font-family:\s*([^;]+);`)
	reSrc := regexp.MustCompile(`src:\s*([^;]+);`)
	reWeight := regexp.MustCompile(`font-weight:\s*([^;]+);`)
	for _, f := range faces {
		face := fontFace{body: f}
		if m := reFamily.FindStringSubmatch(f); m != nil {
			face.family = strings.TrimSpace(m[1])
		}
		if m := reSrc.FindStringSubmatch(f); m != nil {
			face.src = m[1]
		}
		if m := reWeight.FindStringSubmatch(f); m != nil {
			face.weight = strings.TrimSpace(m[1])
		}
		out = append(out, face)
	}
	return out
}

// fontFacesBody splits CSS into the bodies of its @font-face rules.
func fontFacesBody(css string) []string {
	var out []string
	for i := 0; ; {
		start := strings.Index(css[i:], "@font-face")
		if start < 0 {
			return out
		}
		start += i
		open := strings.Index(css[start:], "{")
		if open < 0 {
			return out
		}
		open += start
		end := strings.Index(css[open:], "}")
		if end < 0 {
			return out
		}
		end += open
		out = append(out, css[open+1:end])
		i = end + 1
	}
}
