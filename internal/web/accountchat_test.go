package web

import (
	"context"
	"strings"
	"testing"

	"xbot/internal/account"
)

// 账号里另一台电脑的 Bot 对话：路由按账号设备解析（本机不算），代理到那台电脑
// 自己的用户 API，历史按 owner/bot 两个角色回给面板；开关关着的时候没有这些路由。
func TestAccountDeviceChatProxy(t *testing.T) {
	t.Setenv("XBOT_ACCOUNT_CHAT", "1")
	e, c, local, session := boundAccountBrowser(t)
	target, err := e.s.acct.client.BindDevice(context.Background(), session, account.DeviceReport{ID: "desk", Name: "工作电脑", Bots: []string{"writer"}})
	if err != nil {
		t.Fatal(err)
	}
	var calls []string
	var calls2 []string
	e.s.acct.chatDo = func(ctx context.Context, sess, device, method, path string, body []byte) (int, []byte, string, error) {
		calls = append(calls, sess+"|"+device+"|"+method+" "+path)
		calls2 = append(calls2, string(body))
		switch {
		case method == "GET" && strings.HasSuffix(path, "/chat?limit=200"):
			return 200, []byte(`{"messages":[
				{"id":3,"kind":"owner","time":"2026-11-04T12:00:00Z","text":"在吗"},
				{"id":4,"kind":"bot","time":"2026-11-04T12:00:05Z","text":"在。"},
				{"id":5,"kind":"tool","time":"2026-11-04T12:00:06Z","text":"   "}]}`), "application/json", nil
		case method == "POST" && strings.HasSuffix(path, "/messages"):
			return 200, []byte("event: start\ndata: {}\n\nevent: done\ndata: {\"text\":\"收到啦\"}\n\n"), "text/event-stream", nil
		}
		return 500, nil, "", nil
	}
	base := "/api/account/devices/" + target.DeviceID + "/bots/writer"

	status, out := e.do(c, "GET", base+"/messages?limit=200", nil, nil)
	if status != 200 {
		t.Fatalf("history: %d %v", status, out)
	}
	rows, _ := out["messages"].([]any)
	if len(rows) != 2 {
		t.Fatalf("history kept tool noise or dropped lines: %v", out["messages"])
	}
	if first, _ := rows[0].(map[string]any); first["role"] != "me" || first["text"] != "在吗" || first["state"] != "delivered" {
		t.Fatalf("owner line: %v", first)
	}
	if second, _ := rows[1].(map[string]any); second["role"] != "bot" || second["state"] != "answered" {
		t.Fatalf("bot line: %v", second)
	}
	if dev, _ := out["device"].(map[string]any); dev["id"] != target.DeviceID || dev["label"] != "工作电脑" {
		t.Fatalf("device block: %v", out["device"])
	}
	if len(calls) != 1 || !strings.HasPrefix(calls[0], session+"|"+target.DeviceID+"|GET ") {
		t.Fatalf("history did not carry the account session to that device: %v", calls)
	}

	status, out = e.do(c, "POST", base+"/messages", map[string]string{"text": " 你好 "}, nil)
	if status != 200 {
		t.Fatalf("send: %d %v", status, out)
	}
	msg, _ := out["message"].(map[string]any)
	if msg["text"] != "收到啦" || msg["role"] != "bot" {
		t.Fatalf("answer was not mapped: %v", out["message"])
	}
	if len(calls2) != 2 || calls2[1] != `{"text":"你好"}` {
		t.Fatalf("send body: %q", calls2)
	}

	if status, _ := e.do(c, "POST", base+"/read", nil, nil); status != 204 {
		t.Fatalf("read should settle quietly: %d", status)
	}
	if len(calls) != 2 {
		t.Fatalf("read hit the other computer: %v", calls)
	}

	// 本机这台不算「别的电脑」，账号里没有的设备和不在这台设备名单里的 Bot 都拒绝。
	self := "/api/account/devices/" + local.DeviceID + "/bots/writer"
	if status, _ := e.do(c, "GET", self+"/messages", nil, nil); status != 400 {
		t.Fatalf("this computer was treated as another: %d", status)
	}
	unknownBot := "/api/account/devices/" + target.DeviceID + "/bots/keeper/messages"
	if status, _ := e.do(c, "GET", unknownBot, nil, nil); status != 404 {
		t.Fatalf("bot outside that device's roster: %d", status)
	}
	if status, _ := e.do(c, "GET", "/api/account/devices/nope/bots/writer/messages", nil, nil); status != 404 {
		t.Fatalf("unknown device: %d", status)
	}
	if len(calls) != 2 {
		t.Fatalf("refusals reached the other computer: %v", calls)
	}
}

// 开关能关掉：没有这些路由，侧栏也就不会提供这条路。
func TestAccountDeviceChatOffByDefault(t *testing.T) {
	t.Setenv("XBOT_ACCOUNT_CHAT", "0")
	e, c, _, session := boundAccountBrowser(t)
	target, err := e.s.acct.client.BindDevice(context.Background(), session, account.DeviceReport{ID: "desk", Name: "工作电脑", Bots: []string{"writer"}})
	if err != nil {
		t.Fatal(err)
	}
	status, out := e.do(c, "GET", "/api/account/devices", nil, nil)
	if status != 200 {
		t.Fatalf("roster: %d", status)
	}
	for _, row := range out["devices"].([]any) {
		d, _ := row.(map[string]any)
		if d["self"] == true {
			continue
		}
		if d["chat"] == true {
			t.Fatalf("closed channel offered for %v", d["id"])
		}
	}
	if status, _ := e.do(c, "GET", "/api/account/devices/"+target.DeviceID+"/bots/writer/messages", nil, nil); status != 404 {
		t.Fatalf("routes exist while the switch is off: %d", status)
	}
}

// 代理读回来的 SSE 整段解析：done 里的答、error 里的理由、非 SSE 的 JSON 拒绝。
func TestAccountChatReplyParsing(t *testing.T) {
	for _, tc := range []struct {
		name, raw, ct, text, failed string
	}{
		{"done", "event: start\ndata: {}\n\nevent: done\ndata: {\"text\":\"在\"}\n\n", "text/event-stream", "在", ""},
		{"error", "event: error\ndata: {\"error\":\"模型没配好\"}\n\n", "text/event-stream", "", "模型没配好"},
		{"silent", "event: done\ndata: {}\n\n", "text/event-stream", "", ""},
		{"json", `{"error":"bot 不存在"}`, "application/json", "", "bot 不存在"},
		{"plain", `oops`, "text/plain", "", ""},
	} {
		text, failed := accountChatReply([]byte(tc.raw), tc.ct)
		if text != tc.text || failed != tc.failed {
			t.Errorf("%s: text=%q failed=%q", tc.name, text, failed)
		}
	}
}
