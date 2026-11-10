package main

// 账号内其他电脑的对话线路：web 那边只认一个函数，真正的 pipe 在这里接上。
// 放在 cmd 是因为 internal/web 不能 import internal/pipe（pipe 的测试要起一个
// web.Server，会形成测试环），而 internal/deviceclient 已经 import 了 pipe，
// pipe 也不能反过来 import deviceclient。

import (
	"context"
	"errors"
	"os"
	"strings"

	"xbot/internal/deviceclient"
	"xbot/internal/pipe"
	"xbot/internal/web"
)

// accountChatHub is where tickets are bought. Same default as the device
// client so an install that already talks to a hub keeps using it.
func accountChatHub() string {
	if v := strings.TrimSpace(os.Getenv("XBOT_PIPE_HUB")); v != "" {
		return v
	}
	return deviceclient.Hub()
}

// accountChatSend runs one request on the other computer over a fresh pipe
// session: one session carries one request, so nothing is reused.
func accountChatSend(ctx context.Context, session, device, method, path string, body []byte) (int, []byte, string, error) {
	ct := ""
	if body != nil {
		ct = "application/json"
	}
	hub := accountChatHub()
	ticket, err := pipe.NewTicket(ctx, hub, session, device)
	if err != nil {
		return 0, nil, "", err
	}
	conn, err := pipe.Dial(ctx, hub, ticket)
	if err != nil {
		return 0, nil, "", err
	}
	defer conn.Close()
	return conn.Do(ctx, method, path, body, ct)
}

// accountChatStatus reads the status out of a pipe refusal.
func accountChatStatus(err error) (int, bool) {
	var refusal *pipe.HTTPError
	if errors.As(err, &refusal) {
		return refusal.Status, true
	}
	return 0, false
}

// wireAccountChat hands the web proxy its transport. Called once at startup,
// before the server listens.
func wireAccountChat() {
	web.SetAccountChatPipe(accountChatSend, accountChatStatus)
}
