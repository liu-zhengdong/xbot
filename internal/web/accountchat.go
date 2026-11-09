package web

// 账号内其他电脑上的 Bot 对话：和短码配对设备的远端对话共用侧栏、共用面板，
// 线路换成账号 pipe —— 每个请求现领一张票，按用户的账号会话代理到目标电脑
// 自己的用户 API（GET /api/bots/<bot>/chat、POST /api/bots/<bot>/messages）。
// 短码配对那套加密信箱（internal/remote）不参与，也不需要两机配过对。

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"xbot/internal/account"
	"xbot/internal/bot"
	"xbot/internal/deviceclient"
	"xbot/internal/pipe"
	"xbot/internal/remote"
)

// accountChatWait bounds one send: the pipe hands back the whole SSE stream at
// once, so this is how long a single line may wait for its answer.
const accountChatWait = 5 * time.Minute

// accountChatCallWait bounds the reads (history, mark read); a device that is
// not on standby refuses the ticket quickly, so this only catches a stuck hub.
const accountChatCallWait = 30 * time.Second

// accountChatEnabled is the switch for the account-device conversation line.
// Off by default: the pipe proxy is new and a wrong read here would show a
// panel that cannot send, so it is opt-in until it has run for a while.
func accountChatEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("XBOT_ACCOUNT_CHAT"))) {
	case "1", "true", "on", "yes":
		return true
	}
	return false
}

// registerAccountChatAPI adds the account-device conversation routes. They are
// off unless XBOT_ACCOUNT_CHAT says otherwise: the sidebar reads the same flag
// from the roster's chat field, so a closed channel is never offered.
func (s *Server) registerAccountChatAPI(mux *http.ServeMux) {
	if !accountChatEnabled() {
		return
	}
	mux.HandleFunc("GET /api/account/devices/{id}/bots/{bot}/messages", s.accountChatHistory)
	mux.HandleFunc("POST /api/account/devices/{id}/bots/{bot}/messages", s.accountChatSend)
	mux.HandleFunc("POST /api/account/devices/{id}/bots/{bot}/read", s.accountChatRead)
}

// accountChatPeer is one bot on another computer of the same account.
type accountChatPeer struct {
	device  account.DeviceView
	bot     string
	session string
}

// accountChatPeer resolves the path's device and bot inside this account and
// writes the refusal itself. This computer is never a peer: its bots are local.
func (s *Server) accountChatPeer(w http.ResponseWriter, r *http.Request) (accountChatPeer, bool) {
	if !s.require(w, r) || !s.accountReady(w, r) {
		return accountChatPeer{}, false
	}
	name := r.PathValue("bot")
	if !bot.ValidName(name) {
		writeErr(w, http.StatusBadRequest, "Bot 名字不对")
		return accountChatPeer{}, false
	}
	session := accountSession(r)
	list, err := s.acct.client.Devices(r.Context(), session)
	if err != nil {
		if errors.Is(err, account.ErrSession) {
			s.forgetPipeSession(session)
		}
		writeAccountErr(w, err)
		return accountChatPeer{}, false
	}
	id := r.PathValue("id")
	b, _ := s.readBinding()
	for _, d := range list {
		if d.ID != id {
			continue
		}
		if b.DeviceID != "" && b.DeviceID == id {
			writeErr(w, http.StatusBadRequest, "这台电脑上的 Bot 不用远道")
			return accountChatPeer{}, false
		}
		// The device's own report is the roster: a name it does not list is a
		// 404 here, not a probe of the other computer.
		if !slices.Contains(d.Bots, name) {
			writeErr(w, http.StatusNotFound, "这台电脑上没有这个 Bot")
			return accountChatPeer{}, false
		}
		return accountChatPeer{device: d, bot: name, session: session}, true
	}
	writeErr(w, http.StatusNotFound, "账号里没有这台电脑")
	return accountChatPeer{}, false
}

// accountChatDo is one request to the other computer over a fresh pipe session.
// One pipe session carries one request, so nothing is kept between calls.
// accountChatTransport is the one call the proxy makes; the pipe is the only
// real implementation and tests hand in their own.
type accountChatTransport func(ctx context.Context, session, device, method, path string, body []byte) (int, []byte, string, error)

