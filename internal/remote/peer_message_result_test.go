package remote

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/gateway"
	"xbot/internal/inbox"
	"xbot/internal/peer"
)

// The handler still accepts and stores the signed envelope before this read fails.
type peerMessageLostBody struct{ io.ReadCloser }

func (b peerMessageLostBody) Read([]byte) (int, error) {
	return 0, errors.New("fixture response stream lost")
}

func TestPeerMessageRuntimeGatewayResults(t *testing.T) {
	for _, mode := range []string{"accepted", "503", "507", "429", "408", "400", "lost_response", "lost_response_queue_fault", "http_queue_fault", "transport_queue_fault"} {
		t.Run(mode, func(t *testing.T) {
			a, b, target, ka, kb, self := pair(t)
			sender, err := bot.Create(a, "ada", "", nil)
			mustRemote(t, err)
			mustRemote(t, sender.SetOwners([]string{"u1"}))
			mustRemote(t, SetGateway(a, "http://gateway"))
			handler := (&gateway.Server{Dir: t.TempDir(), Open: true}).Handler()
			calls, failing := 0, true
			var attempts []gateway.Envelope
			old := http.DefaultClient
			http.DefaultClient = &http.Client{Transport: pairTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host != "gateway" {
					t.Fatal("unexpected outbound destination")
				}
				isSend := r.URL.Path == "/v1/send"
				if isSend {
					calls++
					if deadline, ok := r.Context().Deadline(); !ok || time.Until(deadline) > 15*time.Second {
						t.Fatal("gateway request has no original timeout bound")
					}
					raw, err := io.ReadAll(r.Body)
					mustRemote(t, err)
					var env gateway.Envelope
					mustRemote(t, json.Unmarshal(raw, &env))
					attempts = append(attempts, env)
					r.Body = io.NopCloser(bytes.NewReader(raw))
				}
				w := httptest.NewRecorder()
				if isSend && failing {
					switch mode {
					case "503", "507", "429", "408", "400", "http_queue_fault":
						status := map[string]int{"503": 503, "507": 507, "429": 429, "408": 408, "400": 400, "http_queue_fault": 503}[mode]
						w.WriteHeader(status)
						return w.Result(), nil
					case "transport_queue_fault":
						return nil, errors.New("fixture transport interrupted")
					}
				}
				handler.ServeHTTP(w, r)
				resp := w.Result()
				if isSend && failing && strings.HasPrefix(mode, "lost_response") {
					if resp.StatusCode < 200 || resp.StatusCode >= 300 {
						t.Fatal("real gateway did not accept envelope before response loss")
					}
					resp.Body = peerMessageLostBody{resp.Body}
				}
				return resp, nil
			})}
			t.Cleanup(func() { http.DefaultClient = old })
			ctx, now := context.Background(), time.Now()
			ca, cb := &gateway.Client{Base: "http://gateway", Key: ka}, &gateway.Client{Base: "http://gateway", Key: kb}
			mustRemote(t, ca.Register(ctx, ""))
			mustRemote(t, cb.Register(ctx, ""))
			queueFault := strings.HasSuffix(mode, "queue_fault")
			if queueFault {
				mustRemote(t, os.WriteFile(filepath.Join(a, "remote", "outbox"), []byte("fixture filesystem fault"), 0600))
			}
			// A fresh pair has no directory announcement for bob. Sending must not require one.
			if cache, err := VisibleBots(a, sender.Config().Owners, now); err != nil || len(cache) != 0 {
				t.Fatal("fixture unexpectedly has a remote directory")
			}
			restore := bot.SetRedactText(func(text string) string { return strings.ReplaceAll(text, "fixture-private", "[redacted]") })
			defer restore()
			result, sendErr := SendWithResult(ctx, a, "ada", "bob@b", "  hello fixture-private  ", "original-chain", 2, now)
			if result.EnvelopeID == "" || len(attempts) != 1 || result.EnvelopeID != attempts[0].ID || calls != 1 {
				t.Fatal("attempt did not expose exactly its actual envelope")
			}
			wantGateway, wantStatus := "http_error", 0
			switch mode {
			case "accepted":
				wantGateway = "accepted"
			case "lost_response", "lost_response_queue_fault", "transport_queue_fault":
				wantGateway = "unknown"
			default:
				wantStatus = map[string]int{"503": 503, "507": 507, "429": 429, "408": 408, "400": 400, "http_queue_fault": 503}[mode]
			}
			wantQueued := mode != "accepted" && mode != "400" && !queueFault
			wantStage := "complete"
			if mode == "400" {
				wantStage = "gateway"
			} else if queueFault {
				wantStage = "queue"
			}
			if result.Gateway != wantGateway || result.GatewayStatus != wantStatus || result.Queued != wantQueued || result.Stage != wantStage || (sendErr != nil) != (queueFault || mode == "400") {
				t.Fatalf("gateway=%s status=%d queued=%v stage=%s error=%v", result.Gateway, result.GatewayStatus, result.Queued, result.Stage, sendErr != nil)
			}
			var rate struct {
				Hits map[string][]int64 `json:"hits"`
			}
			mustRemote(t, readJSON(filepath.Join(a, "peer-rate.json"), &rate))
			if len(rate.Hits["remote-out:ada->bob@b"]) != 1 {
				t.Fatal("send did not preserve its rate consumption after gateway or queue failure")
			}
			original := attempts[0]
			if original.Verify(ka.Public().(ed25519.PublicKey)) != nil || original.Kind != "box" || original.To != self || bytes.Contains(original.Body, []byte("hello")) {
				t.Fatal("original encrypted signed envelope contract changed")
			}
			xpriv, _, err := XIdentity(b)
			mustRemote(t, err)
			plain, err := openBox(xpriv, original)
			mustRemote(t, err)
			var msg message
			mustRemote(t, json.Unmarshal(plain, &msg))
			if msg.From != "ada" || msg.To != "bob" || msg.Text != "hello [redacted]" || msg.Chain != "original-chain" || msg.Hops != 2 {
				t.Fatal("frozen message metadata or redaction changed")
			}
			if wantQueued {
				path := filepath.Join(a, "remote", "outbox", result.EnvelopeID+".json")
				var item outboxItem
				mustRemote(t, readJSON(path, &item))
				info, err := os.Stat(path)
				mustRemote(t, err)
				if !reflect.DeepEqual(item.Env, original) || item.Attempts != 1 || !item.Next.Equal(now.Add(5*time.Second)) || !item.Created.Equal(now) || item.Snippet != "hello [redacted]" || (runtime.GOOS != "windows" && info.Mode().Perm() != 0600) {
					t.Fatal("durable queue lost envelope, due time, redaction, or permissions")
				}
				mustRemote(t, flushOutbox(ctx, a, ca, now.Add(4*time.Second)))
				if calls != 1 {
					t.Fatal("retry ran before original five-second due time")
				}
				if mode == "503" {
					mustRemote(t, flushOutbox(ctx, a, ca, now.Add(5*time.Second)))
					mustRemote(t, readJSON(path, &item))
					if item.Attempts != 2 || !item.Next.Equal(now.Add(15*time.Second)) {
						t.Fatal("original retry backoff changed")
					}
				}
				failing = false
				mustRemote(t, flushOutbox(ctx, a, ca, item.Next))
				if len(outboxItems(t, a)) != 0 {
					t.Fatal("successful retry retained its queue file")
				}
				for _, retried := range attempts {
					if !reflect.DeepEqual(retried, original) {
						t.Fatal("retry rebuilt or changed the original envelope")
					}
				}
			} else if !queueFault && len(outboxItems(t, a)) != 0 {
				t.Fatal("accepted or permanent failure unexpectedly queued")
			}
			envs, err := cb.Poll(ctx, 0)
			mustRemote(t, err)
			wantStored := mode == "accepted" || wantQueued || mode == "lost_response_queue_fault"
			if !wantStored {
				if len(envs) != 0 {
					t.Fatal("gateway stored an envelope it rejected")
				}
				return
			}
			if len(envs) != 1 || !reflect.DeepEqual(envs[0], original) {
				t.Fatal("real gateway did not retain exactly the original envelope")
			}
			receivedAt := now.Add(20 * time.Second)
			for range 2 {
				ack, bounces, err := Receive(b, self, envs, receivedAt)
				mustRemote(t, err)
				if len(ack) != 1 || ack[0] != original.ID || len(bounces) != 0 {
					t.Fatal("real receiver did not acknowledge original envelope")
				}
			}
			box, err := inbox.Open(target.InboxPath())
			mustRemote(t, err)
			if len(box.Pending()) != 1 {
				t.Fatal("real Receive duplicated or lost the message")
			}
			ev := box.Pending()[0]
			if ev.ID != "remote_"+original.From+"_"+original.ID || ev.Source != "bot:ada@a" || ev.Sender != "ada@a" || ev.SenderKind != inbox.KindBot || !ev.Untrusted || ev.Content != "hello [redacted]" || ev.Chain != "original-chain" || ev.Hops != 2 || !ev.Time.Equal(receivedAt) {
				t.Fatal("receiver event metadata, time, or stable identity changed")
			}
		})
	}
}

