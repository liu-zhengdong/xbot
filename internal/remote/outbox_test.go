package remote

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
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

func TestOutboxRetry(t *testing.T) {
	for _, status := range []int{503, 507, 429, 408, 400} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			a, _, _, ka, kb, _ := pair(t)
			sender, err := bot.Create(a, "ada", "", nil)
			mustRemote(t, err)
			mustRemote(t, sender.SetOwners([]string{"u1"}))
			mustRemote(t, SetGateway(a, "http://gateway"))
			handler := (&gateway.Server{Dir: t.TempDir(), Open: true}).Handler()
			old := http.DefaultClient
			fail, calls := true, 0
			http.DefaultClient = &http.Client{Transport: pairTransport(func(r *http.Request) (*http.Response, error) {
				w := httptest.NewRecorder()
				if r.URL.Path == "/v1/send" {
					calls++
				}
				if fail && r.URL.Path == "/v1/send" {
					w.WriteHeader(status)
				} else {
					handler.ServeHTTP(w, r)
				}
				return w.Result(), nil
			})}
			t.Cleanup(func() { http.DefaultClient = old })
			ctx, now := context.Background(), time.Now()
			ca, cb := &gateway.Client{Base: "http://gateway", Key: ka}, &gateway.Client{Base: "http://gateway", Key: kb}
			mustRemote(t, ca.Register(ctx, ""))
			mustRemote(t, cb.Register(ctx, ""))
			restore := bot.SetRedactText(func(s string) string { return strings.ReplaceAll(s, "secret-value", "[secret]") })
			defer restore()
			started := time.Now()
			queued, err := Send(ctx, a, "ada", "bob@b", "hello secret-value", "chain", 1, now)
			t.Logf("send_ms=%.3f", float64(time.Since(started).Microseconds())/1000)
			paths, _ := filepath.Glob(filepath.Join(a, "remote", "outbox", "*.json"))
			if status == 400 {
				if err == nil || queued || len(paths) != 0 {
					t.Fatal("400 queued or swallowed")
				}
				return
			}
			mustRemote(t, err)
			if !queued || len(paths) != 1 {
				t.Fatal("message not queued")
			}
			info, err := os.Stat(paths[0])
			mustRemote(t, err)
			if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
				t.Fatal("outbox permissions")
			}
			var item outboxItem
			mustRemote(t, readJSON(paths[0], &item))
			original := item.Env
			if item.Snippet != "hello [secret]" {
				t.Fatal("snippet not redacted")
			}
			mustRemote(t, flushOutbox(ctx, a, ca, now))
			if calls != 1 {
				t.Fatal("retried before due")
			}
			mustRemote(t, flushOutbox(ctx, a, ca, now.Add(5*time.Second)))
			mustRemote(t, readJSON(paths[0], &item))
			if item.Attempts != 2 || !item.Next.Equal(now.Add(15*time.Second)) {
				t.Fatal("wrong backoff")
			}
			fail = false
			// Also cover an accepted envelope whose response was lost.
			if status != 503 {
				mustRemote(t, ca.Send(ctx, original))
			}
			mustRemote(t, flushOutbox(ctx, a, ca, item.Next))
			if _, err := os.Stat(paths[0]); !os.IsNotExist(err) {
				t.Fatal("delivered item retained")
			}
			envs, err := cb.Poll(ctx, 0)
			mustRemote(t, err)
			if len(envs) != 1 || !reflect.DeepEqual(envs[0], original) {
				t.Fatal("retry changed envelope or lost delivery")
			}
			if status != 503 {
				return
			}
			fail = true
			for _, size := range []int64{0, 8 << 20} {
				count := 256
				if size > 0 {
					count = 1
				}
				for i := range count {
					path := filepath.Join(a, "remote", "outbox", fmt.Sprintf("cap-%d.json", i))
					f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0600)
					mustRemote(t, err)
					mustRemote(t, f.Truncate(size))
					mustRemote(t, f.Close())
				}
				queued, err := Send(ctx, a, "ada", "bob@b", "hello", "chain", 1, now)
				if queued || err == nil || err.Error() != "remote: outbox full" {
					t.Fatal("outbox exceeded cap")
				}
				for i := range count {
					mustRemote(t, os.Remove(filepath.Join(a, "remote", "outbox", fmt.Sprintf("cap-%d.json", i))))
				}
			}
		})
	}
}

