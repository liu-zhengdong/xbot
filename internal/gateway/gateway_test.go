package gateway

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestGateway(t *testing.T) {
	for _, name := range []string{"validation", "delivery", "quota", "expired quota", "unknown ack", "poll", "cap"} {
		t.Run(name, func(t *testing.T) {
			s := &Server{Dir: t.TempDir(), Token: "join"}
			h := s.Handler()
			hc := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				return w.Result(), nil
			})}
			ctx := context.Background()
			clients := make([]*Client, 2)
			for i := range clients {
				key, err := LoadOrCreateKey(filepath.Join(s.Dir, string(rune('a'+i))+".key"))
				if err != nil {
					t.Fatal(err)
				}
				clients[i] = &Client{Base: "http://gateway", Key: key, HTTP: hc}
			}
			a, b := clients[0], clients[1]
			bid := ID(b.Key.Public().(ed25519.PublicKey))
			check := func(err error, status string) {
				t.Helper()
				if (status == "" && err != nil) || (status != "" && (err == nil || !strings.Contains(err.Error(), status))) {
					t.Fatalf("want %q, got %v", status, err)
				}
			}
			check(a.Register(ctx, ""), "403")
			for _, c := range clients {
				check(c.Register(ctx, "join"), "")
			}
			envelope := func(c *Client, body string) Envelope { return NewEnvelope(c.Key, bid, "msg", []byte(body), time.Now()) }
			switch name {
			case "validation":
				w := httptest.NewRecorder()
				h.ServeHTTP(w, httptest.NewRequest("POST", "/v1/send", strings.NewReader("{}")))
				if w.Code != 401 {
					t.Fatalf("unsigned: %d", w.Code)
				}
				a.Now = func() time.Time { return time.Now().Add(-10 * time.Minute) }
				check(a.Register(ctx, "join"), "401")
				a.Now = nil
				check(a.Register(ctx, ""), "") // Registered keys need only their signature.
				e := envelope(a, "x")
				e.From = bid
				e.Sign(a.Key)
				check(a.Send(ctx, e), "400")
				check(a.Send(ctx, NewEnvelope(a.Key, strings.Repeat("0", 32), "msg", nil, time.Now())), "404")
			case "delivery":
				e := envelope(a, "hello")
				for i := 0; i < 2; i++ {
					check(a.Send(ctx, e), "")
				}
				got, err := b.Poll(ctx, 0)
				check(err, "")
				if len(got) != 1 || got[0].ID != e.ID {
					t.Fatalf("delivery: %v", got)
				}
				check(got[0].Verify(a.Key.Public().(ed25519.PublicKey)), "")
				check(b.Ack(ctx, []string{e.ID}), "")
				// A fresh Server must retain the ack's replay protection.
				h = (&Server{Dir: s.Dir, Token: "join"}).Handler()
				raw, _ := json.Marshal(e)
				req := httptest.NewRequest("POST", "/v1/send", strings.NewReader(string(raw)))
				SignRequest(req, a.Key, raw, time.Now())
				w := httptest.NewRecorder()
				h.ServeHTTP(w, req)
				if w.Code != 200 || !strings.Contains(w.Body.String(), "duplicate") {
					t.Fatalf("replay: %d %s", w.Code, w.Body.String())
				}
				got, err = b.Poll(ctx, 0)
				check(err, "")
				if len(got) != 0 {
					t.Fatal("ack replay was queued")
				}
			case "quota":
				l := s.bounds()
				l.pairCount = 1
				s.limits = &l
				check(a.Send(ctx, envelope(a, "one")), "")
				check(a.Send(ctx, envelope(a, "two")), "507")
				check(b.Send(ctx, envelope(b, "another sender")), "")
			case "expired quota":
				e := envelope(a, "one")
				e.To = e.From
				e.Sign(a.Key)
				raw, err := json.Marshal(e)
				check(err, "")
				l := s.bounds()
				l.totalBytes = int64(len(raw))
				s.limits = &l
				check(a.Send(ctx, e), "")
				old := time.Now().Add(-TTL - time.Second)
				check(os.Chtimes(filepath.Join(s.Dir, "queue", e.To, e.From, e.ID+".json"), old, old), "")
				e.To = bid
				e.Sign(a.Key)
				check(a.Send(ctx, e), "")
			case "unknown ack":
				check(b.Ack(ctx, []string{envelope(a, "unqueued").ID}), "")
				files, err := filepath.Glob(filepath.Join(s.Dir, "acked", bid, "*"))
				check(err, "")
				if len(files) != 0 {
					t.Fatalf("unknown ack created files: %v", files)
				}
			case "poll":
				result := make(chan int, 2)
				start := func() {
					go func() {
						envs, err := b.Poll(ctx, 5*time.Second)
						if err != nil {
							result <- -1
						} else {
							result <- len(envs)
						}
					}()
				}
				await := func(want int) {
					t.Helper()
					select {
					case n := <-result:
						if n != want {
							t.Fatalf("poll: %d, want %d", n, want)
						}
					case <-time.After(time.Second):
						t.Fatal("poll did not return promptly")
					}
				}
				start()
				deadline := time.Now().Add(time.Second)
				for {
					s.mu.Lock()
					waiting := s.waiters[bid] != nil
					s.mu.Unlock()
					if waiting {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("first poll did not start")
					}
					time.Sleep(time.Millisecond)
				}
				start()
				await(0)
				check(a.Send(ctx, envelope(a, "wake")), "")
				await(1)
			case "cap":
				l := s.bounds()
				l.pollBytes = 2000
				s.limits = &l
				for i := 0; i < 2; i++ {
					check(a.Send(ctx, envelope(a, strings.Repeat("x", 1000))), "")
				}
				got, err := b.Poll(ctx, 0)
				check(err, "")
				raw, _ := json.Marshal(map[string]any{"envelopes": got})
				if len(got) != 1 || len(raw)+1 > s.limits.pollBytes {
					t.Fatalf("poll cap: %d envelopes, %d bytes", len(got), len(raw)+1)
				}
				s.limits.pollBytes = 1
				got, err = b.Poll(ctx, 0)
				check(err, "")
				if len(got) != 1 {
					t.Fatal("poll must return at least one")
				}
			}
		})
	}
}

