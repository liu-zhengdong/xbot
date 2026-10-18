package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Remote bots: the sidebar groups other computers' bots by device (v2 侧栏 6–9,
// 16) and a row opens remotechat.js, a panel over .main that chats as the owner
// (remote-*, side-full). Offline never disables the composer.
func TestRemoteChatUI(t *testing.T) {
	for file, contract := range map[string]struct{ want, avoid []string }{
		"index.html": {want: []string{`<link rel="stylesheet" href="/remotechat.css">`, `<script src="/remotechat.js"></script>`}},
		"remotechat.js": {want: []string{
			`"/api/remote/" + encodeURIComponent(d) + "/bots/" + encodeURIComponent(b)`,
			`api("POST", path(d, b) + "/messages", { text: t })`, `api("POST", path(V.dev, V.bot) + "/read")`,
			`e.status === 502 && e.data && e.data.message`,
			`"在 " + label() + " 上"`, `"离线 · 最后联系 "`, `data-rc="info" aria-label="详情"`,
			`这台电脑和 ${esc(where)} 各存一份。`, `只在 ${esc(where)} 上。`, `在「设备」里查看`,
			`已排队 · 还在这台电脑上`, `已排队 · 等 ${esc(label())} 上线`, `没发出去`,
			`打开 XBot 后 ${esc(V.bot)} 才会收到并回复；最多等 7 天。`,
			`placeholder="${esc("写给 " + V.bot)}"`,
			`if (ev.key !== "Enter" || ev.shiftKey || ev.isComposing || ev.keyCode === 229) return;`,
			`const lines = (t) => esc(`, `setInterval(poll, POLL);`,
		}, avoid: []string{"节点", "disabled"}},
		"remotechat.css": {want: []string{`.rc { position: absolute; inset: 0;`, `.rc[hidden] { display: none; }`}},
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
	for path, ctype := range map[string]string{"/remotechat.js": "text/javascript", "/remotechat.css": "text/css"} {
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
