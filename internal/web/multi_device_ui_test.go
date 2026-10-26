package web

import (
	"strings"
	"testing"
)

// 多设备标识与账号配对（docs/dev/multi-device.md 定稿文案）：
// 设置·设备唯一主 CTA「添加设备」、认领走真 /api/account/claim、网关铅笔纯
// icon、解绑真 DELETE、底栏按入口语境、问候只写「在忙」。
func TestMultiDeviceUI(t *testing.T) {
	dev, _ := staticFS.ReadFile("static/devices.js")
	d := string(dev)
	for _, want := range []string{
		// 设置·设备：导语、标题行唯一主按钮、无官网行。
		`<span>添加设备</span>`,
		`绑定后，可在同一界面与各台电脑上的 Bot 聊天。记忆和文件仍在各自设备上。`,
		`<h3>已绑 · ${devs.length}</h3>`,
		// 认领：真 API（POST /api/account/claim → 服务端 bindIfFree + 上报）。
		`把这台电脑绑到账号？`, `绑定后，手机和其它入口也能看到这台电脑上的 Bot。`,
		`data-act="claim-dismiss">先不绑`, `${S.claimBusy ? "绑定中" : "绑定这台电脑"}`,
		`await api("POST", "/api/account/claim")`,
		// 网关：铅笔纯 icon，不再有文字「改」。
		`aria-label="改网关地址" data-act="self-gw-edit">${svg("pencil")}</button>`,
		// 解绑：真 DELETE，侧栏与列表立刻无该机。
		`<span>解除绑定</span>`, `await api("DELETE", "/api/devices/" + encodeURIComponent(S.target.id))`,
		// 在线定义小字；底栏按入口语境。
		`「在线」表示最近能连上该设备上的 XBot。官网入口不是设备，不会出现在此列表。`,
		`"已绑定 " + n + " 台设备"`,
	} {
		if !strings.Contains(d, want) {
			t.Errorf("devices.js missing %s", want)
		}
	}
	for _, avoid := range []string{`>配对设备<`, `>改</button>`, "刚做完", "g7-can"} {
		if strings.Contains(d, avoid) {
			t.Errorf("devices.js retains %s", avoid)
		}
	}
	pair, _ := staticFS.ReadFile("static/pairing.js")
	p := string(pair)
	for _, want := range []string{
		// 添加设备：说明为主、短码次要；无同权「我去连对方」输入。
		`<h2>添加设备</h2>`,
		`到新电脑上打开 XBot，用同一账号登录，并确认「把这台电脑绑到账号」。绑定后，这里就能看到那台电脑上的 Bot。`,
		`也可以让对方输入下方短码（次要）：`, `data-act="close-pair">完成`,
	} {
		if !strings.Contains(p, want) {
			t.Errorf("pairing.js missing %s", want)
		}
	}
	for _, avoid := range []string{"我去连对方", `id="g7-code"`, "g7-or", "submitJoin"} {
		if strings.Contains(p, avoid) {
			t.Errorf("pairing.js retains %s", avoid)
		}
	}
	app, _ := staticFS.ReadFile("static/app.js")
	a := string(app)
	greet := a[strings.Index(a, "function paintGreeting"):]
	greet = greet[:strings.Index(greet, "\n  }")]
	for _, want := range []string{
		// 问候状态只「有 N 个 Bot 在忙」，有忙才出。
		"在忙。", "greet-stack",
	} {
		if !strings.Contains(greet, want) {
			t.Errorf("paintGreeting missing %s", want)
		}
	}
	for _, avoid := range []string{"刚做完", "有什么事，直接说就行"} {
		if strings.Contains(greet, avoid) {
			t.Errorf("paintGreeting retains %s", avoid)
		}
	}
	for _, want := range []string{
		// 入口语境交给设备模块；开机认领。
		`window.xbotDevices.setSession({ remote: !!data.remote })`,
		`window.xbotDevices.offerClaim()`,
	} {
		if !strings.Contains(a, want) {
			t.Errorf("app.js missing %s", want)
		}
	}
	acct, _ := staticFS.ReadFile("static/account.js")
	ac := string(acct)
	for _, want := range []string{
		`把这台电脑绑到账号？`, `call("POST", "/api/account/claim")`, `state: () => (me && me.account) || null`,
	} {
		if !strings.Contains(ac, want) {
			t.Errorf("account.js missing %s", want)
		}
	}
}
