package web

import (
	"strings"
	"testing"

	"xbot/internal/gateway"
)

// Pairing modals follow the designer's copy: the scope line, A's
// 「X 想连到这台电脑」 with 拒绝/允许 and a 2-minute auto-decline, the reinstall
// card (拒绝 on the left, 替换旧的 X primary), and B's four failure states; a
// typo is caught locally with the same alphabet as the Go code. Escape cancels
// like the visible button, a late code POST never reopens a cancelled modal,
// and a "denied" decide answer is not shown as success.
func TestPairingModalsUI(t *testing.T) {
	data, err := staticFS.ReadFile("static/pairing.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(data)
	for _, want := range []string{
		`const ALPHA = "` + gateway.CodeAlphabet + `"`,
		"配对后，两台电脑能互相使用对方的 Bot。", "保存并生成配对码",
		`${esc(a.name)} 想连到这台电脑`, "后自动拒绝",
		`data-d="deny">拒绝</button><span class="spacer"></span>`, `data-d="add">作为新设备添加`, `class="primary" type="button" data-act="decide" data-d="replace">替换旧的 ${esc(c.name)}`,
		"这个配对码有一位不对", "配对码不对或已用过", "这个配对码已经过期，在另一台电脑上点「重新生成」。",
		`连不上网关 ${esc(host(gw))}`, `data-act="gw-open">改网关地址`, `const bad = ph === "typo" || ph === "expired" || ph === "wrong_or_used";`,
		// 429: 操作太频繁 on its own line, code not red, no 改网关地址; name_full likewise.
		`const rateLimited = "操作太频繁，等一分钟再试", nameFull = "同名设备已满 9 台，先解除或改名旧的，或选择替换旧的";`,
		"rate_limited: `<p class=\"error g7-msg\">${rateLimited}</p>`", "name_full: `<p class=\"error g7-msg\">${nameFull}</p>`",
		`const retry = ph === "gateway_unreachable" || ph === "rate_limited" || ph === "offline";`,
		`S.pairing.b = { phase: "none" }`,                                                                        // A 429 ends a retry instead of leaving the code disabled.
		`S.joinErr = e.message;`, `catch (e) { el.disabled = false; S.fieldErr = e.message; render(); return; }`, // 连接's 429/409 keeps the server copy.
		`if (c.length === 12 && !codeOK(c)) S.pairing.b = { phase: "typo" }`,
		`<span>网关 ${esc(host(gw))}</span><button class="text-btn g7-gw-edit" type="button" data-act="gw-open">改</button>`,
		`if (now === seen) return;`,
		"const words = up.match(/[A-Z0-9]+/g) || [];", // Pasted prose never joins the code (same as ParseShortCode).
		// Escape = the visible cancel button, routed through the same act() paths.
		`if (ev.key === "Escape" && Object.prototype.hasOwnProperty.call(D.views, S.view))`,
		`S.view === "add" ? "cancel-code" : ["join", "confirm", "waiting", "success-b"].includes(S.view) ? "close-join" : S.view === "request" ? "decide" : ""`, "else closeModal(); // add-gw, denied, success-a",
		`dataset: S.view === "request" ? { d: "deny" } : {}`,
		// newCode and cancel-code are serialized: a late POST never re-opens the modal.
		"let codeGen = 0, codeRun = null;", `if (gen !== codeGen) { api("DELETE", "/api/devices/code").catch(() => {}); return; }`,
		"++codeGen;", "if (codeRun) await codeRun.catch(() => {});", `case "cancel-code": await cancelCode(); break;`,
		// 允许 clicked after A's window expired comes back 200 + denied: don't claim success.
		`const phase = (S.pairing.a || {}).phase;`, `else if (phase === "done") { S.view = "success-a"; S.code = ""; await refresh(); }`,
		`if (d === "deny" || phase === "denied") { S.view = null; S.code = ""; if (phase === "denied") refresh(); }`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("pairing.js missing %s", want)
		}
	}
	for _, avoid := range []string{"记忆", "memory", "同步", "点开就能对话"} {
		if strings.Contains(js, avoid) {
			t.Errorf("pairing.js retains %s", avoid)
		}
	}
	html, _ := staticFS.ReadFile("static/index.html")
	if !strings.Contains(string(html), "<script src=\"/devices.js\"></script>\n<script src=\"/pairing.js\"></script>") {
		t.Error("pairing.js must load after devices.js")
	}
}