func TestRegistrationEvictsIdleInstall(t *testing.T) {
	s := &Server{Dir: t.TempDir(), Open: true, DailyPerIP: -1} // One test IP registers every install.
	h := s.Handler()
	hc := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Result(), nil
	})}
	ctx := context.Background()
	clients := make([]*Client, MaxInstalls+1)
	for i := range clients {
		_, key, err := ed25519.GenerateKey(nil)
		if err != nil {
			t.Fatal(err)
		}
		clients[i] = &Client{Base: "http://gateway", Key: key, HTTP: hc}
		if i < MaxInstalls {
			if err := clients[i].Register(ctx, ""); err != nil {
				t.Fatal(err)
			}
		}
	}
	path := func(i int) string {
		return filepath.Join(s.Dir, "installs", ID(clients[i].Key.Public().(ed25519.PublicKey)))
	}
	age := func(i int, days int) {
		t.Helper()
		old := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
		if err := os.Chtimes(path(i), old, old); err != nil {
			t.Fatal(err)
		}
	}
	// The oldest installation has pending mail and must survive eviction.
	to := ID(clients[0].Key.Public().(ed25519.PublicKey))
	if err := clients[1].Send(ctx, NewEnvelope(clients[1].Key, to, "msg", []byte("pending"), time.Now())); err != nil {
		t.Fatal(err)
	}
	age(0, 33)
	age(1, 32)
	if _, err := clients[1].Poll(ctx, 0); err != nil {
		t.Fatal(err)
	}
	if err := clients[MaxInstalls].Register(ctx, ""); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("full active/protected installs: %v", err)
	}
	age(2, 31)
	if err := clients[MaxInstalls].Register(ctx, ""); err != nil {
		t.Fatalf("register after idle eviction: %v", err)
	}
	if info, err := os.Stat(path(1)); err != nil || time.Since(info.ModTime()) > time.Minute {
		t.Fatalf("verified request did not refresh last seen: %v", err)
	}
	if _, err := os.Stat(path(2)); !os.IsNotExist(err) {
		t.Fatalf("idle install was not evicted: %v", err)
	}
	if _, err := os.Stat(path(0)); err != nil {
		t.Fatalf("install with queued mail was evicted: %v", err)
	}
}

func TestGatewaySenderByteQuota(t *testing.T) {
	s := &Server{Dir: t.TempDir(), Open: true}
	h := s.Handler()
	hc := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Result(), nil
	})}
	ctx := context.Background()
	clients := make([]*Client, 3)
	for i := range clients {
		_, key, err := ed25519.GenerateKey(nil)
		if err != nil {
			t.Fatal(err)
		}
		clients[i] = &Client{Base: "http://gateway", Key: key, HTTP: hc}
		if err := clients[i].Register(ctx, ""); err != nil {
			t.Fatal(err)
		}
	}
	stamp := time.Now() // Keep RFC3339Nano width identical for byte-exact quota fixtures.
	envelope := func(from, to int) Envelope {
		return NewEnvelope(clients[from].Key, ID(clients[to].Key.Public().(ed25519.PublicKey)), "msg", []byte("mail"), stamp)
	}
	first := envelope(0, 0)
	raw, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	l := s.bounds()
	l.senderBytes = 2 * int64(len(raw))
	l.totalBytes = 3 * int64(len(raw))
	s.limits = &l
	for _, e := range []Envelope{first, envelope(0, 1)} {
		if err := clients[0].Send(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	// Rebuild accounting from disk before checking the cross-recipient cap.
	s = &Server{Dir: s.Dir, Open: true, limits: &l}
	h = s.Handler()
	err = clients[0].Send(ctx, envelope(0, 2))
	if err == nil || !strings.Contains(err.Error(), "507") {
		t.Errorf("sender quota: want 507, got %v", err)
	}
	if err := clients[1].Send(ctx, envelope(1, 2)); err != nil {
		t.Fatalf("another sender must retain queue capacity: %v", err)
	}
	if err := clients[0].Ack(ctx, []string{first.ID}); err != nil {
		t.Fatal(err)
	}
	replacement := envelope(0, 0)
	if err := clients[0].Send(ctx, replacement); err != nil {
		t.Fatalf("ack must release sender quota: %v", err)
	}
	old := time.Now().Add(-TTL - time.Second)
	path := filepath.Join(s.Dir, "queue", replacement.To, replacement.From, replacement.ID+".json")
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if err := clients[0].Send(ctx, envelope(0, 2)); err != nil {
		t.Fatalf("expiry must release sender quota across recipients: %v", err)
	}
}