func TestOutboxUndelivered(t *testing.T) {
	for _, mode := range []string{"expired", "rejected", "bounce expired"} {
		t.Run(mode, func(t *testing.T) {
			a, _, _, key, _, self := pair(t)
			sender, err := bot.Create(a, "ada", "", nil)
			mustRemote(t, err)
			now := time.Now()
			env := gateway.NewEnvelope(key, self, "box", nil, now)
			if mode == "bounce expired" {
				env.Kind = "bounce"
			}
			item := outboxItem{Env: env, FromBot: "ada", To: "bob@b", Snippet: "hello", Created: now.Add(-25 * time.Hour), Next: now.Add(time.Hour)}
			if mode == "rejected" {
				item.Created, item.Next = now, now
			}
			mustRemote(t, enqueue(a, item))
			client := &gateway.Client{Base: "http://gateway", Key: key, HTTP: &http.Client{Transport: pairTransport(func(r *http.Request) (*http.Response, error) {
				if mode != "rejected" {
					t.Fatal("expired item sent")
				}
				w := httptest.NewRecorder()
				w.WriteHeader(400)
				return w.Result(), nil
			})}}
			mustRemote(t, flushOutbox(context.Background(), a, client, now))
			paths, _ := filepath.Glob(filepath.Join(a, "remote", "outbox", "*.json"))
			if len(paths) != 0 {
				t.Fatal("undelivered item retained")
			}
			box, err := inbox.Open(sender.InboxPath())
			mustRemote(t, err)
			if mode == "bounce expired" {
				if len(box.Pending()) != 0 {
					t.Fatal("bounce notified")
				}
				return
			}
			if len(box.Pending()) != 1 {
				t.Fatal("missing notification")
			}
			ev := box.Pending()[0]
			if ev.ID != "remote_undelivered_"+env.ID || ev.Source != "remote" || !ev.Untrusted || !strings.HasSuffix(ev.Content, ": hello") {
				t.Fatal("unsafe notification")
			}
			if mode == "expired" && !strings.Contains(ev.Content, "after 24h") {
				t.Fatal("wrong expiry reason")
			}
		})
	}
}

func TestReceiveBounce(t *testing.T) {
	for _, mode := range []string{"missing", "wrong owner", "rate limited", "bad signature"} {
		t.Run(mode, func(t *testing.T) {
			a, b, target, ka, kb, self := pair(t)
			sender, err := bot.Create(a, "ada", "", nil)
			mustRemote(t, err)
			mustRemote(t, sender.SetOwners([]string{"u1"}))
			to := "missing"
			if mode == "wrong owner" {
				to = "bob"
				mustRemote(t, target.SetOwners([]string{"other"}))
			}
			cfg, err := Load(a)
			mustRemote(t, err)
			now := time.Now()
			body, _ := json.Marshal(message{"ada", to, "hello", "chain", 1})
			env, err := newBoxEnvelope(ka, cfg.Devices[0], "box", body, now)
			mustRemote(t, err)
			if mode == "bad signature" {
				env.Sig[0] ^= 1
			}
			if mode == "rate limited" {
				for range 8 {
					mustRemote(t, (&peer.Bus{Root: b}).Allow("remote-bounce:"+env.From, now))
				}
			}
			ack, bounces, err := Receive(b, self, []gateway.Envelope{env}, now)
			mustRemote(t, err)
			if len(ack) != 1 {
				t.Fatal("missing ack")
			}
			if mode != "missing" && mode != "wrong owner" {
				if len(bounces) != 0 {
					t.Fatal("rejected envelope bounced")
				}
				return
			}
			if len(bounces) != 1 || bounces[0].Kind != "bounce" || bounces[0].Verify(kb.Public().(ed25519.PublicKey)) != nil {
				t.Fatal("missing signed bounce")
			}
			_, replies, err := Receive(a, env.From, bounces, now)
			mustRemote(t, err)
			if len(replies) != 0 {
				t.Fatal("bounce of bounce")
			}
			_, replay, err := Receive(b, self, []gateway.Envelope{env}, now)
			mustRemote(t, err)
			if len(replay) != 0 {
				t.Fatal("replay bounced")
			}
			box, err := inbox.Open(sender.InboxPath())
			mustRemote(t, err)
			if len(box.Pending()) != 1 {
				t.Fatal("bounce not delivered")
			}
			ev := box.Pending()[0]
			if ev.ID != "remote_bounce_"+bounces[0].ID || !ev.Untrusted || ev.Source != "remote" || ev.Content != "message to "+to+"@b was not delivered: no such bot" {
				t.Fatal("wrong bounce event")
			}
		})
	}
}

