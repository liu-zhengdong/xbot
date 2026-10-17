package web

import (
	"strings"
	"testing"
)

func TestLoginSessionCopy(t *testing.T) {
	for file, wants := range map[string][]string{
		"static/index.html": {
			`id="login-form" class="gate-card" novalidate`,
			`id="modal-form" class="modal-card" novalidate`,
			"登录码", "启动 XBot 时终端里会打印 login token", "autofocus",
		},
		"static/app.js": {
			"先填登录码", "登录码不对，重新复制终端里 login token 那一行",
			"登录已过期，重新输入登录码", "连不上 XBot", "XBot 出错了，稍后再试",
			"res.status === 401 && authenticated", "sessionStorage.setItem",
			"sessionStorage.getItem", `"xbot-draft:" + (selection.type === "group" ? "group:" : "") + selection.id`, "closeModal();", "closePops();", "closeStp();",
		},
	} {
		data, err := staticFS.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range wants {
			if !strings.Contains(string(data), want) {
				t.Errorf("%s missing %q", file, want)
			}
		}
	}
}
