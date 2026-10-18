package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"xbot/internal/bot"
	"xbot/internal/gateway"
	"xbot/internal/remote"
)

// 主人远端对话的网页接口（G7e 批次 B）：一台配对设备上某个 Bot 的对话记录、
// 发送一句话、标记已读，以及设备列表上每个 Bot 的摘要。

func (s *Server) registerRemoteChatAPI(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/remote/{device}/bots/{bot}/messages", s.remoteChatHistory)
	mux.HandleFunc("POST /api/remote/{device}/bots/{bot}/messages", s.sendRemoteChat)
	mux.HandleFunc("POST /api/remote/{device}/bots/{bot}/read", s.remoteChatRead)
}

// chatRow is one transcript line plus no_ack: online, sent, and not acked
// within remote.ChatAckTimeout (「没收到 X 的回执」, not 已排队).
type chatRow struct {
	remote.ChatMessage
	NoAck bool `json:"no_ack,omitempty"`
}

// maxChatBody caps a POST body; one chat line is at most 16 KiB.
const maxChatBody = 64 << 10

// remoteTarget resolves the path's device and bot, writing the 404/400 itself.
func (s *Server) remoteTarget(w http.ResponseWriter, r *http.Request) (remote.Device, string, bool) {
	cfg, err := remote.Load(s.Root)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return remote.Device{}, "", false
	}
	d, ok := cfg.ByID(r.PathValue("device"))
	if !ok {
		writeErr(w, http.StatusNotFound, "没有这台设备")
		return remote.Device{}, "", false
	}
	name := r.PathValue("bot")
	if !bot.ValidName(name) {
		writeErr(w, http.StatusBadRequest, "Bot 名字不对")
		return remote.Device{}, "", false
	}
	return d, name, true
}

// remoteDeviceBlock is the device line above a remote chat; online follows the
// same rule as the 设备 list.
func (s *Server) remoteDeviceBlock(w http.ResponseWriter, r *http.Request, d remote.Device) map[string]any {
	self, err := remote.Self(s.Root, s.owner)
	up := err == nil && gatewayUp(r.Context(), self.Gateway, false)
	views, err := remote.Devices(s.Root)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return nil
	}
	for _, v := range views {
		if v.ID == d.ID {
			return map[string]any{"id": v.ID, "label": v.Label, "online": deviceOnline(v, up), "seen": v.Seen, "unpaired": v.Unpaired}
		}
	}
	writeErr(w, http.StatusNotFound, "没有这台设备")
	return nil
}

func (s *Server) remoteChatHistory(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	d, name, ok := s.remoteTarget(w, r)
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
	lines, err := remote.ChatHistory(s.Root, d.ID, name, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	dev := s.remoteDeviceBlock(w, r, d)
	if dev == nil {
		return
	}
	// Opening the chat is seeing it, up to the last line returned; a failure
	// here only leaves unread higher, so it never fails the history.
	if len(lines) > 0 {
		_ = remote.MarkChatRead(s.Root, d.ID, name, lines[len(lines)-1].ID)
	}
	online, now := dev["online"] == true, time.Now()
	out := make([]chatRow, 0, len(lines))
	for _, m := range lines {
		out = append(out, chatRow{m, remote.NoAck(m, online, now)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": out, "device": dev, "ack_timeout": int(remote.ChatAckTimeout.Seconds())})
}

// sendRemoteChat is the only caller of remote.SendOwnerChat: it is the line the
// owner typed in the chat composer, and that is what makes it the owner's own
// words on the other computer.
func (s *Server) sendRemoteChat(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	d, name, ok := s.remoteTarget(w, r)
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
	case d.XKey == "":
		writeErr(w, http.StatusConflict, "这台设备还没有加密密钥，请重新配对")
		return
	case !d.UnpairedAt.IsZero():
		// Shown as 「没能完成：…」 with the text back in the composer.
		writeErr(w, http.StatusConflict, deviceName(d)+" 已经解除了和这台电脑的配对，这条没有发出")
		return
	}
	m, err := remote.SendOwnerChat(r.Context(), s.Root, d.ID, name, text, time.Now())
	var status *gateway.HTTPError
	switch {
	case err == nil || m.State == "queued_local" || m.State == "sent":
		// queued_local is success: the line waits in the outbox and goes later.
		// sent with an error means the gateway took it but the local state
		// update failed; a retry would send the owner's words twice.
		writeJSON(w, http.StatusOK, map[string]any{"message": m})
	case !errors.As(err, &status):
		// A local or key failure, not the gateway refusing: nothing is queued.
		writeErr(w, http.StatusInternalServerError, "没送到另一台电脑，再试一次")
	default:
		// The gateway refused the line for good (or the outbox is full); it
		// stays in the transcript as failed.
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "另一台电脑拒收，这一句没送到", "message": m})
	}
}

func (s *Server) remoteChatRead(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	d, name, ok := s.remoteTarget(w, r)
	if !ok {
		return
	}
	if err := remote.MarkChatRead(s.Root, d.ID, name, ""); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// deviceName is the name the owner gave the device here, else its address name.
func deviceName(d remote.Device) string {
	if d.Label != "" {
		return d.Label
	}
	return d.Name
}
