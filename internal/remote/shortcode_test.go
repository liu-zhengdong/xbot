package remote

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/fsutil"
	"xbot/internal/gateway"
	"xbot/internal/inbox"
	"xbot/internal/person"
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

func TestShortCodeGuessLimit(t *testing.T) {
	h := newShortHarness(t)
	lookup, secret, err := ParseShortCode(h.code)
	mustRemote(t, err)
	secret = string(gateway.CodeAlphabet[(strings.IndexByte(gateway.CodeAlphabet, secret[0])+1)%len(gateway.CodeAlphabet)]) + secret[1:]
	data := lookup + secret
	wrong := displayCode(data + string(checkChar(data)))
	for i := 0; i < gateway.MaxCodeClaims; i++ {
		mustRemote(t, JoinShortCode(context.Background(), h.b, wrong, h.ca.Base))
		h.deliver(h.a, h.ca)
		h.deliver(h.b, h.cb)
		if PairingStatus(h.b).B.Phase != "wrong" {
			t.Fatal("wrong PAKE password accepted")
		}
		assertNoShortDevices(t, h.a, h.b)
		// An unanswered session occupies its slot for two minutes; no extra guess.
		tickPairing(context.Background(), h.a, h.ca, time.Now().Add(2*time.Minute+time.Second))
	}
	if err := JoinShortCode(context.Background(), h.b, h.code, h.ca.Base); err == nil {
		t.Fatal("sixth claim accepted")
	}
	if PairingStatus(h.b).B.Phase != "expired" || PairingStatus(h.a).A.Phase != "expired" {
		t.Fatal("guess budget did not expire the code")
	}
}
func TestShortCodeMITM(t *testing.T) {
	h := newShortHarness(t)
	mustRemote(t, JoinShortCode(context.Background(), h.b, h.code, h.ca.Base))
	envs := h.poll(h.ca)
	if len(envs) != 1 {
		t.Fatal("missing pake1")
	}
	env := envs[0]
	var m shortMessage
	mustRemote(t, json.Unmarshal(env.Body, &m))
	pub, evil, err := ed25519.GenerateKey(rand.Reader)
	mustRemote(t, err)
	attacker := &gateway.Client{Base: h.ca.Base, Key: evil}
	mustRemote(t, attacker.Register(context.Background(), "test"))
	m.Key = base64.StdEncoding.EncodeToString(pub)
	env.From = gateway.ID(pub)
	env.Body, _ = json.Marshal(m)
	env.Sign(evil)
	_, aid, _, err := Identity(h.a)
	mustRemote(t, err)
	ack, _, err := Receive(h.a, aid, []gateway.Envelope{env}, time.Now())
	mustRemote(t, err)
	if len(ack) != 1 {
		t.Fatal("tampered handshake not acknowledged")
	}
	responses := h.poll(attacker)
	if len(responses) != 1 {
		t.Fatal("missing owner response")
	}
	reply := responses[0]
	_, bid, _, err := Identity(h.b)
	mustRemote(t, err)
	_, _, err = Receive(h.b, bid, []gateway.Envelope{reply}, time.Now())
	mustRemote(t, err)
	if PairingStatus(h.b).B.Phase != "checking" {
		t.Fatal("wrong recipient accepted")
	}
	// Give the relay authenticated forwarding as an extra power: even then,
	// changed identity/key bindings must make A's confirmation fail at B.
	reply.To = bid
	reply.Sign(h.ca.Key)
	_, _, err = Receive(h.b, bid, []gateway.Envelope{reply}, time.Now())
	mustRemote(t, err)
	// The first rejected id was acknowledged; use a fresh authenticated id.
	reply.ID = gateway.NewEnvelope(h.ca.Key, bid, "pake2", nil, time.Now()).ID
	reply.Sign(h.ca.Key)
	_, _, err = Receive(h.b, bid, []gateway.Envelope{reply}, time.Now())
	mustRemote(t, err)
	if PairingStatus(h.b).B.Phase != "wrong" {
		t.Fatal("substituted identity did not fail confirmation")
	}
	assertNoShortDevices(t, h.a, h.b)
}
func TestShortCodeDeny(t *testing.T) {
	h := newShortHarness(t)
	h.request()
	mustRemote(t, DecidePairing(context.Background(), h.a, false, false))
	h.deliver(h.b, h.cb)
	if PairingStatus(h.a).A.Phase != "denied" || PairingStatus(h.b).B.Phase != "denied" {
		t.Fatal("denial not delivered")
	}
	assertNoShortDevices(t, h.a, h.b)
	if err := JoinShortCode(context.Background(), h.b, h.code, h.ca.Base); err == nil || PairingStatus(h.b).B.Phase != "expired" {
		t.Fatal("denied code reused")
	}
}
func TestShortCodeCollision(t *testing.T) {
	for _, replace := range []bool{false, true} {
		t.Run(fmt.Sprint(replace), func(t *testing.T) {
			h := newShortHarness(t)
			oldRoot := pairingRoot(t)
			_, oldID, oldKey, err := Identity(oldRoot)
			mustRemote(t, err)
			_, oldX, err := XIdentity(oldRoot)
			mustRemote(t, err)
			_, err = person.Identify(h.a, person.Account{Link: "terminal", ExternalID: "uid:2"}, "other")
			mustRemote(t, err)
			old, err := AddDevice(h.a, "keep-address", oldKey, oldX, "u1") // 替换 only within one person.
			mustRemote(t, err)
			mustRemote(t, update(h.a, func(c *Config) error { c.Devices[0].Label = "Café laptop"; return nil }))
			cache := filepath.Join(h.a, "remote", "dirs", oldID+".json")
			mustRemote(t, fsutil.WriteAtomic(cache, []byte(`{}`), 0600))
			h.request()
			if collision := PairingStatus(h.a).A.Collision; collision == nil || collision.Name != "Café laptop" {
				t.Fatal("missing label collision")
			}
			mustRemote(t, DecidePairing(context.Background(), h.a, true, replace))
			h.deliver(h.b, h.cb)
			cfg, err := Load(h.a)
			mustRemote(t, err)
			if replace {
				if len(cfg.Devices) != 1 || cfg.Devices[0].Name != old.Name || cfg.Devices[0].Person != old.Person || cfg.Devices[0].Label != "Café laptop" || cfg.Devices[0].ID == old.ID {
					t.Fatal("replacement changed preserved fields")
				}
				if _, err := os.Stat(cache); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("old directory survived replacement")
				}
			} else {
				if len(cfg.Devices) != 2 || cfg.Devices[0].ID != old.ID || cfg.Devices[1].Label != "Café laptop 2" || cfg.Devices[1].Person != "u1" || cfg.Devices[1].Name != "Cafe-laptop-2" {
					t.Fatal("collision was not added separately")
				}
			}
		})
	}
}
func TestShortCodeTimers(t *testing.T) {
	for _, mode := range []string{"checking", "waiting", "request", "code", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			h := newShortHarness(t)
			switch mode {
			case "checking":
				mustRemote(t, JoinShortCode(context.Background(), h.b, h.code, h.ca.Base))
				tickPairing(context.Background(), h.b, h.cb, time.Now().Add(21*time.Second))
				if PairingStatus(h.b).B.Phase != "offline" {
					t.Fatal("missing checking deadline")
				}
			case "waiting":
				h.request()
				tickPairing(context.Background(), h.b, h.cb, time.Now().Add(2*time.Minute+16*time.Second))
				if PairingStatus(h.b).B.Phase != "denied" {
					t.Fatal("missing waiting deadline")
				}
			case "request":
				h.request()
				s := pairingFor(h.a)
				s.Lock()
				s.owner.expires = time.Now().Add(-time.Second)
				s.Unlock()
				tickPairing(context.Background(), h.a, h.ca, time.Now())
				if PairingStatus(h.a).A.Phase != "request" {
					t.Fatal("code expiry interrupted request")
				}
				tickPairing(context.Background(), h.a, h.ca, time.Now().Add(2*time.Minute+time.Second))
				h.deliver(h.b, h.cb)
				if PairingStatus(h.a).A.Phase != "denied" || PairingStatus(h.b).B.Phase != "denied" {
					t.Fatal("request not auto-denied")
				}
			case "code":
				tickPairing(context.Background(), h.a, h.ca, time.Now().Add(inviteTTL+time.Second))
				if PairingStatus(h.a).A.Phase != "expired" {
					t.Fatal("code did not expire")
				}
			case "cancel":
				CancelShortCode(context.Background(), h.a)
				if PairingStatus(h.a).A.Phase != "none" {
					t.Fatal("owner cancellation failed")
				}
				mustRemote(t, func() error {
					err := JoinShortCode(context.Background(), h.b, h.code, h.ca.Base)
					if err == nil {
						return errors.New("cancelled code accepted")
					}
					return nil
				}())
				CancelJoin(h.b)
				if PairingStatus(h.b).B.Phase != "none" {
					t.Fatal("join cancellation failed")
				}
			}
			assertNoShortDevices(t, h.a, h.b)
		})
	}
}
func TestShortCodeFailurePhases(t *testing.T) {
	for _, mode := range []string{"typo", "unknown", "network", "503", "expired", "already"} {
		t.Run(mode, func(t *testing.T) {
			h := newShortHarness(t)
			code, want := h.code, ""
			switch mode {
			case "typo":
				code = "bad"
				want = "typo"
			case "unknown":
				raw, err := randomShortCode()
				mustRemote(t, err)
				code = displayCode(raw)
				want = "wrong_or_used"
			case "network":
				want = "gateway_unreachable"
			case "503":
				want = "gateway_unreachable"
			case "expired":
				CancelShortCode(context.Background(), h.a)
				want = "expired"
			case "already":
				_, _, key, err := Identity(h.a)
				mustRemote(t, err)
				_, xkey, err := XIdentity(h.a)
				mustRemote(t, err)
				mustRemote(t, SetGateway(h.b, h.ca.Base)) // Devices on another gateway are a mismatch.
				_, err = AddDevice(h.b, "known", key, xkey, "")
				mustRemote(t, err)
				want = "already"
			}
			old := http.DefaultClient
			if mode == "network" || mode == "503" || mode == "typo" {
				http.DefaultClient = &http.Client{Transport: pairTransport(func(r *http.Request) (*http.Response, error) {
					if mode == "typo" {
						t.Fatal("typo performed network I/O")
					}
					if mode == "network" {
						return nil, errors.New("unavailable")
					}
					return &http.Response{StatusCode: 503, Status: "503", Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
				})}
				defer func() { http.DefaultClient = old }()
			}
			_ = JoinShortCode(context.Background(), h.b, code, h.ca.Base)
			got := PairingStatus(h.b).B
			if got.Phase != want {
				t.Fatalf("phase=%s, want %s", got.Phase, want)
			}
			if want == "gateway_unreachable" && got.Gateway != h.ca.Base {
				t.Fatal("missing failing gateway")
			}
		})
	}
}
func TestShortCodeStatusDuringSend(t *testing.T) {
	h := newShortHarness(t)
	old := http.DefaultClient
	entered, release := make(chan struct{}), make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(release)
		}
		http.DefaultClient = old
	}()
	http.DefaultClient = &http.Client{Transport: pairTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/v1/send" {
			close(entered)
			select {
			case <-release:
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
		}
		return old.Transport.RoundTrip(r)
	})}
	done := make(chan error, 1)
	go func() { done <- JoinShortCode(context.Background(), h.b, h.code, h.ca.Base) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("join did not reach send")
	}
	got := make(chan Status, 1)
	started := time.Now()
	go func() { got <- PairingStatus(h.b) }()
	select {
	case status := <-got:
		if status.B.Phase != "checking" {
			t.Fatal("missing checking snapshot")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("status blocked on gateway operation")
	}
	t.Logf("status during blocked send: %s", time.Since(started))
	close(release)
	released = true
	mustRemote(t, <-done)
}