func TestPeerMessageRuntimePolicyStages(t *testing.T) {
	for _, mode := range []string{"exact_device", "unpaired", "off", "missing_xkey", "foreign_owner", "empty", "oversized", "hops_zero", "hops_four", "redacted_empty", "redacted_large", "identity", "bad_xkey", "rate"} {
		t.Run(mode, func(t *testing.T) {
			a, _, _, _, _, _ := pair(t)
			sender, err := bot.Create(a, "ada", "", nil)
			mustRemote(t, err)
			mustRemote(t, sender.SetOwners([]string{"u1"}))
			addr, text, hops := "bob@b", "hello", 1
			stage := "validation"
			switch mode {
			case "exact_device":
				addr = "bob@B"
			case "unpaired":
				mustRemote(t, update(a, func(c *Config) error { c.Devices[0].UnpairedAt = time.Now(); return nil }))
			case "off":
				mustRemote(t, update(a, func(c *Config) error { c.Devices[0].NoBotMessaging = true; return nil }))
			case "missing_xkey":
				mustRemote(t, update(a, func(c *Config) error { c.Devices[0].XKey = ""; return nil }))
			case "foreign_owner":
				mustRemote(t, sender.SetOwners([]string{"other"}))
			case "empty":
				text = " \n\t "
			case "oversized":
				text = strings.Repeat("x", peer.MaxText+1)
			case "hops_zero":
				hops = 0
			case "hops_four":
				hops = peer.MaxHops + 1
			case "redacted_empty":
				restore := bot.SetRedactText(func(string) string { return " " })
				defer restore()
			case "redacted_large":
				restore := bot.SetRedactText(func(string) string { return strings.Repeat("x", peer.MaxText+1) })
				defer restore()
			case "identity":
				stage = "identity"
				mustRemote(t, os.WriteFile(filepath.Join(a, "keys", "remote", "install.key"), []byte("invalid fixture key"), 0600))
			case "bad_xkey":
				stage = "envelope"
				mustRemote(t, update(a, func(c *Config) error { c.Devices[0].XKey = "malformed"; return nil }))
			case "rate":
				stage = "rate"
			}
			now, calls := time.Now(), 0
			if mode == "rate" {
				for range 8 {
					mustRemote(t, (&peer.Bus{Root: a}).Allow("remote-out:ada->bob@b", now))
				}
			}
			old := http.DefaultClient
			http.DefaultClient = &http.Client{Transport: pairTransport(func(*http.Request) (*http.Response, error) {
				calls++
				return nil, errors.New("policy rejection reached network")
			})}
			t.Cleanup(func() { http.DefaultClient = old })
			result, err := SendWithResult(context.Background(), a, "ada", addr, text, "chain", hops, now)
			if err == nil || result.Stage != stage || result.Gateway != "not_attempted" || result.GatewayStatus != 0 || result.Queued || calls != 0 || len(outboxItems(t, a)) != 0 || (result.EnvelopeID != "") != (mode == "rate") {
				t.Fatalf("stage=%s gateway=%s envelope=%v queued=%v calls=%d error=%v", result.Stage, result.Gateway, result.EnvelopeID != "", result.Queued, calls, err != nil)
			}
			if mode == "rate" && !errors.Is(err, peer.ErrRateLimit) {
				t.Fatal("original rate error cause lost")
			}
		})
	}
}

