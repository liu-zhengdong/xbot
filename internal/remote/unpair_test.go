package remote

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/gateway"
	"xbot/internal/inbox"
)

// gatewayPair pairs A and B through a real in-process gateway (like
// TestSendGatewayReceive) and gives both sides the same address.
func gatewayPair(t *testing.T) (a, b string, ka, kb ed25519.PrivateKey) {
	t.Helper()
	a, b, _, ka, kb, _ = pair(t)
	handler := (&gateway.Server{Dir: t.TempDir(), Token: "join"}).Handler()
	old := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: pairTransport(func(r *http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Result(), nil
	})}
	t.Cleanup(func() { http.DefaultClient = old })
	for _, key := range []ed25519.PrivateKey{ka, kb} {
		mustRemote(t, (&gateway.Client{Base: "http://gateway", Key: key}).Register(context.Background(), "join"))
	}
	return a, b, ka, kb
}

// Unpairing on A tells B in one kind unpair envelope: B keeps its device for A,
// marked as unpaired by the peer, refuses to send to it, and drops what it
// still receives from it; A has no device for B any more.
func TestUnpairNotifiesPeer(t *testing.T) {
	a, b, ka, kb := gatewayPair(t)
	mustRemote(t, SetGateway(a, "http://gateway"))
	mustRemote(t, SetGateway(b, "http://gateway"))
	_, aid, _, err := Identity(a)
	mustRemote(t, err)
	_, bid, _, err := Identity(b)
	mustRemote(t, err)
	cfg, err := Load(b)
	mustRemote(t, err)
	if len(cfg.Devices) != 1 || cfg.Devices[0].ID != aid {
		t.Fatalf("B is not paired with A: %+v", cfg.Devices)
	}
	cb := &gateway.Client{Base: "http://gateway", Key: kb}
	sender, err := bot.Create(b, "ada", "", nil) // For B's side; it is also a valid bot@a name.
	mustRemote(t, err)
	mustRemote(t, sender.SetOwners([]string{"u1"}))
	now := time.Now()
	acfg, err := Load(a)
	mustRemote(t, err)
	bOnA := acfg.Devices[0] // A's record of B, to forge a late box from A below.

	mustRemote(t, RemoveDeviceID(a, bid))

	envs, err := cb.Poll(context.Background(), 0)
	mustRemote(t, err)
	if len(envs) != 1 || envs[0].Kind != "unpair" || envs[0].From != aid || envs[0].To != bid {
		t.Fatalf("A did not send one unpair envelope to B: %+v", envs)
	}
	// A box from A in the same poll batch, after the unpair, is dropped too.
	box, err := newBoxEnvelope(ka, bOnA, "box", []byte(`{"from_bot":"bob","to_bot":"ada","text":"late","chain":"c","hops":1}`), now)
	mustRemote(t, err)
	ack, _, err := Receive(b, bid, append(envs, box), now)
	mustRemote(t, err)
	if len(ack) != 2 {
		t.Fatal("unpair envelope and box not acknowledged")
	}
	cfg, err = Load(b)
	mustRemote(t, err)
	if len(cfg.Devices) != 1 || cfg.Devices[0].UnpairedAt.IsZero() {
		t.Fatalf("B did not mark A as unpaired: %+v", cfg.Devices)
	}
	if cfg, err = Load(a); err != nil || len(cfg.Devices) != 0 {
		t.Fatalf("A still has a device after unpair: %+v %v", cfg.Devices, err)
	}
	if _, err := Send(context.Background(), b, "ada", "bob@a", "hello", "chain", 1, now); err == nil || err.Error() != "remote: device a was unpaired by the other computer" {
		t.Fatalf("send to an unpaired device: %v", err)
	}
	in, err := inbox.Open(sender.InboxPath())
	mustRemote(t, err)
	if len(in.Pending()) != 0 {
		t.Fatal("box from a device that unpaired us was delivered")
	}
}

