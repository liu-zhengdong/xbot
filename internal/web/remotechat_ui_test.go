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
		"index.html":     {want: []string{`<link rel="stylesheet" href="/remotechat.css">`, `<script src="/remotechat.js"></script>`}},
		"remotechat.js":  {want: chatContract(t), avoid: []string{"节点", "disabled", "D.chat = "}},
		"remotechat.css": {want: []string{`.rc { position: absolute; inset: 0;`, `.rc[hidden] { display: none; }`, `.rc-state .rc-hint {`, `.rc-error[hidden], .rc-error:empty { display: none; }`}},
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

// chatContract: what remotechat.js must keep. A second line under an owner
// bubble is 「没收到 <设备> 的回执」 (no_ack, online only), and a 4xx with
// error (409 已解除配对) says 没能完成：<error> above the composer instead of
// becoming a 「重试」 bubble or touching V.failed.
func chatContract(t *testing.T) []string {
	t.Helper()
	// One literal per line, as in the rest of this file: chatContract itself
	// has no raw string, so it cannot span from one literal to the next.
	return []string{
		`window.xbotRemoteChat = { open: open,`, // devices.js reads D.chat through a getter over this
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
		// 状态：no_ack 只在线时写，离线仍是「已排队」。
		`if (m.role === "me" && m.no_ack && online()) return "noack";`, `没收到 `, ` 的回执`,
		`<span class="rc-hint">可以在「设备」里看看它的状态</span>`,
		// 4xx 带 error：原文放回空着的输入框，上面一行「没能完成：」，不进 V.failed。
		`<div class="rc-error" id="rc-error" role="alert" hidden></div>`,
		`e.status >= 400 && e.status < 500 && msg`, `没能完成：`, `el.input.value = t; grow(); el.input.focus();`,
		`} else if (!V.failed.includes(t)) V.failed.push(t);`,
	}
}

// 409 已解除配对：原文回到输入框并在上面报错，不进 V.failed（重试弹窗）。
func TestRemoteChatUnpaired(t *testing.T) {
	data, err := staticFS.ReadFile("static/remotechat.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(data)
	start := strings.Index(js, "async function sendText(")
	if start < 0 {
		t.Fatal("sendText not found")
	}
	body := js[start:]
	catch := strings.Index(body, "} catch (e) {")
	client := strings.Index(body, "e.status >= 400 && e.status < 500 && msg")
	fail := strings.Index(body, "V.failed.push(t)")
	if catch < 0 || client < catch || fail < client {
		t.Fatalf("4xx must branch before V.failed.push: catch=%d client=%d fail=%d", catch, client, fail)
	}
	if !strings.Contains(body, `showError("没能完成：" + msg);`) {
		t.Error("4xx path must show 没能完成：<error>")
	}
}
