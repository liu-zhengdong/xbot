package web

import (
	"os/exec"
	"strings"
	"testing"
)

func TestGatewayTextSpacing(t *testing.T) {
	raw, err := staticFS.ReadFile("static/devices.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	a := strings.Index(s, "  const sp =")
	b := strings.Index(s[a:], "\n")
	script := s[a:a+b] + `
 const assert=require('node:assert/strict');
 assert.equal(sp('连不上网关'+'xbot.babelgo.cn'),'连不上网关 xbot.babelgo.cn');
 assert.equal(sp('连不上网关'),'连不上网关');
 assert.equal(sp('等了3分钟，XBot已重连'),'等了 3 分钟，XBot 已重连');
 `
	if out, err := exec.Command("node", "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
