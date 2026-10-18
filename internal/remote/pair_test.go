package remote

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/gateway"
	"xbot/internal/inbox"
	"xbot/internal/person"
)

func pairingRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	_, err := person.Identify(root, person.Account{Link: "terminal", ExternalID: "uid:1"}, "owner")
	mustRemote(t, err)
	return root
}

type pairTransport func(*http.Request) (*http.Response, error)

func (f pairTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestJoinRetry(t *testing.T) {
	handler := (&gateway.Server{Dir: t.TempDir(), Token: "join"}).Handler()
	failSend := true
	old := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: pairTransport(func(r *http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		if r.URL.Path == "/v1/send" && failSend {
			w.WriteHeader(503)
		} else {
			handler.ServeHTTP(w, r)
		}
		return w.Result(), nil
	})}
	t.Cleanup(func() { http.DefaultClient = old })
	a, b := pairingRoot(t), pairingRoot(t)
	ctx, now := context.Background(), time.Now()
	t.Setenv("XBOT_GATEWAY_TOKEN", "join")
	mustRemote(t, SetGateway(a, "http://gateway"))
	ka, self, _, err := Identity(a)
	mustRemote(t, err)
	client := &gateway.Client{Base: "http://gateway", Key: ka}
	mustRemote(t, client.Register(ctx, "join"))
	code, err := CreateInvite(a, "", now)
	mustRemote(t, err)
	if _, err := Join(ctx, b, code, "a", "", "", now); err == nil {
		t.Fatal("send should fail")
	}
	failSend = false
	// A device recorded without an xkey gets it from a later join.
	mustRemote(t, update(b, func(c *Config) error { c.Devices[0].XKey = ""; return nil }))
	_, err = Join(ctx, b, code, "a", "", "", now)
	mustRemote(t, err)
	if cfg, _ := Load(b); cfg.Devices[0].XKey == "" {
		t.Fatal("join did not fill the missing xkey")
	}
	kb, _, _, err := Identity(b)
	mustRemote(t, err)
	mustRemote(t, (&gateway.Client{Base: "http://gateway", Key: kb}).Register(ctx, ""))
	envs, err := client.Poll(ctx, 0)
	mustRemote(t, err)
	if len(envs) != 1 {
		t.Fatal("retry did not send pair")
	}
	ack, _, err := Receive(a, self, envs, now)
	mustRemote(t, err)
	if len(ack) != 1 {
		t.Fatal("pair not acknowledged")
	}
}

func TestReceivePair(t *testing.T) {
	for _, mode := range []string{"batch", "storage", "retry", "fill xkey", "key mismatch", "expired", "bad mac"} {
		t.Run(mode, func(t *testing.T) {
			sender, a, target, kb, _, self := pair(t)
			if mode != "retry" && mode != "fill xkey" && mode != "key mismatch" {
				mustRemote(t, RemoveDevice(a, "a"))
			}
			now := time.Now()
			mustRemote(t, SetGateway(a, "https://example.test"))
			code, err := CreateInvite(a, "", now)
			mustRemote(t, err)
			pc, err := ParseCode(code)
			mustRemote(t, err)
			_, _, key, err := Identity(sender)
			mustRemote(t, err)
			_, xkey, err := XIdentity(sender)
			mustRemote(t, err)
			env := gateway.NewEnvelope(kb, self, "pair", nil, now)
			secret, _ := base64.RawURLEncoding.DecodeString(pc.Secret)
			req := pairRequest{Name: "a", Key: key, XKey: xkey}
			req.Mac = base64.StdEncoding.EncodeToString(hmacSHA256(secret, pairDomain+key+"|"+xkey+"|a|"+self+"|"+env.ID))
			if mode == "bad mac" {
				req.Mac = base64.StdEncoding.EncodeToString(make([]byte, 32))
			}
			env.Body, _ = json.Marshal(req)
			env.Sign(kb)
			if mode == "storage" {
				path := filepath.Join(a, "remote", "devices.json.lock")
				mustRemote(t, os.Remove(path))
				mustRemote(t, os.Mkdir(path, 0700))
				ack, _, err := Receive(a, self, []gateway.Envelope{env}, now)
				if err == nil || len(ack) != 0 {
					t.Fatal("storage error was acknowledged")
				}
				var list []invite
				mustRemote(t, readJSON(filepath.Join(a, "remote", "invites.json"), &list))
				if list[0].Used {
					t.Fatal("failed AddDevice consumed invite")
				}
				mustRemote(t, os.Remove(path))
			}
			if mode == "fill xkey" {
				mustRemote(t, update(a, func(c *Config) error { c.Devices[0].XKey = ""; return nil }))
			}
			if mode == "key mismatch" {
				mustRemote(t, update(a, func(c *Config) error { c.Devices[0].Key = pc.Key; return nil }))
			}
			body, _ := json.Marshal(message{"ada", "bob", "hello", "chain", 1})
			msg, err := newBoxEnvelope(kb, Device{ID: self, XKey: pc.XKey}, "box", body, now)
			mustRemote(t, err)
			receivedAt := now
			if mode == "expired" {
				receivedAt = now.Add(inviteTTL)
			}
			ack, _, err := Receive(a, self, []gateway.Envelope{env, msg}, receivedAt)
			mustRemote(t, err)
			if len(ack) != 2 {
				t.Fatal("missing ack")
			}
			box, err := inbox.Open(target.InboxPath())
			mustRemote(t, err)
			want := 1
			if mode == "key mismatch" || mode == "expired" || mode == "bad mac" {
				want = 0
			}
			if len(box.Pending()) != want {
				t.Fatal("same-batch message lost or rejected pair accepted")
			}
			var list []invite
			mustRemote(t, readJSON(filepath.Join(a, "remote", "invites.json"), &list))
			if list[0].Used != (want == 1) { // retry also covers a crash between AddDevice and marking used
				t.Fatal("wrong invite consumption")
			}
			if cfg, _ := Load(a); mode == "fill xkey" && cfg.Devices[0].XKey != xkey {
				t.Fatal("pairing did not fill the missing xkey")
			}
			if mode == "batch" {
				mustRemote(t, RemoveDevice(a, "a"))
				for range 2 {
					ack, _, err = Receive(a, self, []gateway.Envelope{env}, now)
					mustRemote(t, err)
					cfg, err := Load(a)
					mustRemote(t, err)
					if len(ack) != 1 || len(cfg.Devices) != 0 {
						t.Fatal("replay restored removed device")
					}
					// Exercise the used invite independently of seen-id protection.
					mustRemote(t, os.Remove(filepath.Join(a, "remote", "seen.json")))
				}
			}
		})
	}
}

