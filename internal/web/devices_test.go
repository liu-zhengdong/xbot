package web

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/gateway"
	"xbot/internal/person"
	"xbot/internal/remote"
)

type deviceSide struct {
	t    *testing.T
	root string
	srv  *httptest.Server
	tok  string
}

func newDeviceSide(t *testing.T, label, botName string) *deviceSide {
	t.Helper()
	root := t.TempDir()
	p, err := person.Identify(root, person.Account{Link: "terminal", ExternalID: "uid:1"}, "owner")
	if err != nil {
		t.Fatal(err)
	}
	b, err := bot.Create(root, botName, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{p.ID}); err != nil {
		t.Fatal(err)
	}
	if err := remote.SetSelfName(root, label); err != nil {
		t.Fatal(err)
	}
	s := &Server{Root: root, Owner: p.ID}
	tok, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(muxOf(s))
	t.Cleanup(srv.Close)
	return &deviceSide{t, root, srv, tok}
}

func (d *deviceSide) call(method, path, body string, out any) int {
	d.t.Helper()
	req, err := http.NewRequest(method, d.srv.URL+path, bytes.NewReader([]byte(body)))
	if err != nil {
		d.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", d.srv.URL)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: d.tok})
	res, err := d.srv.Client().Do(req)
	if err != nil {
		d.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if out != nil && json.Unmarshal(raw, out) != nil {
		d.t.Fatalf("%s %s: %d %s", method, path, res.StatusCode, raw)
	}
	return res.StatusCode
}

// The 设备 drawer's read and edit endpoints: a gateway change is checked first
// and keeps the old address on failure (and keeps paired devices), rename
// changes only the label, unpair drops the device.
func TestDevicesAPIList(t *testing.T) {
	gw := httptest.NewServer((&gateway.Server{Dir: t.TempDir(), Token: "test"}).Handler())
	defer gw.Close()
	a := newDeviceSide(t, "书房 iMac", "ada")
	if self, err := remote.Self(a.root, ""); err != nil || self.Gateway != remote.DefaultGateway {
		t.Fatalf("unset gateway is not the official default: %+v %v", self, err)
	}
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	d, err := remote.AddDevice(a.root, "mbp", base64.StdEncoding.EncodeToString(pub), base64.StdEncoding.EncodeToString(make([]byte, 32)), "")
	if err != nil {
		t.Fatal(err)
	}
	if code := a.call("PUT", "/api/devices/gateway", `{"gateway":"`+gw.URL+`"}`, nil); code != 200 {
		t.Fatalf("gateway: %d", code)
	}
	var e struct {
		Error string `json:"error"`
	}
	if code := a.call("PUT", "/api/devices/gateway", `{"gateway":"http://127.0.0.1:1"}`, &e); code != http.StatusBadGateway || !strings.Contains(e.Error, "还在用原来的地址") {
		t.Fatalf("unreachable gateway: %d %+v", code, e)
	}
	var list struct {
		Self      remote.SelfInfo `json:"self"`
		GatewayOK bool            `json:"gateway_ok"`
		Devices   []deviceRow     `json:"devices"`
	}
	a.call("GET", "/api/devices", "", &list)
	if !list.GatewayOK || list.Self.Name != "书房 iMac" || list.Self.Gateway != gw.URL || len(list.Self.Bots) != 1 || len(list.Devices) != 1 || list.Devices[0].Label != "mbp" || list.Devices[0].Online {
		t.Fatalf("list: %+v", list)
	}
	if code := a.call("PATCH", "/api/devices/"+d.ID, `{"label":"我的 MacBook"}`, nil); code != 200 {
		t.Fatalf("rename: %d", code)
	}
	if code := a.call("PATCH", "/api/devices/self", `{"name":"书房"}`, nil); code != 200 {
		t.Fatalf("rename self: %d", code)
	}
	a.call("GET", "/api/devices", "", &list)
	if list.Devices[0].Label != "我的 MacBook" || list.Devices[0].Name != "mbp" || list.Self.Name != "书房" {
		t.Fatalf("rename: %+v", list)
	}
	if code := a.call("DELETE", "/api/devices/"+d.ID, "", nil); code != 200 {
		t.Fatalf("unpair: %d", code)
	}
	a.call("GET", "/api/devices", "", &list)
	if len(list.Devices) != 0 {
		t.Fatalf("unpair kept the device: %+v", list)
	}
}

// A device that unpaired us stays listed with "unpaired": true, never online,
// so the drawer offers only 解除配对 and the sidebar leaves it out.
func TestDevicesAPIUnpaired(t *testing.T) {
	a := newDeviceSide(t, "书房 iMac", "ada")
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	d, err := remote.AddDevice(a.root, "mbp", base64.StdEncoding.EncodeToString(pub), base64.StdEncoding.EncodeToString(make([]byte, 32)), "")
	if err != nil {
		t.Fatal(err)
	}
	// The peer's kind unpair sets this in devices.json; set it the same way here.
	cfg, err := remote.Load(a.root)
	if err != nil {
		t.Fatal(err)
	}
	for i := range cfg.Devices {
		if cfg.Devices[i].ID == d.ID {
			cfg.Devices[i].UnpairedAt = time.Now().UTC()
		}
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.root, "remote", "devices.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	var list struct {
		Devices []map[string]any `json:"devices"`
	}
	if code := a.call("GET", "/api/devices", "", &list); code != 200 || len(list.Devices) != 1 {
		t.Fatalf("list: %d %+v", code, list)
	}
	got := list.Devices[0]
	if got["unpaired"] != true || got["online"] != false || got["label"] != "mbp" {
		t.Fatalf("unpaired device row: %+v", got)
	}
}

// `PATCH /api/devices/{id}` also flips 「Bot 互发消息」; the label stays when
// only bot_messaging is sent, and an unknown id is 404 没有这台设备.
func TestDevicesAPIBotMessaging(t *testing.T) {
	a := newDeviceSide(t, "书房 iMac", "ada")
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	d, err := remote.AddDevice(a.root, "mbp", base64.StdEncoding.EncodeToString(pub), base64.StdEncoding.EncodeToString(make([]byte, 32)), "")
	if err != nil {
		t.Fatal(err)
	}
	row := func() map[string]any {
		var list struct {
			Devices []map[string]any `json:"devices"`
		}
		a.call("GET", "/api/devices", "", &list)
		if len(list.Devices) != 1 {
			t.Fatalf("list: %+v", list)
		}
		return list.Devices[0]
	}
	if got := row(); got["bot_messaging"] != true || got["label"] != "mbp" {
		t.Fatalf("default row: %+v", got)
	}
	if code := a.call("PATCH", "/api/devices/"+d.ID, `{"bot_messaging":false}`, nil); code != 200 {
		t.Fatalf("switch off: %d", code)
	}
	if got := row(); got["bot_messaging"] != false || got["label"] != "mbp" {
		t.Fatalf("switched-off row: %+v", got)
	}
	var e struct {
		Error string `json:"error"`
	}
	if code := a.call("PATCH", "/api/devices/nope", `{"bot_messaging":true}`, &e); code != http.StatusNotFound || e.Error != "没有这台设备" {
		t.Fatalf("unknown id: %d %+v", code, e)
	}
}

// A gateway 429 on the code and a full name are not 连不上网关 or 没送到:
// pairErrCopy gives each its status and copy, and the handlers use it.
func TestDevicesAPIErrorCopy(t *testing.T) {
	for _, c := range []struct {
		err  error
		code int
		msg  string
	}{
		{remote.ErrRateLimited, http.StatusTooManyRequests, "操作太频繁，等一分钟再试"},
		{fmt.Errorf("%w: device name suffixes exhausted", remote.ErrNameFull), http.StatusConflict, "同名设备已满 9 台，先解除或改名旧的，或选择替换旧的"},
	} {
		if code, msg, ok := pairErrCopy(fmt.Errorf("gateway: %w", c.err)); !ok || code != c.code || msg != c.msg {
			t.Fatalf("wrapped %v: %d %q %v", c.err, code, msg, ok)
		}
		if _, _, ok := pairErrCopy(errors.New("remote: no pairing request")); ok {
			t.Fatal("other errors took a 429/409 copy")
		}
	}
	// The handler: a gateway answering 429 to /v1/code twice (after the 2 s
	// retry too) is 「操作太频繁」, not 连不上网关.
	real := (&gateway.Server{Dir: t.TempDir(), Open: true}).Handler()
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/code" {
			real.ServeHTTP(w, r)
			return
		}
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer fake.Close()
	a := newDeviceSide(t, "书房", "ada")
	var e struct {
		Error string `json:"error"`
	}
	if code := a.call("POST", "/api/devices/code", `{"gateway":"`+fake.URL+`"}`, &e); code != http.StatusTooManyRequests || e.Error != "操作太频繁，等一分钟再试" {
		t.Fatalf("code on 429: %d %+v", code, e)
	}
}

type pairingOut struct {
	Pairing remote.Status `json:"pairing"`
}

func (d *deviceSide) waitPhase(side, phase string) remote.PairingPhase {
	d.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	var got remote.PairingPhase
	for time.Now().Before(deadline) {
		var out pairingOut
		d.call("GET", "/api/devices/pairing", "", &out)
		got = out.Pairing.A
		if side == "B" {
			got = out.Pairing.B
		}
		if got.Phase == phase {
			return got
		}
		time.Sleep(20 * time.Millisecond)
	}
	d.t.Fatalf("%s never reached %s: %+v", side, phase, got)
	return got
}

// Key path through the HTTP API: A saves a gateway and makes a code, B (no
// gateway token) types it with the gateway in the 加入 modal, both confirm, and
// each 设备 list shows the other by its display name.
func TestDevicesAPIPairing(t *testing.T) {
	gwDir := t.TempDir()
	gw := httptest.NewServer((&gateway.Server{Dir: gwDir, Token: "test"}).Handler())
	defer gw.Close()
	t.Setenv("XBOT_GATEWAY_TOKEN", "test")
	a, b := newDeviceSide(t, "书房 iMac", "ada"), newDeviceSide(t, "MacBook", "bob")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{}, 2)
	go func() { remote.Run(ctx, a.root, "test", nil); done <- struct{}{} }()
	go func() { remote.Run(ctx, b.root, "", nil); done <- struct{}{} }()
	defer func() { cancel(); <-done; <-done }()

	if code := a.call("PUT", "/api/devices/gateway", `{"gateway":"http://127.0.0.1:1"}`, nil); code != http.StatusBadGateway {
		t.Fatalf("unreachable gateway: want 502, got %d", code)
	}
	var made pairingOut
	if code := a.call("POST", "/api/devices/code", `{"gateway":"`+gw.URL+`"}`, &made); code != 200 || made.Pairing.A.Phase != "waiting" || len(made.Pairing.A.Code) != 14 {
		t.Fatalf("code: %d %+v", code, made)
	}
	code := made.Pairing.A.Code

	var out pairingOut
	typo := code[:13] + string(code[13]^1)
	if b.call("POST", "/api/devices/join", `{"code":"`+typo+`"}`, &out); out.Pairing.B.Phase != "typo" {
		t.Fatalf("typo: %+v", out)
	}
	if _, err := os.Stat(filepath.Join(b.root, "remote", "config.json")); err == nil {
		if cfg, _ := remote.Load(b.root); cfg.Gateway != "" {
			t.Fatal("a typo must not touch the gateway settings")
		}
	}
	b.call("POST", "/api/devices/join", `{"code":"`+code+`","gateway":"`+gw.URL+`"}`, &out)
	got := b.waitPhase("B", "confirm")
	if got.Name != "书房 iMac" || len(got.Bots) != 1 || got.Bots[0] != "ada" {
		t.Fatalf("B confirm: %+v", got)
	}
	b.call("POST", "/api/devices/join/confirm", "", nil)
	req := a.waitPhase("A", "request")
	if req.Name != "MacBook" || req.Collision != nil {
		t.Fatalf("A request: %+v", req)
	}
	if code := a.call("POST", "/api/devices/request", `{"decision":"allow"}`, nil); code != 200 {
		t.Fatalf("allow: %d", code)
	}
	b.waitPhase("B", "done")

	var listA, listB struct {
		Self      remote.SelfInfo `json:"self"`
		GatewayOK bool            `json:"gateway_ok"`
		Devices   []deviceRow     `json:"devices"`
	}
	a.call("GET", "/api/devices", "", &listA)
	b.call("GET", "/api/devices", "", &listB)
	if !listA.GatewayOK || listA.Self.Name != "书房 iMac" || len(listA.Devices) != 1 || listA.Devices[0].Label != "MacBook" {
		t.Fatalf("A list: %+v", listA)
	}
	if len(listB.Devices) != 1 || listB.Devices[0].Label != "书房 iMac" || listB.Self.Gateway != gw.URL {
		t.Fatalf("B list: %+v", listB)
	}
	// The used code is now dead at the gateway: 410.
	b.call("POST", "/api/devices/join", `{"code":"`+code+`"}`, &out)
	if out.Pairing.B.Phase != "already" && out.Pairing.B.Phase != "expired" {
		t.Fatalf("reuse: %+v", out)
	}
	id := listA.Devices[0].ID
	if code := a.call("PATCH", "/api/devices/"+id, `{"label":"我的 MacBook"}`, nil); code != 200 {
		t.Fatalf("rename: %d", code)
	}
	if code := a.call("DELETE", "/api/devices/"+id, "", nil); code != 200 {
		t.Fatalf("unpair: %d", code)
	}
	a.call("GET", "/api/devices", "", &listA)
	if len(listA.Devices) != 0 {
		t.Fatalf("unpair kept the device: %+v", listA)
	}
}
