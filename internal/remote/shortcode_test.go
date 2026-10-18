package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/fsutil"
	"xbot/internal/gateway"
	"xbot/internal/inbox"
)

type shortHarness struct {
	t      *testing.T
	a, b   string
	ca, cb *gateway.Client
	code   string
	gwDir  string
	failOn string // fail the next send of this envelope kind, once
}

func newShortHarness(t *testing.T) *shortHarness {
	t.Helper()
	h := &shortHarness{t: t, a: pairingRoot(t), b: pairingRoot(t), gwDir: t.TempDir()}
	handler := (&gateway.Server{Dir: h.gwDir, Token: "test"}).Handler()
	old := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: pairTransport(func(r *http.Request) (*http.Response, error) {
		if h.failOn != "" && r.URL.Path == "/v1/send" && r.Body != nil {
			body, _ := io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(body))
			if bytes.Contains(body, []byte(`"kind":"`+h.failOn+`"`)) {
				h.failOn = ""
				return nil, errors.New("injected send failure")
			}
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Result(), nil
	})}
	t.Cleanup(func() { http.DefaultClient = old })
	t.Setenv("XBOT_GATEWAY_TOKEN", "test")
	for _, v := range []struct{ root, label, bot string }{{h.a, "书房 iMac", "ada"}, {h.b, "Café laptop", "bob"}} {
		raw, _ := json.Marshal(map[string]string{"name": v.label})
		mustRemote(t, fsutil.WriteAtomic(filepath.Join(v.root, "remote", "device.json"), raw, 0600))
		b, err := bot.Create(v.root, v.bot, "", nil)
		mustRemote(t, err)
		mustRemote(t, b.SetOwners([]string{"u1"}))
	}
	mustRemote(t, SetGateway(h.a, "http://gateway"))
	var err error
	h.ca, err = shortClient(h.a)
	mustRemote(t, err)
	h.code, _, err = NewShortCode(context.Background(), h.a, "")
	mustRemote(t, err)
	kb, _, _, err := Identity(h.b)
	mustRemote(t, err)
	h.cb = &gateway.Client{Base: h.ca.Base, Key: kb}
	t.Cleanup(func() { pairings.Delete(h.a); pairings.Delete(h.b) })
	return h
}
func (h *shortHarness) poll(c *gateway.Client) []gateway.Envelope {
	h.t.Helper()
	envs, err := c.Poll(context.Background(), 0)
	mustRemote(h.t, err)
	return envs
}
func (h *shortHarness) deliver(root string, c *gateway.Client) {
	h.t.Helper()
	_, self, _, err := Identity(root)
	mustRemote(h.t, err)
	envs := h.poll(c)
	if len(envs) == 0 {
		h.t.Fatal("expected pairing envelope")
	}
	ack, replies, err := Receive(root, self, envs, time.Now())
	mustRemote(h.t, err)
	if len(ack) != len(envs) {
		h.t.Fatal("missing acknowledgment")
	}
	mustRemote(h.t, c.Ack(context.Background(), ack))
	for _, e := range replies {
		mustRemote(h.t, c.Send(context.Background(), e))
	}
}
func (h *shortHarness) join() {
	h.t.Helper()
	mustRemote(h.t, JoinShortCode(context.Background(), h.b, h.code, h.ca.Base))
	h.deliver(h.a, h.ca)
	h.deliver(h.b, h.cb)
	if got := PairingStatus(h.b).B; got.Phase != "confirm" || got.Name != "书房 iMac" || !slices.Equal(got.Bots, []string{"ada"}) {
		h.t.Fatal("join did not reach owner confirmation")
	}
}
func (h *shortHarness) request() {
	h.t.Helper()
	h.join()
	mustRemote(h.t, ConfirmJoin(context.Background(), h.b))
	h.deliver(h.a, h.ca)
	if got := PairingStatus(h.a).A; got.Phase != "request" || got.Name != "Café laptop" || !slices.Equal(got.Bots, []string{"bob"}) {
		h.t.Fatal("owner did not receive join request")
	}
}
func assertNoShortDevices(t *testing.T, roots ...string) {
	t.Helper()
	for _, root := range roots {
		cfg, err := Load(root)
		mustRemote(t, err)
		if len(cfg.Devices) != 0 {
			t.Fatal("unexpected paired device")
		}
	}
}
func TestShortCodeKeyPath(t *testing.T) {
	h := newShortHarness(t)
	started := time.Now()
	h.request()
	assertNoShortDevices(t, h.a, h.b)
	// UI snapshots must not permit a caller to modify the live request.
	snapshot := PairingStatus(h.a)
	snapshot.A.Bots[0] = "other"
	if PairingStatus(h.a).A.Bots[0] != "bob" {
		t.Fatal("status aliases mutable state")
	}
	mustRemote(t, DecidePairing(context.Background(), h.a, true, false))
	h.deliver(h.b, h.cb)
	for _, v := range []struct{ root, other string }{{h.a, h.b}, {h.b, h.a}} {
		cfg, err := Load(v.root)
		mustRemote(t, err)
		_, id, key, err := Identity(v.other)
		mustRemote(t, err)
		_, xkey, err := XIdentity(v.other)
		mustRemote(t, err)
		if len(cfg.Devices) != 1 || cfg.Devices[0].ID != id || cfg.Devices[0].Key != key || cfg.Devices[0].XKey != xkey || cfg.Devices[0].Person != "u1" || !namePattern.MatchString(cfg.Devices[0].Name) {
			t.Fatal("incorrect paired identity")
		}
	}
	if PairingStatus(h.a).A.Phase != "done" || PairingStatus(h.b).B.Phase != "done" {
		t.Fatal("pairing not complete")
	}
	// The installed xkeys must support real encrypted mailbox traffic both ways.
	for _, v := range []struct {
		from, to, src, dst string
		c                  *gateway.Client
	}{{h.a, h.b, "ada", "bob", h.ca}, {h.b, h.a, "bob", "ada", h.cb}} {
		cfg, err := Load(v.from)
		mustRemote(t, err)
		body, _ := json.Marshal(message{v.src, v.dst, "hello", "chain", 1})
		env, err := newBoxEnvelope(v.c.Key, cfg.Devices[0], "box", body, time.Now())
		mustRemote(t, err)
		_, id, _, err := Identity(v.to)
		mustRemote(t, err)
		ack, _, err := Receive(v.to, id, []gateway.Envelope{env}, time.Now())
		mustRemote(t, err)
		if len(ack) != 1 {
			t.Fatal("box not acknowledged")
		}
		target, err := bot.Load(v.to, v.dst)
		mustRemote(t, err)
		box, err := inbox.Open(target.InboxPath())
		mustRemote(t, err)
		if len(box.Pending()) != 1 {
			t.Fatal("encrypted box was not delivered")
		}
	}
	t.Logf("in-process short pairing, including device writes: %s", time.Since(started))
	// Codes and PAKE session material must never be written under remote/.
	lookup, secret, err := ParseShortCode(h.code)
	mustRemote(t, err)
	for _, root := range []string{h.a, h.b} {
		mustRemote(t, filepath.WalkDir(filepath.Join(root, "remote"), func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			data, err := os.ReadFile(path)
			if bytes.Contains(data, []byte(h.code)) || bytes.Contains(data, []byte(lookup+secret)) || bytes.Contains(data, []byte(secret)) {
				t.Fatal("short code persisted")
			}
			return err
		}))
	}
}
