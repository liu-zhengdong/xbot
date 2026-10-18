package web

import (
	"bytes"
	"io"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// The XBot mark replaces the green icon and the text 「X」 placeholders. The
// small icons are pixel-tuned files served as they are.
func TestBrandMark(t *testing.T) {
	s := &Server{Root: t.TempDir(), Owner: "u1"}
	srv := httptest.NewServer(muxOf(s))
	defer srv.Close()
	for path, typ := range map[string]string{
		"/favicon.svg":           "image/svg+xml",
		"/favicon-32.png":        "image/png",
		"/apple-touch-icon.png":  "image/png",
		"/icon-maskable-512.png": "image/png",
		"/icon-192.png":          "image/png",
		"/icon-512.png":          "image/png",
	} {
		res, err := srv.Client().Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		got, _ := io.ReadAll(res.Body)
		res.Body.Close()
		want, _ := staticFS.ReadFile("static" + path)
		if res.StatusCode != 200 || !strings.HasPrefix(res.Header.Get("Content-Type"), typ) || len(want) == 0 || !bytes.Equal(got, want) {
			t.Errorf("%s: %d %q (%d bytes)", path, res.StatusCode, res.Header.Get("Content-Type"), len(got))
		}
	}
	read := func(name string) string {
		b, err := staticFS.ReadFile("static/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	html, css, sw, manifest := read("index.html"), read("app.css"), read("sw.js"), read("manifest.webmanifest")
	for _, s := range []string{
		`<link rel="icon" href="/favicon.svg" type="image/svg+xml">`,
		`<link rel="apple-touch-icon" href="/apple-touch-icon.png">`,
		`<svg class="appbar-logo" viewBox="0 0 16 16" aria-hidden="true"><mask id="xb-appbar-cut">`,
		`<svg viewBox="0 0 100 100"><path d="M16 20 L40 50 L16 80 M84 20 L60 50 L84 80"`,
	} {
		if !strings.Contains(html, s) {
			t.Errorf("index.html is missing %q", s)
		}
	}
	if strings.Contains(html, `aria-hidden="true">X</span>`) || strings.Contains(html, `font-family="Mona Sans VF, sans-serif">X</text>`) {
		t.Error("the text X placeholders are gone from the titlebar and the login page")
	}
	if !strings.Contains(css, ".mark { width: 48px; height: 48px; color: var(--text); }") || strings.Contains(css, "font-size: 10px; font-weight: 700; line-height: 1;\n}") {
		t.Error("the login mark is single-color and the titlebar logo is the cut-out square")
	}
	if !strings.Contains(sw, `const CACHE = "xbot-ui-v11";`) || !strings.Contains(sw, `"/favicon.svg"`) {
		t.Error("the service worker cache moves to v11 (avatar bundle) and keeps the new icons")
	}
	if !strings.Contains(manifest, `"purpose": "maskable"`) {
		t.Error("the manifest lists the maskable icon")
	}
	pkg, err := os.ReadFile("../../scripts/package-desktop.sh")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(pkg), "PIL") || !strings.Contains(string(pkg), `cp packaging/icons/XBot.ico "$work/XBot.ico"`) {
		t.Error("packaging copies the hand-tuned .ico and .icns instead of resizing")
	}
}
