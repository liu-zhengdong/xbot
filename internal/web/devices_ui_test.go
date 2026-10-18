package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 设备 is the first item in the 我 menu and opens a 440px right-side drawer in
// the v2 layout: lead copy, a grey gateway-down banner, 这台电脑, one card per
// paired device (status, Bot chips, address, the Bot 互发消息 switch), an
// online/离线 note, the 连接 section (局域网直连, 网关) and the two pairing buttons.
func TestDevicesDrawerUI(t *testing.T) {
	for file, contract := range map[string]struct{ want, avoid []string }{
		"index.html": {want: []string{
			"<div id=\"account-menu\" class=\"pop account-menu\" hidden>\n            <button id=\"account-devices\" type=\"button\">设备",
			`<link rel="stylesheet" href="/devices.css">`, `<script src="/devices.js"></script>`,
		}},
		"devices.js": {
			want: []string{
				// 说明、网关不通横幅（灰色，不用红色），重试用现有的 gw-retry
				"配对后，在这台电脑上就能和另一台电脑上的 Bot 聊天，两边的 Bot 也能互发消息。记忆和文件留在各自的电脑上。",
				`<span>连不上网关${sp(host(gw))}`, `data-act="gw-retry"`, "连不上网关，其他电脑的 Bot 暂时用不了。",
				// 这台电脑：系统 · N 个 Bot，铅笔进改自己名字的弹窗
				`data-act="rename-self"`, `self.os, (self.bots || []).length + " 个 Bot"`, "这台电脑的名字",
				"只对以后配对的设备生效。", `已经配对的${others.map((d) => " " + esc(d.label)).join("、")} 上，还显示原来的名字。`,
				// 已配对：状态、Bot 药丸、地址、按设备的能力
				`<h3>已配对 · ${devs.length}</h3>`, `<i class="status-dot ok"></i>在线`, `<i class="status-dot off-ring"></i>离线 · 最后联系 ${esc(seen)}`,
				`class="avatar xs"`, `<code class="g7-addr">名字@${esc(d.name)}</code>`, "还没收到它的 Bot 列表 · 几秒后会更新",
				`<b>Bot 互发消息</b><small>两边的 Bot 能互相发消息</small>`, `const on = d.bot_messaging !== false`,
				// 开关走 PATCH；失败时开关改回去并在卡片里报错
				`await api("PATCH", "/api/devices/" + encodeURIComponent(d.id), { bot_messaging: on })`, `d.bot_messaging = old; S.bmErr = d.id`,
				// 行菜单：重命名、解除配对；解除配对确认写「会尽量通知」
				`data-act="rename"`, `data-act="ask-unpair"`, `<span>解除配对</span>`,
				`会尽量通知 ${esc(t.label)}，它那边会显示「已解除配对」。通知没送到，这边也照样解除。`,
				// 对方解除了：灰色标记 +「移除」，直接删本机记录，不弹确认
				`已解除配对 · 对方解除的`, `data-act="remove" data-id="${esc(d.id)}"`, `case "remove":`,
				`if (d.unpaired) {`,
				// 关于「在线」的小字
				"「在线」指 16 分钟内收到过对方的 Bot 列表",
				// 连接：局域网直连（灰掉、默认关、公司网络可能不允许）、网关与「改」、编辑器、校验、检查中、502
				`<b>局域网直连</b>`, "公司网络可能不允许。", `aria-disabled="true"`,
				`data-act="self-gw-edit">${gw ? "改" : "设置"}`, `现在用：`, `data-act="self-gw-official">用官方网关</button>`,
				`function gwCheck(v, cur)`, "网关地址不对：要用 http:// 或 https:// 开头，后面跟主机名。", `正在检查 ${esc(host(normGw(v)))}`, `${S.checking ? "检查中" : "保存"}`,
				`api("PUT", "/api/devices/gateway", { gateway: S.gwVal })`, `S.selfGwErr = e.message`, `S.checking = true; render();`,
				// 已有配对设备时改网关先弹确认（原型 crop 13）
				`<h2>换网关后要重新配对</h2>`, `一台电脑只用一个网关。换成 ${esc(next)} 以后，下面这 ${devs.length} 台设备都要在新网关上重新配对，才能继续使用。`,
				"保存前会先检查新地址，连不上就还用原来的网关。", `data-act="gw-confirm-save">换网关`, `data-act="gw-confirm-cancel">取消`,
				// 空状态和底部按钮
				`<p>把你的另一台电脑配对进来，它上面的 Bot 会出现在侧栏里，点开就能聊天。</p>`, `<span>记忆和文件留在各自的电脑上。</span>`,
				`class="secondary" type="button" data-act="join-open">${svg("keyboard")}<span>加入另一台设备</span>`,
				`class="primary" type="button" data-act="add">${svg("plus")}<span>添加设备</span>`, `const pair = !!D.acts.add`,
				// 保留：抽屉、登录过期、焦点陷阱、pairing.js 用的导出
				`class="modal profile-drawer g7-drawer"`, `window.xbotDevices =`,
				`if (res.status === 401) { sessionExpired(); }`, `location.reload();`, `if (!authed) {`,
				`function trap(ev)`, `trap(ev);`,
			},
			avoid: []string{"最后在线", "节点", "同步", "即将推出", "G7 之后", "G7之后", "重命名这台电脑", "这些设备连在原来的网关上", `class="modal-actions g7-drawer-foot"`},
		},
		"devices.css": {want: []string{
			".g7-drawer .profile-modal { width: 440px;", ".g7-banner {", ".g7-dev-card {", ".g7-cap {",
			".g7-switch[aria-checked=\"true\"] { background: var(--primary); }", ".g7-set-edit { display: block; }", ".g7-layer:empty { display: none; }",
		}},
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