// An unreachable gateway never blocks or fails the unpair: the device is gone
// locally and the call returns nil at once.
func TestUnpairUnreachableGateway(t *testing.T) {
	a, _, _, _ := gatewayPair(t)
	mustRemote(t, SetGateway(a, "http://127.0.0.1:1"))
	cfg, err := Load(a)
	mustRemote(t, err)
	id := cfg.Devices[0].ID
	started := time.Now()
	mustRemote(t, RemoveDeviceID(a, id))
	if took := time.Since(started); took > time.Second {
		t.Fatalf("unpair waited on an unreachable gateway: %s", took)
	}
	if cfg, err = Load(a); err != nil || len(cfg.Devices) != 0 {
		t.Fatalf("device survived a failed unpair notice: %+v %v", cfg.Devices, err)
	}
}

// A stalled gateway is cut off at the 5 second deadline instead of holding the
// unpair open, and the device is still removed.
func TestUnpairNoticeTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("waits for the 5 second unpair timeout")
	}
	a, _, _, _ := gatewayPair(t)
	mustRemote(t, SetGateway(a, "http://gateway"))
	old := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: pairTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/v1/send" {
			<-r.Context().Done() // The gateway never answers.
			return nil, r.Context().Err()
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	})}
	defer func() { http.DefaultClient = old }()
	cfg, err := Load(a)
	mustRemote(t, err)
	started := time.Now()
	mustRemote(t, RemoveDeviceID(a, cfg.Devices[0].ID))
	if took := time.Since(started); took < unpairTimeout || took > unpairTimeout+2*time.Second {
		t.Fatalf("unpair notice stopped after %s, want about %s", took, unpairTimeout)
	}
	if cfg, err = Load(a); err != nil || len(cfg.Devices) != 0 {
		t.Fatalf("device survived a timed out unpair notice: %+v %v", cfg.Devices, err)
	}
}

// Pairing the same device again clears the unpaired mark, whichever path does it.
func TestRepairClearsUnpaired(t *testing.T) {
	root := pairingRoot(t)
	_, id, key, _ := randomKey(t)
	x := base64.StdEncoding.EncodeToString(make([]byte, 32))
	d, err := AddDevice(root, "mbp", key, x, "")
	mustRemote(t, err)
	mark := func() {
		mustRemote(t, update(root, func(c *Config) error {
			for i := range c.Devices {
				if c.Devices[i].ID == id {
					c.Devices[i].UnpairedAt = time.Now().UTC()
				}
			}
			return nil
		}))
	}
	unpaired := func(step string) {
		t.Helper()
		cfg, err := Load(root)
		mustRemote(t, err)
		if len(cfg.Devices) != 1 || cfg.Devices[0].ID != id {
			t.Fatalf("%s: %+v", step, cfg.Devices)
		}
		if step == "marked" && cfg.Devices[0].UnpairedAt.IsZero() {
			t.Fatal("mark not set")
		}
		if step != "marked" && !cfg.Devices[0].UnpairedAt.IsZero() {
			t.Fatalf("%s kept the unpaired mark: %+v", step, cfg.Devices[0])
		}
	}
	mark()
	unpaired("marked")
	if _, err := savePairDevice(root, d.Label, key, x, "", false); err != nil {
		t.Fatal(err)
	}
	unpaired("pair again")
	mark()
	if err := RemoveDevice(root, d.Name); err != nil {
		t.Fatal(err)
	}
	if _, err := AddDevice(root, d.Name, key, x, ""); err != nil {
		t.Fatal(err)
	}
	unpaired("add again")
}

// The body is just {"at": <time>}; a garbled one changes nothing.
func TestReceiveUnpairBody(t *testing.T) {
	_, b, _, _ := gatewayPair(t)
	cfg, err := Load(b)
	mustRemote(t, err)
	peer := cfg.Devices[0]
	if err := receiveUnpair(b, peer.ID, []byte(`{`), time.Now()); err != nil {
		t.Fatal(err)
	}
	if cfg, err = Load(b); err != nil || !cfg.Devices[0].UnpairedAt.IsZero() {
		t.Fatalf("garbled unpair body marked the device: %+v", cfg.Devices)
	}
	if err := receiveUnpair(b, peer.ID, []byte(`{"at":1}`), time.Now()); err != nil {
		t.Fatal(err)
	}
	if cfg, err = Load(b); err != nil || cfg.Devices[0].UnpairedAt.IsZero() {
		t.Fatalf("valid unpair body not applied: %v %+v", err, cfg.Devices)
	}
}
