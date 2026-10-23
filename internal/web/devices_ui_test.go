package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDeviceAssets(t *testing.T) {
	srv := httptest.NewServer(muxOf(&Server{Root: t.TempDir()}))
	defer srv.Close()
	for path, ctype := range map[string]string{"/devices.js": "text/javascript", "/devices.css": "text/css"} {
		res, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 200 || !strings.HasPrefix(res.Header.Get("Content-Type"), ctype) {
			t.Errorf("%s: %d %s", path, res.StatusCode, res.Header.Get("Content-Type"))
		}
	}
}
