package web

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"xbot/internal/account"
)

// 认领本机把安装绑到登录着的账号：走真 bindIfFree + ReportDevice，落盘后
// 我的设备立刻列出这台电脑。隧道那头的请求没有属主证明，认领被拒。
func TestAccountClaimBindsThisMachine(t *testing.T) {
	e := newAcctEnv(t, false)
	tunnel := map[string]string{"Cf-Connecting-Ip": "203.0.113.9"}
	c := e.browser()
	// Sign up through the tunnel: the account exists, the install stays unbound.
	code, out := e.do(c, "POST", "/api/account/register", map[string]string{"email": "zhengdong@example.com", "password": "password 12"}, tunnel)
	if code != 200 || acct(out)["owner"] != false {
		t.Fatalf("register %d %v", code, out)
	}
	if _, me := e.do(c, "GET", "/api/account/me", nil, tunnel); acct(me)["unbound"] != true {
		t.Fatalf("me after tunnel sign-up %v", me)
	}
	// Claiming from the tunnel has no owner proof.
	if code, out := e.do(c, "POST", "/api/account/claim", nil, tunnel); code != http.StatusForbidden || out["error"] == nil {
		t.Fatalf("claim through tunnel %d %v", code, out)
	}
	if _, me := e.do(c, "GET", "/api/account/me", nil, nil); acct(me)["unbound"] != true {
		t.Fatalf("me before claim %v", me)
	}
	// Claiming from this computer binds for real: the binding file holds the
	// account key, and 我的设备 lists this machine with its bots at once.
	code, out = e.do(c, "POST", "/api/account/claim", nil, nil)
	if code != 200 || out["bound"] != true || acct(out)["owner"] != true || acct(out)["unbound"] != false {
		t.Fatalf("claim %d %v", code, out)
	}
	raw, err := os.ReadFile(filepath.Join(e.root, "account-binding.json"))
	if err != nil || !strings.Contains(string(raw), `"key"`) || strings.Contains(string(raw), "zhengdong") {
		t.Fatalf("binding file %q %v", raw, err)
	}
	code, devs := e.do(c, "GET", "/api/account/devices", nil, nil)
	list, _ := devs["devices"].([]any)
	if code != 200 || len(list) != 1 {
		t.Fatalf("devices after claim %d %v", code, devs)
	}
	d := list[0].(map[string]any)
	if d["self"] != true || d["online"] != true || len(d["bots"].([]any)) != 2 {
		t.Fatalf("self device after claim %v", d)
	}
	if code, _ := e.do(c, "GET", "/api/bots", nil, nil); code != 200 {
		t.Fatalf("owner interface after claim %d", code)
	}
	// A second account cannot claim an already bound install.
	other := e.browser()
	if code, _ := e.do(other, "POST", "/api/account/register", map[string]string{"email": "other@example.com", "password": "password 12"}, nil); code != 200 {
		t.Fatal("other register")
	}
	if code, out := e.do(other, "POST", "/api/account/claim", nil, nil); code != http.StatusForbidden || out["error"] == nil {
		t.Fatalf("claim when bound %d %v", code, out)
	}
}

// session 带上入口语境：经隧道/代理（官网入口）remote=true，本机窗口 false。
func TestSessionMarksRemoteEntry(t *testing.T) {
	e := newAcctEnv(t, false)
	c := e.browser()
	if code, out := e.do(c, "POST", "/api/account/register", map[string]string{"email": "zhengdong@example.com", "password": "password 12"}, nil); code != 200 {
		t.Fatalf("register %d %v", code, out)
	}
	e.claim(c, nil)
	tunnel := map[string]string{"X-Forwarded-For": "203.0.113.9"}
	if _, out := e.do(c, "GET", "/api/session", nil, nil); out["remote"] != false {
		t.Fatalf("local session %v", out)
	}
	if _, out := e.do(c, "GET", "/api/session", nil, tunnel); out["remote"] != true {
		t.Fatalf("tunnel session %v", out)
	}
}

type failingReport struct{ account.Client }

func (failingReport) Report(context.Context, string, string, string, []string) error {
	return errors.New("report down")
}

// 认领绑上了但账号设备登记失败：回包说没登记上，下一次页面请求重报，
// 不能装作账号列表里已经有这台电脑。
func TestAccountClaimSaysWhenReportFails(t *testing.T) {
	e := newAcctEnv(t, false)
	c := e.browser()
	if code, out := e.do(c, "POST", "/api/account/register", map[string]string{"email": "zhengdong@example.com", "password": "password 12"}, nil); code != 200 {
		t.Fatalf("register %d %v", code, out)
	}
	real := e.s.acct.client
	e.s.acct.client = failingReport{real}
	code, out := e.do(c, "POST", "/api/account/claim", nil, nil)
	if code != 200 || out["bound"] != true || out["reported"] != false {
		t.Fatalf("claim with report down %d %v", code, out)
	}
	if _, devs := e.do(c, "GET", "/api/account/devices", nil, nil); len(devs["devices"].([]any)) != 0 {
		t.Fatalf("devices while report down %v", devs)
	}
	// Report is back: the next page request reports without waiting a minute.
	e.s.acct.client = real
	e.do(c, "GET", "/api/account/me", nil, nil)
	if _, devs := e.do(c, "GET", "/api/account/devices", nil, nil); len(devs["devices"].([]any)) != 1 {
		t.Fatalf("devices after report recovers %v", devs)
	}
}
