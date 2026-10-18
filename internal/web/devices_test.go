package web

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
