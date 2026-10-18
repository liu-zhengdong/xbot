package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// The Worker's FAVICON_SVG is the same mark the Go gateway embeds.
func TestFaviconSameInWorker(t *testing.T) {
	src, err := os.ReadFile("../../gateway-worker/src/favicon.ts")
	if err != nil {
		t.Fatal(err)
	}
	_, lit, ok := strings.Cut(string(src), "export const FAVICON_SVG = ")
	lit = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(lit), ";"))
	var got string
	if !ok || json.Unmarshal([]byte(lit), &got) != nil || got != string(faviconSVG) {
		t.Fatal("gateway-worker/src/favicon.ts differs from internal/gateway/favicon.svg")
	}
	if !strings.HasPrefix(got, "<svg") {
		t.Fatal("favicon is not an SVG")
	}
}

func TestFaviconHeaders(t *testing.T) {
	srv := httptest.NewServer((&Server{}).Handler())
	defer srv.Close()
	res, err := http.Head(srv.URL + "/favicon.ico")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "image/svg+xml" || res.Header.Get("Cache-Control") != "public, max-age=86400" {
		t.Fatalf("%d %v", res.StatusCode, res.Header)
	}
}
