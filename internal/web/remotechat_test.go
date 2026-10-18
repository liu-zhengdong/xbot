package web

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/remote"
)

// fakeChatGateway answers every /v1/send with sendStatus.
func fakeChatGateway(t *testing.T, sendStatus int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/send" {
			w.WriteHeader(sendStatus)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// chatPeer adds a paired device "b" with an encryption key to a, pointed at
// gw, and returns its id.
func chatPeer(t *testing.T, a *deviceSide, gw *httptest.Server) string {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	_, xkey, err := remote.XIdentity(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	d, err := remote.AddDevice(a.root, "b", base64.StdEncoding.EncodeToString(pub), xkey, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.SetGateway(a.root, gw.URL); err != nil {
		t.Fatal(err)
	}
	return d.ID
}

// chatLine writes one transcript line for bot on device id.
func chatLine(t *testing.T, root, id, bot string, m remote.ChatMessage) {
	t.Helper()
	m.At = time.Now()
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "remote", "chats", id, bot+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Write(append(raw, '\n')); err != nil {
		t.Fatal(err)
	}
}

type chatReply struct {
	Messages []remote.ChatMessage `json:"messages"`
	Device   map[string]any       `json:"device"`
	Message  remote.ChatMessage   `json:"message"`
	Error    string               `json:"error"`
}

// noSession sends the request as a browser that has no owner cookie.
func (d *deviceSide) noSession(method, path string) int {
	d.t.Helper()
	req, err := http.NewRequest(method, d.srv.URL+path, bytes.NewReader([]byte(`{"text":"hi"}`)))
	if err != nil {
		d.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := d.srv.Client().Do(req)
	if err != nil {
		d.t.Fatal(err)
	}
	res.Body.Close()
	return res.StatusCode
}

// TestRemoteChatAPI: the history marks the chat read and carries the device
// line; a typed line is sent (queued_local is still success); read answers 204;
// the 设备 list gains per-bot summaries.
func TestRemoteChatAPI(t *testing.T) {
	gw := fakeChatGateway(t, http.StatusAccepted)
	a := newDeviceSide(t, "书房 iMac", "ada")
	id := chatPeer(t, a, gw)
	chatLine(t, a.root, id, "bob", remote.ChatMessage{ID: "m1", Role: "me", Text: "在吗", State: "answered"})
	chatLine(t, a.root, id, "bob", remote.ChatMessage{ID: "b1", Role: "bot", Text: "在的", State: "answered"})
	base := "/api/remote/" + id + "/bots/bob/"

	if code := a.noSession("GET", base+"messages"); code != http.StatusUnauthorized {
		t.Fatalf("history without session: %d", code)
	}
	if code := a.noSession("POST", base+"messages"); code != http.StatusUnauthorized {
		t.Fatalf("send without session: %d", code)
	}
	if code := a.noSession("POST", base+"read"); code != http.StatusUnauthorized {
		t.Fatalf("read without session: %d", code)
	}

	var hist chatReply
	if code := a.call("GET", base+"messages?limit=50", "", &hist); code != 200 || len(hist.Messages) != 2 || hist.Messages[1].Text != "在的" {
		t.Fatalf("history: %d %+v", code, hist)
	}
	if hist.Device["id"] != id || hist.Device["label"] != "b" || hist.Device["online"] != false || hist.Device["unpaired"] != false {
		t.Fatalf("device block: %+v", hist.Device)
	}
	sums, err := remote.ChatSummaries(a.root, id)
	if err != nil || sums["bob"].Unread != 0 {
		t.Fatalf("history did not mark read: %+v %v", sums, err)
	}

	var sent chatReply
	if code := a.call("POST", base+"messages", `{"text":"  你好  "}`, &sent); code != 200 || sent.Message.Text != "你好" || sent.Message.Role != "me" || (sent.Message.State != "sent" && sent.Message.State != "queued_local") {
		t.Fatalf("send: %d %+v", code, sent)
	}
	// An empty body is not JSON, and a 204 makes .call skip decoding it.
	if code := a.call("POST", base+"read", "", nil); code != http.StatusNoContent {
		t.Fatalf("read: %d", code)
	}
	if code := a.call("GET", base+"messages?limit=x", "", nil); code != http.StatusBadRequest {
		t.Fatalf("bad limit: %d", code)
	}

	// The 设备 list now carries the summary, including the waiting line.
	var list struct {
		Devices []struct {
			Chats map[string]remote.ChatSummary `json:"chats"`
		} `json:"devices"`
	}
	if code := a.call("GET", "/api/devices", "", &list); code != 200 || len(list.Devices) != 1 {
		t.Fatalf("list: %d %+v", code, list)
	}
	if s := list.Devices[0].Chats["bob"]; s.Last != "你好" || s.LastRole != "me" || len(list.Devices[0].Chats) != 1 {
		t.Fatalf("summary on the device row: %+v", list.Devices[0].Chats)
	}
}

// TestRemoteChatAPIErrors: unknown device 404, bad bot name 400, empty or long
// text 400, a device unpaired by the other computer 409, and a non-retryable
// gateway rejection 502 with the stored line marked failed.
func TestRemoteChatAPIErrors(t *testing.T) {
	gw := fakeChatGateway(t, http.StatusForbidden)
	a := newDeviceSide(t, "书房 iMac", "ada")
	id := chatPeer(t, a, gw)
	unknown := "/api/remote/" + strings.Repeat("ab", 16) + "/bots/bob/"
	var e chatReply
	if code := a.call("GET", unknown+"messages", "", &e); code != http.StatusNotFound {
		t.Fatalf("unknown device: %d", code)
	}
	// "%20" reaches the handler as a space: not a bot name.
	for _, method := range []string{"GET", "POST"} {
		if code := a.call(method, "/api/remote/"+id+"/bots/bad%20name/messages", `{"text":"hi"}`, &e); code != http.StatusBadRequest {
			t.Fatalf("%s bad bot name: %d", method, code)
		}
	}
	base := "/api/remote/" + id + "/bots/bob/"
	if code := a.call("POST", base+"messages", `{"text":"   "}`, &e); code != http.StatusBadRequest {
		t.Fatalf("empty text: %d", code)
	}
	if code := a.call("POST", base+"messages", `{"text":"`+strings.Repeat("x", remote.MaxChatText+1)+`"}`, &e); code != http.StatusBadRequest {
		t.Fatalf("long text: %d", code)
	}

	// The gateway refuses the line for good: stored as failed, answered 502.
	if code := a.call("POST", base+"messages", `{"text":"会被拒"}`, &e); code != http.StatusBadGateway || e.Message.State != "failed" || e.Error == "" {
		t.Fatalf("rejected send: %d %+v", code, e)
	}
	if code := a.call("POST", base+"messages", `{"text":"too much`+strings.Repeat(" ", 64<<10)+`"}`, &e); code != http.StatusBadRequest {
		t.Fatalf("oversized body: %d", code)
	}

	// Unpaired by the other computer: its kind unpair sets the mark, then 409.
	unpairPeer(t, a.root, id)
	if code := a.call("POST", base+"messages", `{"text":"还在吗"}`, &e); code != http.StatusConflict || !strings.Contains(e.Error, "已经解除了和这台电脑的配对") {
		t.Fatalf("unpaired send: %d %+v", code, e)
	}
	if sums, err := remote.ChatSummaries(a.root, id); err != nil || sums["bob"].Failed != true || sums["bob"].Queued != "" {
		t.Fatalf("transcript after the cases: %+v %v", sums, err)
	}
}

// TestRemoteChatQueued: a gateway 5xx leaves the line queued_local, which the
// web layer reports as success.
func TestRemoteChatQueued(t *testing.T) {
	a := newDeviceSide(t, "书房 iMac", "ada")
	id := chatPeer(t, a, fakeChatGateway(t, http.StatusServiceUnavailable))
	var out chatReply
	if code := a.call("POST", "/api/remote/"+id+"/bots/bob/messages", `{"text":"离线时说的话"}`, &out); code != 200 || out.Message.State != "queued_local" {
		t.Fatalf("queued send: %d %+v", code, out)
	}
}

// unpairPeer marks device id as unpaired by the other computer; the device
// stays in the list, as after the peer's kind unpair.
func unpairPeer(t *testing.T, root, id string) {
	t.Helper()
	cfg, err := remote.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	for i := range cfg.Devices {
		if cfg.Devices[i].ID == id {
			cfg.Devices[i].UnpairedAt = time.Now().UTC()
		}
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "remote", "devices.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
}