func TestOutboxRetryableAndCorrupt(t *testing.T) {
	if !retryable(errors.New("stream reset while reading body")) {
		t.Fatal("non-HTTP client error must be retryable")
	}
	for code, want := range map[int]bool{400: false, 403: false, 408: true, 429: true, 503: true, 507: true} {
		if retryable(&gateway.HTTPError{StatusCode: code}) != want {
			t.Fatalf("status %d retryable != %v", code, want)
		}
	}
	root := t.TempDir()
	dir := filepath.Join(root, "remote", "outbox")
	mustRemote(t, os.MkdirAll(dir, 0700))
	mustRemote(t, os.WriteFile(filepath.Join(dir, "bad.json"), []byte("{"), 0600))
	mustRemote(t, flushOutbox(context.Background(), root, &gateway.Client{Base: "http://gateway"}, time.Now()))
	if _, err := os.Stat(filepath.Join(dir, "bad.json")); !os.IsNotExist(err) {
		t.Fatal("corrupt outbox record kept")
	}
}

// A bounce-shaped plaintext sealed as kind box must not open as a bounce, and
// a real bounce must not open as box: the kind is bound into the AAD.
func TestBoxKindBound(t *testing.T) {
	for _, relabel := range []bool{false, true} {
		a, b, target, ka, _, self := pair(t)
		cfg, err := Load(a)
		mustRemote(t, err)
		now := time.Now()
		body, _ := json.Marshal(bounce{"ref1", "bob", "carl", "no such bot"})
		kind := "bounce"
		if relabel {
			kind = "box"
		}
		env, err := newBoxEnvelope(ka, cfg.Devices[0], kind, body, now)
		mustRemote(t, err)
		if relabel {
			env.Kind = "bounce"
			env.Sign(ka)
		} else if got := string(boxAAD(gateway.Envelope{ID: "i", From: "f", To: "t", Kind: "box"})); got != "xbot-box-v1|i|f|t" {
			t.Fatalf("kind box AAD changed from the G4 form: %q", got)
		}
		_, _, err = Receive(b, self, []gateway.Envelope{env}, now)
		mustRemote(t, err)
		box, err := inbox.Open(target.InboxPath())
		mustRemote(t, err)
		if got := len(box.Pending()); got != map[bool]int{false: 1, true: 0}[relabel] {
			t.Fatalf("relabel=%v delivered %d events", relabel, got)
		}
	}
}

func TestOutboxStopsSendingWhenDown(t *testing.T) {
	root := t.TempDir()
	calls := 0
	old := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: pairTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		w := httptest.NewRecorder()
		w.WriteHeader(503)
		return w.Result(), nil
	})}
	t.Cleanup(func() { http.DefaultClient = old })
	now := time.Now()
	for _, id := range []string{"a1", "a2", "a3"} {
		mustRemote(t, enqueue(root, outboxItem{Env: gateway.Envelope{V: 1, ID: id, Kind: "box"}, Created: now, Next: now}))
	}
	_, key, _ := ed25519.GenerateKey(nil)
	mustRemote(t, flushOutbox(context.Background(), root, &gateway.Client{Base: "http://gateway", Key: key}, now))
	paths, _ := filepath.Glob(filepath.Join(root, "remote", "outbox", "*.json"))
	if calls != 1 || len(paths) != 3 {
		t.Fatalf("calls=%d kept=%d; want one attempt and all kept", calls, len(paths))
	}
}

// A line queued while the gateway was unreachable must stop showing as
// queued_local once the retry lands. The line id travels with the envelope.
func TestOutboxRetrySettlesChatLine(t *testing.T) {
	a, _, _, _ := chatPair(t)
	target := deviceOf(t, a, "b")
	now := time.Now()
	id := "0011223344556677"
	if err := appendChat(a, target.ID, "bob", ChatMessage{ID: id, Role: "me", Text: "在家吗", At: now.UTC(), State: "queued_local"}); err != nil {
		t.Fatal(err)
	}
	item := outboxItem{
		Env:     gateway.Envelope{ID: "env-settle", Kind: "chat", To: "bob@" + target.Name},
		To:      "bob@" + target.Name,
		Created: now,
		Next:    now.Add(-time.Second),
		ChatRef: id,
		ChatDev: target.ID,
		ChatBot: "bob",
	}
	if err := enqueue(a, item); err != nil {
		t.Fatal(err)
	}
	// The gateway is unreachable, so the retry fails and the queue gives up.
	client := &gateway.Client{Base: "http://127.0.0.1:1"}
	_ = flushOutbox(context.Background(), a, client, now.Add(25*time.Hour))
	lines, err := readChat(a, target.ID, "bob")
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 {
		t.Fatalf("chat lines %d", len(lines))
	}
	if lines[0].State == "queued_local" {
		t.Fatal("a line the queue gave up on still shows as queued")
	}
}