func TestShortCodeFinalSessionCanPair(t *testing.T) {
	h := newShortHarness(t)
	lookup, secret, err := ParseShortCode(h.code)
	mustRemote(t, err)
	secret = string(gateway.CodeAlphabet[(strings.IndexByte(gateway.CodeAlphabet, secret[0])+1)%len(gateway.CodeAlphabet)]) + secret[1:]
	data := lookup + secret
	wrong := displayCode(data + string(checkChar(data)))
	for range gateway.MaxCodeClaims - 1 {
		mustRemote(t, JoinShortCode(context.Background(), h.b, wrong, h.ca.Base))
		h.deliver(h.a, h.ca)
		h.deliver(h.b, h.cb)
		tickPairing(context.Background(), h.a, h.ca, time.Now().Add(2*time.Minute+time.Second))
	}
	h.request()
	mustRemote(t, DecidePairing(context.Background(), h.a, true, false))
	h.deliver(h.b, h.cb)
	if PairingStatus(h.b).B.Phase != "done" {
		t.Fatal("final allowed attempt could not finish")
	}
}

func TestShortCodeReplacesPrevious(t *testing.T) {
	h := newShortHarness(t)
	previous := h.code
	t.Setenv("XBOT_GATEWAY_TOKEN", "") // Known installations re-register without a token.
	code, expires, err := NewShortCode(context.Background(), h.a, "u1")
	mustRemote(t, err)
	if code == previous || time.Until(expires) < 9*time.Minute {
		t.Fatal("new code was not minted")
	}
	if err := JoinShortCode(context.Background(), h.b, previous, h.ca.Base); err == nil || PairingStatus(h.b).B.Phase != "wrong_or_used" {
		// The gateway keeps one record per owner, so a replaced code is unknown (404).
		t.Fatal("previous code survived replacement")
	}
	h.code = code
	h.join()
	mustRemote(t, SetGateway(h.b, h.ca.Base))
	// A paired device prevents moving this data root to a different gateway.
	_, _, key, err := Identity(h.a)
	mustRemote(t, err)
	_, xkey, err := XIdentity(h.a)
	mustRemote(t, err)
	_, err = AddDevice(h.b, "owner", key, xkey, "")
	mustRemote(t, err)
	if err := JoinShortCode(context.Background(), h.b, code, "http://different"); err == nil {
		t.Fatal("changed gateway with paired devices")
	}
	cfg, err := Load(h.b)
	mustRemote(t, err)
	if cfg.Gateway != h.ca.Base {
		t.Fatal("gateway changed on rejection")
	}
}