func TestPeerMessageRuntimeQueueCapacityAndLegacyAdapter(t *testing.T) {
	a, _, _, _, _, _ := pair(t)
	sender, err := bot.Create(a, "ada", "", nil)
	mustRemote(t, err)
	mustRemote(t, sender.SetOwners([]string{"u1"}))
	mustRemote(t, SetGateway(a, "http://gateway"))
	old := http.DefaultClient
	status := 503
	http.DefaultClient = &http.Client{Transport: pairTransport(func(*http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		w.WriteHeader(status)
		return w.Result(), nil
	})}
	t.Cleanup(func() { http.DefaultClient = old })
	now := time.Now()
	queued, err := Send(context.Background(), a, "ada", "bob@b", strings.Repeat("界", 2730)+"ab", "chain", peer.MaxHops, now)
	mustRemote(t, err)
	if !queued {
		t.Fatal("legacy wrapper lost queued success for exact 8 KiB UTF-8 body")
	}
	status = 400
	queued, err = Send(context.Background(), a, "ada", "bob@b", "rejected", "chain", 1, now)
	var httpErr *gateway.HTTPError
	if queued || !errors.As(err, &httpErr) || httpErr.StatusCode != 400 {
		t.Fatal("legacy wrapper lost permanent HTTP error cause")
	}
	status = 503
	dir := filepath.Join(a, "remote", "outbox")
	// One real item plus 255 records reaches the original 256-item limit.
	for i := range 255 {
		mustRemote(t, os.WriteFile(filepath.Join(dir, fmt.Sprintf("capacity-%03d.json", i)), []byte("{}"), 0600))
	}
	result, err := SendWithResult(context.Background(), a, "ada", "bob@b", "capacity", "chain", 1, now)
	if err == nil || err.Error() != "remote: outbox full" || result.Stage != "queue" || result.Gateway != "http_error" || result.GatewayStatus != 503 || result.EnvelopeID == "" || result.Queued || len(outboxItems(t, a)) != 256 {
		t.Fatal("queue capacity failure claimed durable success or lost gateway facts")
	}
}