func TestInvitePerson(t *testing.T) {
	root, now := pairingRoot(t), time.Now()
	mustRemote(t, SetGateway(root, "https://example.test"))
	if _, err := CreateInvite(root, "missing", now); err == nil {
		t.Fatal("invalid --person accepted")
	}
	_, err := CreateInvite(root, "", now)
	mustRemote(t, err)
	var list []invite
	mustRemote(t, readJSON(filepath.Join(root, "remote", "invites.json"), &list))
	if len(list) != 1 || list[0].Person != "u1" {
		t.Fatal("default person not resolved")
	}
	_, err = person.Identify(root, person.Account{Link: "test", ExternalID: "second"}, "second")
	mustRemote(t, err)
	if _, err := CreateInvite(root, "", now); err == nil || !strings.Contains(err.Error(), "--person") {
		t.Fatal("ambiguous person accepted")
	}
	if _, err := CreateInvite(root, "u1", now); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XBOT_GATEWAY_TOKEN", strings.Repeat("x", codeMax))
	if _, err := CreateInvite(root, "u1", now); err == nil {
		t.Fatal("minted oversized code")
	}
}

// A default-gateway install with pairs refuses a code from another gateway instead of moving.
func TestJoinKeepsDefaultGatewayWithDevices(t *testing.T) {
	a, b := pairingRoot(t), pairingRoot(t)
	mustRemote(t, SetGateway(a, "http://other"))
	code, err := CreateInvite(a, "", time.Now())
	mustRemote(t, err)
	mustRemote(t, update(b, func(c *Config) error { c.Devices = []Device{{Name: "c", ID: "peer"}}; return nil }))
	if _, err := Join(context.Background(), b, code, "a", "", "", time.Now()); err == nil || !strings.Contains(err.Error(), "gateway mismatch") {
		t.Fatalf("join: %v", err)
	}
	if cfg, _ := Load(b); cfg.Gateway != "" {
		t.Fatalf("gateway moved to %q", cfg.Gateway)
	}
}

// A self-host join token in the environment never goes to the official default gateway.
func TestDefaultGatewayGetsNoToken(t *testing.T) {
	t.Setenv("XBOT_GATEWAY_TOKEN", "self-host-secret")
	a := pairingRoot(t)
	mustRemote(t, SetGateway(a, DefaultGateway+"/"))
	if cfg, _ := Load(a); cfg.Gateway != "" {
		t.Fatalf("official URL stored explicitly: %q", cfg.Gateway)
	}
	code, err := CreateInvite(a, "", time.Now())
	mustRemote(t, err)
	pc, err := ParseCode(code)
	mustRemote(t, err)
	if pc.Gateway != DefaultGateway || pc.Token != "" {
		t.Fatalf("code: gateway %q, token %q", pc.Gateway, pc.Token)
	}
	if cfg, _ := Load(a); cfg.GatewayToken("x") != "" {
		t.Fatal("run would send the token to the default gateway")
	}
	// Older configs and other spellings of the official URL count as the default too.
	for _, raw := range []string{"https://xbot.babelgo.cn", "https://XBOT.babelgo.cn:443/"} {
		mustRemote(t, update(a, func(c *Config) error { c.Gateway = raw; return nil }))
		if cfg, _ := Load(a); cfg.Gateway != "" || cfg.GatewayToken("x") != "" {
			t.Fatalf("%s: gateway %q", raw, cfg.Gateway)
		}
	}
}
