package pipe

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

// The hub's public webhook ingress (ADR 0031). A sender posts to
// {hub}/pipe/v1/hook/{handle}/webhook/{bot}/{name}; the hub hands the request
// to the standby the handle names and returns the device's answer. The hub
// never holds a hook secret: the device's own inbound handler checks the
// signature or token.
const (
	HookBody           = 32 << 10 // same as inbound.MaxBody
	HookTimeout        = 15 * time.Second
	MaxHooksPerDevice  = 8
	maxHookHeader      = 512
	maxHookAnswer      = 1 << 10
	hookIngressPattern = "POST /pipe/v1/hook/{handle}/webhook/{bot}/{name}"
)

// hookHeaders are the only request headers that cross the hub.
var hookHeaders = []string{"Content-Type", "Authorization", "X-Xbot-Signature", "X-Xbot-Delivery", "X-Delivery-Id", "X-Xbot-Event", "X-Xbot-Sender"}

// HookHandle names one account's device in the public ingress. Device ids are
// only unique within an account, so the handle covers both; it is stable
// across reconnects and reveals neither. The hub sends it with the standby ack.
func HookHandle(accountKey, deviceID string) string {
	sum := sha256.Sum256([]byte("xbot-hook\x00" + accountKey + "/" + deviceID))
	return hex.EncodeToString(sum[:16])
}

// HookBase is the public base for a device's webhooks; inbound.WebhookURL
// appends /webhook/{bot}/{name}.
func HookBase(hub, handle string) string {
	return strings.TrimRight(hub, "/") + "/pipe/v1/hook/" + url.PathEscape(handle)
}

type pendingHook struct {
	slot string
	res  chan control
}

func (h *Hub) serveHook(w http.ResponseWriter, r *http.Request) {
	handle, botName, name := r.PathValue("handle"), r.PathValue("bot"), r.PathValue("name")
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, HookBody))
	if err != nil {
		writeErr(w, http.StatusRequestEntityTooLarge, "too_large")
		return
	}
	h.mu.Lock()
	slot := h.hookSlots[handle]
	standby := h.standby[slot]
	switch {
	case standby == nil:
		h.mu.Unlock()
		writeErr(w, http.StatusServiceUnavailable, "device_offline")
		return
	case h.hookLoad[slot] >= MaxHooksPerDevice:
		h.mu.Unlock()
		writeErr(w, http.StatusTooManyRequests, "busy")
		return
	}
	if h.hooks == nil {
		h.hooks, h.hookLoad = make(map[string]pendingHook), make(map[string]int)
	}
	id := randomHex(12)
	p := pendingHook{slot: slot, res: make(chan control, 1)}
	h.hooks[id] = p
	h.hookLoad[slot]++
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.hooks, id)
		if h.hookLoad[slot]--; h.hookLoad[slot] <= 0 {
			delete(h.hookLoad, slot)
		}
		h.mu.Unlock()
	}()

	header := map[string]string{}
	for _, k := range hookHeaders {
		if v := r.Header.Get(k); v != "" && len(v) <= maxHookHeader {
			header[k] = v
		}
	}
	msg, _ := json.Marshal(control{T: "hook", ID: id, Path: "/webhook/" + url.PathEscape(botName) + "/" + url.PathEscape(name), Header: header, Body: body})
	if standby.WriteMessage(opText, msg) != nil {
		writeErr(w, http.StatusServiceUnavailable, "device_offline")
		return
	}
	timer := time.NewTimer(HookTimeout)
	defer timer.Stop()
	select {
	case res := <-p.res:
		h.logf("pipe: hook %s answered %d", short(id), res.Status)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(res.Status)
		_, _ = w.Write(res.Body)
	case <-timer.C:
		writeErr(w, http.StatusGatewayTimeout, "device_timeout")
	case <-r.Context().Done():
	}
}

// hookAnswer hands a device's answer to the waiting sender. Only the standby
// the hook was sent to may answer it.
func (h *Hub) hookAnswer(slot string, m control) {
	if m.Status < 200 || m.Status > 599 || len(m.Body) > maxHookAnswer {
		return
	}
	h.mu.Lock()
	p, ok := h.hooks[m.ID]
	h.mu.Unlock()
	if !ok || p.slot != slot {
		return
	}
	select {
	case p.res <- m:
	default:
	}
}

// hook runs one forwarded webhook against d.Hooks, without the owner's
// credential, and answers on the standby socket.
func (d *Device) hook(ctx context.Context, c *wsConn, m control) {
	status, body := http.StatusNotFound, []byte(`{"status":"not_found"}`)
	if d.Hooks != nil && hookPath(m.Path) && len(m.Body) <= HookBody {
		ctx, cancel := context.WithTimeout(ctx, HookTimeout)
		defer cancel()
		if r, err := http.NewRequestWithContext(ctx, http.MethodPost, m.Path, bytes.NewReader(m.Body)); err == nil {
			for k, v := range m.Header {
				if slices.Contains(hookHeaders, http.CanonicalHeaderKey(k)) && len(v) <= maxHookHeader && !strings.ContainsAny(v, "\r\n") {
					r.Header.Set(k, v)
				}
			}
			r.Host, r.RemoteAddr = "pipe.local", "pipe"
			w := &capture{header: http.Header{}}
			d.Hooks.ServeHTTP(w, r)
			status, body = cmpStatus(w.status), w.body.Bytes()
		}
	}
	if len(body) > maxHookAnswer {
		body = body[:maxHookAnswer]
	}
	raw, _ := json.Marshal(control{T: "hook_res", ID: m.ID, Status: status, Body: body})
	if err := c.WriteMessage(opText, raw); err != nil {
		d.logf("pipe: hook %s answer: %v", short(m.ID), err)
	}
}

// hookPath accepts only /webhook/{bot}/{name}, with no query.
func hookPath(p string) bool {
	u, err := url.ParseRequestURI(p)
	if err != nil || u.RawQuery != "" || u.Host != "" {
		return false
	}
	parts := strings.Split(u.EscapedPath(), "/")
	return len(parts) == 4 && parts[0] == "" && parts[1] == "webhook" && parts[2] != "" && parts[3] != ""
}
