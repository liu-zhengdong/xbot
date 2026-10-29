package web

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"image/png"
	"io"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// Brand resources are served from the embedded static files.
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
		if typ == "image/png" {
			img, err := png.Decode(bytes.NewReader(got))
			if err != nil {
				t.Fatalf("%s: %v", path, err)
			}
			w, h := img.Bounds().Dx(), img.Bounds().Dy()
			wantAlpha := uint32(0)
			if path == "/icon-maskable-512.png" {
				wantAlpha = 0xffff
			}
			for _, p := range [][2]int{{0, 0}, {w - 1, 0}, {0, h - 1}, {w - 1, h - 1}, {w / 2, 0}} {
				if _, _, _, a := img.At(p[0], p[1]).RGBA(); a != wantAlpha {
					t.Errorf("%s: canvas alpha at %v = %d, want %d", path, p, a, wantAlpha)
				}
			}
			for _, p := range [][2]int{{w / 2, h / 4}, {w / 2, h / 2}, {w / 2, 3 * h / 4}} {
				if _, _, _, a := img.At(p[0], p[1]).RGBA(); a != 0xffff {
					t.Errorf("%s: panel must remain opaque at %v", path, p)
				}
			}
		}
	}
	var favicon struct {
		Image struct {
			Href string `xml:"href,attr"`
		} `xml:"image"`
	}
	svg, _ := staticFS.ReadFile("static/favicon.svg")
	if err := xml.Unmarshal(svg, &favicon); err != nil {
		t.Fatal(err)
	}
	embedded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(favicon.Image.Href, "data:image/png;base64,"))
	faviconPNG, _ := staticFS.ReadFile("static/favicon-32.png")
	if err != nil || !bytes.Equal(embedded, faviconPNG) {
		t.Fatal("SVG favicon must embed the same transparent PNG as favicon-32.png")
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
	if !strings.Contains(sw, `"/favicon.svg"`) {
		t.Error("the service worker keeps the new icons")
	}
	if !strings.Contains(manifest, `"purpose": "maskable"`) {
		t.Error("the manifest lists the maskable icon")
	}
	pkg, err := os.ReadFile("../../scripts/package-desktop.sh")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(pkg), "PIL") || !strings.Contains(string(pkg), `cp packaging/icons/XBot.ico "$work/XBot.ico"`) {
		t.Error("packaging copies the .ico and .icns instead of resizing")
	}
}