func (s *Server) accountChatDo(ctx context.Context, session, device, method, path string, body []byte) (int, []byte, string, error) {
	if s.acct.chatDo != nil {
		return s.acct.chatDo(ctx, session, device, method, path, body)
	}
	ct := ""
	if body != nil {
		ct = "application/json"
	}
	hub := deviceclient.Hub()
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

// writeAccountChatErr turns a pipe refusal into a line the panel shows as
// 「没能完成：…」. 409 is the hub saying this device is not on standby.
func writeAccountChatErr(w http.ResponseWriter, err error, label string) {
	var refusal *pipe.HTTPError
	if errors.As(err, &refusal) {
		switch refusal.Status {
		case http.StatusNotFound:
			writeErr(w, http.StatusNotFound, "账号里没有这台电脑")
			return
		case http.StatusConflict, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			writeErr(w, http.StatusGatewayTimeout, label+"现在不在线，等它在 XBot 里醒来再试")
			return
		case http.StatusUnauthorized:
			writeErr(w, http.StatusUnauthorized, "登录过期了，重新登录")
			return
		case http.StatusTooManyRequests:
			writeErr(w, http.StatusTooManyRequests, "同时连过去的太多，等一分钟再试")
			return
		}
	}
	writeErr(w, http.StatusBadGateway, "连不上"+label+"："+err.Error())
}

// accountChatRow is one line of the other computer's own transcript.
type accountChatRow struct {
	ID   int       `json:"id"`
	Kind string    `json:"kind"`
	Time time.Time `json:"time"`
	Text string    `json:"text"`
}

func (s *Server) accountChatHistory(w http.ResponseWriter, r *http.Request) {
	p, ok := s.accountChatPeer(w, r)
	if !ok {
		return
	}
	limit := 200
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			writeErr(w, http.StatusBadRequest, "limit 不对")
			return
		}
		limit = min(n, 1000)
	}
	ctx, cancel := context.WithTimeout(r.Context(), accountChatCallWait)
	defer cancel()
	path := "/api/bots/" + url.PathEscape(p.bot) + "/chat?limit=" + strconv.Itoa(min(limit, bot.MaxWindowLimit))
	code, raw, _, err := s.accountChatDo(ctx, p.session, p.device.ID, "GET", path, nil)
	if err != nil {
		writeAccountChatErr(w, err, p.device.Name)
		return
	}
	var transcript struct {
		Messages []accountChatRow `json:"messages"`
	}
	if code != http.StatusOK || json.Unmarshal(raw, &transcript) != nil {
		writeErr(w, http.StatusBadGateway, p.device.Name+"的对话没读出来")
		return
	}
	lines := make([]remote.ChatMessage, 0, len(transcript.Messages))
	for _, m := range transcript.Messages {
		text := strings.TrimSpace(m.Text)
		if text == "" { // tool noise and system rows are not a conversation
			continue
		}
		role := "bot"
		if m.Kind == "owner" {
			role = "me"
		}
		lines = append(lines, remote.ChatMessage{ID: strconv.Itoa(m.ID), Role: role, Text: m.Text,
			At: m.Time, State: map[string]string{"me": "delivered", "bot": "answered"}[role]})
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": lines, "device": p.block()})
}

// block is the device line above the panel; the account service is the only
// thing that knows whether that computer is awake.
func (p accountChatPeer) block() map[string]any {
	out := map[string]any{"id": p.device.ID, "label": p.device.Name, "online": p.device.Online}
	if !p.device.LastSeen.IsZero() {
		out["seen"] = p.device.LastSeen
	}
	return out
}

func (s *Server) accountChatSend(w http.ResponseWriter, r *http.Request) {
	p, ok := s.accountChatPeer(w, r)
	if !ok {
		return
	}
	var in struct {
		Text string `json:"text"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxChatBody)
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	text := strings.TrimSpace(in.Text)
	switch {
	case text == "":
		writeErr(w, http.StatusBadRequest, "消息是空的")
		return
	case len(text) > remote.MaxChatText:
		writeErr(w, http.StatusBadRequest, "一次说一句，别超过 16 KiB")
		return
	}
	if !p.device.Online {
		writeErr(w, http.StatusGatewayTimeout, p.device.Name+"现在不在线，等它在 XBot 里醒来再试")
		return
	}
	body, _ := json.Marshal(map[string]string{"text": text})
	ctx, cancel := context.WithTimeout(r.Context(), accountChatWait)
	defer cancel()
	code, raw, ct, err := s.accountChatDo(ctx, p.session, p.device.ID, "POST", "/api/bots/"+url.PathEscape(p.bot)+"/messages", body)
	if err != nil {
		writeAccountChatErr(w, err, p.device.Name)
		return
	}
	// The other computer answers with the same SSE stream the local chat shows.
	// It arrives whole, so this request waits for the end of the turn and the
	// answer is what it hands back; a turn that says nothing leaves it empty.
	reply, failed := accountChatReply(raw, ct)
	if code != http.StatusOK {
		if failed != "" {
			writeErr(w, http.StatusBadGateway, failed)
			return
		}
		writeErr(w, http.StatusBadGateway, "另一台电脑没收下这句")
		return
	}
	if reply == "" {
		writeJSON(w, http.StatusOK, map[string]any{"message": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"message": remote.ChatMessage{
		Role: "bot", Text: reply, At: time.Now().UTC(), State: "answered",
	}})
}

// accountChatReply reads the buffered SSE stream: the last done carries the
// answer, and an error event carries the reason it has none.
func accountChatReply(raw []byte, ct string) (text, failed string) {
	if !strings.Contains(ct, "text/event-stream") {
		var refusal struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(raw, &refusal) == nil && refusal.Error != "" {
			return "", refusal.Error
		}
		return "", ""
	}
	event := ""
	for _, line := range strings.Split(string(raw), "\n") {
		switch {
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			payload := []byte(strings.TrimPrefix(line, "data: "))
			switch event {
			case "done":
				var done doneBody
				if json.Unmarshal(payload, &done) == nil && strings.TrimSpace(done.Text) != "" {
					text = done.Text
				}
			case "error":
				var ref struct {
					Error string `json:"error"`
				}
				if json.Unmarshal(payload, &ref) == nil && strings.TrimSpace(ref.Error) != "" {
					failed = ref.Error
				}
			}
		}
	}
	return text, failed
}

// accountChatRead has nothing to mark: the account roster carries no unread
// counter for these lines, and the other computer marks nothing either. It
// answers like the paired route so the panel's one call works on both.
func (s *Server) accountChatRead(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.accountChatPeer(w, r); !ok {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