func TestShortCodePairedSender(t *testing.T) {
	h := newShortHarness(t)
	_, _, key, err := Identity(h.b)
	mustRemote(t, err)
	_, err = AddDevice(h.a, "known", key, "", "")
	mustRemote(t, err)
	h.request()
	mustRemote(t, DecidePairing(context.Background(), h.a, true, false))
	h.deliver(h.b, h.cb)
	cfg, err := Load(h.a)
	mustRemote(t, err)
	_, xkey, err := XIdentity(h.b)
	mustRemote(t, err)
	if len(cfg.Devices) != 1 || cfg.Devices[0].Name != "known" || cfg.Devices[0].XKey != xkey {
		t.Fatal("paired sender did not repair encryption identity in place")
	}
}

// Review P2: a failed pake2 send leaves pake1 unacked; its redelivery must
// resend the same pake2 instead of being swallowed by the active-session guard.
func TestShortCodeRedeliveredPake1ResendsPake2(t *testing.T) {
	h := newShortHarness(t)
	mustRemote(t, JoinShortCode(context.Background(), h.b, h.code, h.ca.Base))
	_, self, _, err := Identity(h.a)
	mustRemote(t, err)
	envs := h.poll(h.ca)
	h.failOn = "pake2"
	if ack, _, err := Receive(h.a, self, envs, time.Now()); err == nil || len(ack) != 0 {
		t.Fatalf("failed pake2 send must leave pake1 unacked: ack=%v err=%v", ack, err)
	}
	ack, _, err := Receive(h.a, self, envs, time.Now())
	mustRemote(t, err)
	mustRemote(t, h.ca.Ack(context.Background(), ack))
	h.deliver(h.b, h.cb)
	if got := PairingStatus(h.b).B; got.Phase != "confirm" {
		t.Fatalf("B after redelivery: %+v", got)
	}
}

