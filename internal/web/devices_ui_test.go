package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 设备 is the first item in the 我 menu and opens a right-side drawer: this
// computer with an inline 网关 X 改 edit, paired devices with rename/unpair, and
// one gateway-down notice instead of per-device 离线 while the gateway is down.
func TestDevicesDrawerUI(t *testing.T) {
	for file, contract := range map[string]struct{ want, avoid []string }{
		"index.html": {want: []string{
			"<div id=\"account-menu\" class=\"pop account-menu\" hidden>\n            <button id=\"account-devices\" type=\"button\">设备",
			`<link rel="stylesheet" href="/devices.css">`, `<script src="/devices.js"></script>`,
		}},
		"devices.js": {
			want: []string{
				`"网关 " + esc(host(gw))`, `data-act="self-gw-edit">${gw ? "改" : "设置"}`,
				`api("PUT", "/api/devices/gateway", { gateway: S.gwVal })`, `S.selfGwErr = e.message`,
				"连不上网关，其他电脑的 Bot 暂时用不了。", `const status = !up ? "" : d.online`,
				`只改这台电脑上显示的名字`, `解除和 ${esc(t.label)} 的配对？`,
				`class="modal profile-drawer g7-drawer"`, `const pair = !!D.acts.add`,
			},
			avoid: []string{"记忆", "memory", "同步"},
		},
		"devices.css": {want: []string{".g7-dev-card.is-off .g7-dev-ico { color: var(--muted); }", ".g7-layer:empty { display: none; }"}},
	} {
		data, err := staticFS.ReadFile("static/" + file)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range contract.want {
			if !strings.Contains(string(data), want) {
				t.Errorf("%s missing %s", file, want)
			}
		}
		for _, avoid := range contract.avoid {
			if strings.Contains(string(data), avoid) {
				t.Errorf("%s retains %s", file, avoid)
			}
		}
	}
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
