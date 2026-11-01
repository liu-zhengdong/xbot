package web

import (
	"net/http"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDesktopStatusAPI(t *testing.T) {
	a := newDeviceSide(t, "desktop-test", "deskbot")
	// Explicit missing dependency: authenticated status remains an inspectable
	// error, not a fabricated granted state; no OS prompt is requested.
	t.Setenv("XBOT_AGENT_DESKTOP", filepath.Join(t.TempDir(), "missing-cli"))
	var out struct {
		Platform string
		Legacy   bool
		Error    string
	}
	if code := a.call("GET", "/api/devices/desktop", "", &out); code != 200 {
		t.Fatal(code)
	}
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		if !strings.Contains(out.Error, "not installed") {
			t.Fatalf("%+v", out)
		}
	} else if !out.Legacy {
		t.Fatal(out)
	}
	res, err := http.Get(a.srv.URL + "/api/devices/desktop")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatal(res.StatusCode)
	}
}