// Review P2: once A allowed (B admitted, device saved), a failed pake4 send is
// retried with the same answer; the request timeout must not turn it into a denial.
func TestShortCodeCommittedAllowSurvivesTimeout(t *testing.T) {
	h := newShortHarness(t)
	h.request()
	h.failOn = "pake4"
	if err := DecidePairing(context.Background(), h.a, true, false); err == nil {
		t.Fatal("injected pake4 failure not reported")
	}
	tickPairing(context.Background(), h.a, h.ca, time.Now().Add(2*time.Minute+time.Second))
	h.deliver(h.b, h.cb)
	if got := PairingStatus(h.b).B; got.Phase != "done" {
		t.Fatalf("B got %+v, want done", got)
	}
	if got := PairingStatus(h.a).A; got.Phase != "done" {
		t.Fatalf("A got %+v, want done", got)
	}
}

// Review P2: if the gateway cannot admit B (its pending install expired), A
// must not save B.
func TestShortCodeFailedAdmissionSavesNothing(t *testing.T) {
	h := newShortHarness(t)
	h.request()
	_, bid, _, err := Identity(h.b)
	mustRemote(t, err)
	mustRemote(t, os.Remove(filepath.Join(h.gwDir, "pending", bid)))
	if err := DecidePairing(context.Background(), h.a, true, false); err == nil {
		t.Fatal("admission failure not reported")
	}
	assertNoShortDevices(t, h.a)
}

// Review P2: B's confirmation window ends no later than A's session deadline.
func TestShortCodeJoinerDeadlineWithinOwner(t *testing.T) {
	h := newShortHarness(t)
	start := time.Now()
	h.join()
	got := PairingStatus(h.b).B
	if !got.Expires.Before(start.Add(2*time.Minute)) || got.Expires.Before(start.Add(time.Minute)) {
		t.Fatalf("B confirm deadline %v not inside A's 2-minute session from %v", got.Expires, start)
	}
}
