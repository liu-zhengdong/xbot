package remote

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"xbot/internal/fsutil"
	"xbot/internal/gateway"
	"xbot/internal/pake"
)

// fakeOwner plays A by hand over a real gateway, so B's side is tested alone.
type fakeOwner struct {
	t        *testing.T
	c        *gateway.Client
	key, x   string
	id       string
	code     string
	b        string
	cb       *gateway.Client
	bKey, bX string
}

func newFakeOwner(t *testing.T) *fakeOwner {
	t.Helper()
	handler := (&gateway.Server{Dir: t.TempDir(), Token: "test"}).Handler()
	old := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: pairTransport(func(r *http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Result(), nil
	})}
	t.Cleanup(func() { http.DefaultClient = old })
	ownerRoot := pairingRoot(t)
	priv, id, key, err := Identity(ownerRoot)
	mustRemote(t, err)
	_, x, err := XIdentity(ownerRoot)
	mustRemote(t, err)
	o := &fakeOwner{t: t, c: &gateway.Client{Base: "http://gateway", Key: priv}, key: key, x: x, id: id, b: pairingRoot(t)}
	ctx := context.Background()
	mustRemote(t, o.c.Register(ctx, "test"))
	o.code, err = randomShortCode()
	mustRemote(t, err)
	mustRemote(t, o.c.RegisterCode(ctx, o.code[:4], 10*time.Minute))
	raw, _ := json.Marshal(map[string]string{"name": "Café laptop"})
	mustRemote(t, fsutil.WriteAtomic(filepath.Join(o.b, "remote", "device.json"), raw, 0600))
	kb, _, bKey, err := Identity(o.b)
	mustRemote(t, err)
	_, o.bX, err = XIdentity(o.b)
	mustRemote(t, err)
	o.cb, o.bKey = &gateway.Client{Base: o.c.Base, Key: kb}, bKey
	t.Cleanup(func() { pairings.Delete(o.b) })
	return o
}

// next returns the one envelope waiting for c.
func (o *fakeOwner) next(c *gateway.Client) gateway.Envelope {
	o.t.Helper()
	envs, err := c.Poll(context.Background(), 0)
	mustRemote(o.t, err)
	if len(envs) != 1 {
		o.t.Fatalf("want 1 envelope, got %d", len(envs))
	}
	mustRemote(o.t, c.Ack(context.Background(), []string{envs[0].ID}))
	return envs[0]
}
func (o *fakeOwner) deliverB() {
	o.t.Helper()
	_, self, _, err := Identity(o.b)
	mustRemote(o.t, err)
	_, _, err = Receive(o.b, self, []gateway.Envelope{o.next(o.cb)}, time.Now())
	mustRemote(o.t, err)
}

// answer reads B's pake1 and replies with pake2 run over secret.
func (o *fakeOwner) answer(secret string) pake.Keys {
	o.t.Helper()
	env := o.next(o.c)
	var m shortMessage
	mustRemote(o.t, json.Unmarshal(env.Body, &m))
	if env.Kind != "pake1" || m.Lookup != o.code[:4] || m.Key != o.bKey {
		o.t.Fatalf("bad pake1 %s %+v", env.Kind, m)
	}
	state, pb, err := pake.New(pake.RoleB, []byte(env.From), []byte(o.id), pake.Password(pairV2, []byte(o.code[:4]+secret)))
	mustRemote(o.t, err)
	keys, err := state.Finish(m.PA, pairAAD(o.code[:4], m.Key, m.XKey, o.key, o.x))
	mustRemote(o.t, err)
	reply := gateway.NewEnvelope(o.c.Key, env.From, "pake2", nil, time.Now())
	info := pairInfo{Name: "书房 iMac", OS: "macOS", Bots: []string{"ada"}, Deadline: time.Now().Add(2 * time.Minute).Unix()}
	sealed, err := sealPair(keys, reply, info)
	mustRemote(o.t, err)
	reply.Body, _ = json.Marshal(shortMessage{PB: pb, Key: o.key, XKey: o.x, Confirm: keys.MyConfirm, Info: sealed})
	reply.Sign(o.c.Key)
	mustRemote(o.t, o.c.Send(context.Background(), reply))
	o.deliverB()
	return keys
}

// Key path for the joiner with no gateway token: claim, SPAKE2, 连接, A allows.
func TestJoinShortCodeAgainstOwner(t *testing.T) {
	o := newFakeOwner(t)
	ctx := context.Background()
	mustRemote(t, JoinShortCode(ctx, o.b, "配对码 "+displayCode(o.code), o.c.Base))
	if got := PairingStatus(o.b).B.Phase; got != "checking" {
		t.Fatal("phase after claim:", got)
	}
	keys := o.answer(o.code[4:11])
	if got := PairingStatus(o.b).B; got.Phase != "confirm" || got.Name != "书房 iMac" || got.OS != "macOS" {
		t.Fatalf("B did not show A: %+v", got)
	}
	mustRemote(t, ConfirmJoin(ctx, o.b))
	env := o.next(o.c)
	var m shortMessage
	mustRemote(t, json.Unmarshal(env.Body, &m))
	pub, _ := base64.StdEncoding.DecodeString(o.bKey)
	var info pairInfo
	if env.Kind != "pake3" || env.Verify(pub) != nil || !keys.Verify(m.Confirm) || openPair(keys, env, m.Info, &info) != nil || info.Name != "Café laptop" {
		t.Fatal("bad pake3")
	}
	if got := PairingStatus(o.b).B.Phase; got != "waiting" {
		t.Fatal("phase after 连接:", got)
	}
	mustRemote(t, o.c.AdmitCode(ctx, gateway.ID(o.cb.Key.Public().(ed25519.PublicKey))))
	answer := gateway.NewEnvelope(o.c.Key, gateway.ID(o.cb.Key.Public().(ed25519.PublicKey)), "pake4", nil, time.Now())
	answer.Body, _ = sealPair(keys, answer, map[string]bool{"ok": true})
	answer.Sign(o.c.Key)
	mustRemote(t, o.c.Send(ctx, answer))
	o.deliverB()
	cfg, err := Load(o.b)
	mustRemote(t, err)
	if got := PairingStatus(o.b).B; got.Phase != "done" || len(cfg.Devices) != 1 || cfg.Devices[0].Key != o.key || cfg.Devices[0].Label != "书房 iMac" || cfg.Devices[0].Person == "" {
		t.Fatalf("not paired: %+v %+v", got, cfg.Devices)
	}
}
