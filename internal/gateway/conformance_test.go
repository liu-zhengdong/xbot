package gateway

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var conformanceKeyMu sync.Mutex
var conformanceKeyCounts = make(map[string]int)

func TestConformanceGo(t *testing.T) {
	s := httptest.NewServer((&Server{Dir: t.TempDir(), Open: true}).Handler())
	defer s.Close()
	runConformance(t, s.URL)
}
func TestConformanceURL(t *testing.T) {
	base := os.Getenv("XBOT_GATEWAY_URL")
	if base == "" {
		t.Skip("XBOT_GATEWAY_URL is unset")
	}
	runConformance(t, base)
}
func conformanceRequire(t *testing.T, ok bool, message any) {
	if !ok {
		t.Fatal(message)
	}
}
func runConformance(t *testing.T, base string) {
	started := time.Now()
	var requests atomic.Int64
	t.Cleanup(func() { t.Logf("HTTP requests: %d; elapsed: %s", requests.Load(), time.Since(started)) })
	hc := &http.Client{Timeout: 60 * time.Second}
	type response struct {
		status  int
		body    []byte
		err     error
		retried bool // a transport error was retried, so the first attempt may have landed
	}
	var exchange func(method, path string, body, signedBody []byte, key ed25519.PrivateKey, now time.Time) response
	once := func(method, path string, body, signedBody []byte, key ed25519.PrivateKey, now time.Time) response {
		req, err := http.NewRequest(method, strings.TrimRight(base, "/")+path, bytes.NewReader(body))
		if err != nil {
			return response{err: err}
		}
		if key != nil {
			SignRequest(req, key, signedBody, now)
		}
		requests.Add(1)
		res, err := hc.Do(req)
		if err != nil {
			return response{err: err}
		}
		defer res.Body.Close()
		b, err := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
		if err == nil && len(b) > 1<<20 {
			err = fmt.Errorf("response exceeds 1 MiB")
		}
		if strings.HasPrefix(path, "/v1/") && res.StatusCode != 404 && !strings.HasPrefix(res.Header.Get("Content-Type"), "application/json") {
			err = fmt.Errorf("Content-Type: %q", res.Header.Get("Content-Type"))
		}
		return response{res.StatusCode, b, err, false}
	}
	// Remote runs cross real networks that drop a fraction of a percent of connections (seen
	// even against unrelated hosts). Retry those once or twice; HTTP replies are never retried.
	exchange = func(method, path string, body, signedBody []byte, key ed25519.PrivateKey, now time.Time) response {
		r := once(method, path, body, signedBody, key, now)
		for i := 0; i < 2 && r.status == 0 && r.err != nil && base != "" && !strings.HasPrefix(base, "http://127.0.0.1"); i++ {
			t.Logf("retrying %s %s after transport error: %v", method, path, r.err)
			r = once(method, path, body, signedBody, key, now)
			r.retried = true
		}
		return r
	}
	raw := func(t *testing.T, method, path string, body []byte, key ed25519.PrivateKey, now time.Time) (int, []byte) {
		r := exchange(method, path, body, body, key, now)
		conformanceRequire(t, r.err == nil, r.err)
		return r.status, r.body
	}
	marshal := func(t *testing.T, v any) []byte {
		b, err := json.Marshal(v)
		conformanceRequire(t, err == nil, err)
		return b
	}
	check := func(t *testing.T, status, want int, body []byte, shape string) {
		conformanceRequire(t, status == want, fmt.Sprintf("status %d, want %d: %.200s", status, want, body))
		var got, expected any
		if shape != "" {
			conformanceRequire(t, !(json.Unmarshal(body, &got) != nil || json.Unmarshal([]byte(shape), &expected) != nil || !reflect.DeepEqual(got, expected)), fmt.Sprintf("body %.200s, want %s", body, shape))
		} else if status >= 400 {
			var e map[string]any
			conformanceRequire(t, !(json.Unmarshal(body, &e) != nil), fmt.Sprintf("invalid error JSON: %.200s", body))
			msg, _ := e["error"].(string)
			conformanceRequire(t, msg != "", fmt.Sprintf("missing error: %.200s", body))
		}
	}
	call := func(t *testing.T, method, path string, body []byte, key ed25519.PrivateKey, want int, shape string) []byte {
		r := exchange(method, path, body, body, key, time.Now())
		conformanceRequire(t, r.err == nil, r.err)
		if r.retried && r.status == 200 && (want == 202 && path == "/v1/send" || path == "/v1/ack") {
			return r.body // The dropped attempt may have queued or acked already; the retry then sees a duplicate or fewer acks.
		}
		check(t, r.status, want, r.body, shape)
		return r.body
	}
	poll := func(t *testing.T, key ed25519.PrivateKey) ([]Envelope, []byte) {
		b := call(t, "GET", "/v1/poll?wait=0", nil, key, 200, "")
		var out struct {
			Envelopes []Envelope `json:"envelopes"`
		}
		var fields map[string]json.RawMessage
		conformanceRequire(t, !(json.Unmarshal(b, &out) != nil || out.Envelopes == nil || json.Unmarshal(b, &fields) != nil || len(fields) != 1), fmt.Sprintf("bad poll: %.200s", b))
		return out.Envelopes, b
	}
	ack := func(t *testing.T, key ed25519.PrivateKey, ids []string, n int) {
		call(t, "POST", "/v1/ack", marshal(t, map[string]any{"ids": ids}), key, 200, fmt.Sprintf(`{"acked":%d}`, n))
	}
	drain := func(t *testing.T, key ed25519.PrivateKey) {
		for i := 0; i < 5; i++ {
			envs, _ := poll(t, key)
			if len(envs) == 0 {
				return
			}
			ids := make([]string, len(envs))
			for j, e := range envs {
				ids[j] = e.ID
			}
			ack(t, key, ids, len(ids))
		}
		t.Error("queue did not drain within five rounds")
	}
	fresh := func(t *testing.T, register bool) ed25519.PrivateKey {
		var key ed25519.PrivateKey
		if seed, set := os.LookupEnv("XBOT_GATEWAY_TEST_SEED"); set {
			conformanceKeyMu.Lock()
			conformanceKeyCounts[t.Name()]++
			n := conformanceKeyCounts[t.Name()]
			conformanceKeyMu.Unlock()
			sum := sha256.Sum256([]byte(fmt.Sprintf("xbot-conformance-v1|%s|%s|%d", seed, t.Name(), n)))
			key = ed25519.NewKeyFromSeed(sum[:])
		} else {
			var err error
			_, key, err = ed25519.GenerateKey(nil)
			conformanceRequire(t, err == nil, err)
		}
		if register {
			call(t, "POST", "/v1/register", nil, key, 200, `{"status":"ok"}`)
			drain(t, key)
			t.Cleanup(func() { drain(t, key) })
		}
		return key
	}
	pair := func(t *testing.T) (ed25519.PrivateKey, ed25519.PrivateKey) { return fresh(t, true), fresh(t, true) }
	env := func(a, b ed25519.PrivateKey, body []byte) Envelope {
		return NewEnvelope(a, ID(b.Public().(ed25519.PublicKey)), "msg", body, time.Now())
	}
	send := func(t *testing.T, a ed25519.PrivateKey, e Envelope, status int, state string) {
		call(t, "POST", "/v1/send", marshal(t, e), a, status, `{"status":"`+state+`"}`)
	}
	equal := func(t *testing.T, got, want Envelope, key ed25519.PrivateKey) {
		conformanceRequire(t, got.Time.Equal(want.Time), "envelope time changed")
		got.Time = want.Time
		conformanceRequire(t, !(!reflect.DeepEqual(got, want) || got.Verify(key.Public().(ed25519.PublicKey)) != nil), "envelope fields or signature changed")
	}
	t.Run("health", func(t *testing.T) {
		status, b := raw(t, "GET", "/healthz", nil, nil, time.Now())
		conformanceRequire(t, !(status != 200 || string(b) != "ok"), fmt.Sprintf("health: %d %q", status, b))
	})
	t.Run("auth", func(t *testing.T) {
		key := fresh(t, false)
		for _, d := range []time.Duration{0, -10 * time.Minute, 10 * time.Minute} {
			k := key
			if d == 0 {
				k = nil
			}
			s, b := raw(t, "POST", "/v1/register", nil, k, time.Now().Add(d))
			check(t, s, 401, b, "")
		}
		r := exchange("POST", "/v1/register", []byte(`{}`), []byte(`{"different":true}`), key, time.Now())
		conformanceRequire(t, r.err == nil, r.err)
		check(t, r.status, 401, r.body, "")
	})
	t.Run("body limit", func(t *testing.T) { call(t, "POST", "/v1/send", bytes.Repeat([]byte("x"), MaxBody+1), nil, 413, "") })
	t.Run("unregistered", func(t *testing.T) {
		key := fresh(t, false)
		for _, path := range []string{"/v1/poll?wait=0", "/v1/send", "/v1/ack"} {
			method := "POST"
			if strings.Contains(path, "poll") {
				method = "GET"
			}
			call(t, method, path, nil, key, 403, "")
		}
	})
	t.Run("register", func(t *testing.T) {
		key := fresh(t, true)
		call(t, "POST", "/v1/register", nil, key, 200, `{"status":"ok"}`)
	})
	t.Run("send validation", func(t *testing.T) {
		a, b := pair(t)
		for _, mutate := range []func(*Envelope){
			func(e *Envelope) { e.From = e.To }, func(e *Envelope) { e.V = 2 },
			func(e *Envelope) { e.Kind = "Msg" }, func(e *Envelope) { e.Kind = strings.Repeat("a", 17) },
			func(e *Envelope) { e.ID = "ABCDEF0123456789ABCDEF0123456789" }, func(e *Envelope) { e.ID = "bad" },
		} {
			e := env(a, b, []byte("test"))
			mutate(&e)
			e.Sign(a)
			call(t, "POST", "/v1/send", marshal(t, e), a, 400, "")
		}
		e := env(a, b, nil)
		e.Sig[0] ^= 1
		call(t, "POST", "/v1/send", marshal(t, e), a, 400, "")
		call(t, "POST", "/v1/send", []byte("not JSON"), a, 400, "")
		e = NewEnvelope(a, strings.Repeat("0", 32), "msg", nil, time.Now())
		call(t, "POST", "/v1/send", marshal(t, e), a, 404, "")
	})
	t.Run("time canonicalization", func(t *testing.T) {
		a, b := pair(t)
		for _, ns := range []time.Duration{123456789, 120000000} {
			e := env(a, b, []byte("time"))
			e.Time = time.Now().Truncate(time.Second).Add(ns).In(time.FixedZone("", 8*3600))
			e.Sign(a)
			send(t, a, e, 202, "queued")
			got, _ := poll(t, b)
			conformanceRequire(t, len(got) == 1, fmt.Sprintf("got %d envelopes", len(got)))
			equal(t, got[0], e, a)
			ack(t, b, []string{e.ID}, 1)
		}
	})
	t.Run("delivery and duplicates", func(t *testing.T) {
		a, b := pair(t)
		e := env(a, b, []byte("hello"))
		send(t, a, e, 202, "queued")
		send(t, a, e, 200, "duplicate")
		got, _ := poll(t, b)
		conformanceRequire(t, len(got) == 1, fmt.Sprintf("got %d envelopes", len(got)))
		equal(t, got[0], e, a)
		ack(t, b, []string{e.ID}, 1)
		ack(t, b, []string{e.ID}, 0)
		send(t, a, e, 200, "duplicate")
		_, body := poll(t, b)
		check(t, 200, 200, body, `{"envelopes":[]}`)
	})
	t.Run("order", func(t *testing.T) {
		a, b := pair(t)
		want := make([]Envelope, 3)
		for i := range want {
			want[i] = env(a, b, []byte("ordered"))
			send(t, a, want[i], 202, "queued")
			time.Sleep(20 * time.Millisecond) // Go orders by file mtime, which is jiffy-coarse on Linux.
		}
		got, _ := poll(t, b)
		conformanceRequire(t, len(got) == 3, fmt.Sprintf("got %d envelopes", len(got)))
		for i := range want {
			equal(t, got[i], want[i], a)
		}
	})
	t.Run("ack validation", func(t *testing.T) {
		a, b := pair(t)
		for _, body := range [][]byte{[]byte(`{"ids":["not-hex"]}`), marshal(t, map[string]any{"ids": strings.Fields(strings.Repeat(strings.Repeat("0", 32)+" ", 65))}), []byte("not JSON")} {
			call(t, "POST", "/v1/ack", body, b, 400, "")
		}
		e := env(a, b, []byte("ack validation")) // A random id: a fixed one would hit the tombstone left by the last run.
		id := e.ID
		ack(t, b, []string{id}, 0)
		send(t, a, e, 202, "queued")
		call(t, "POST", "/v1/ack", marshal(t, map[string]any{"ids": []string{id, "not-hex"}}), b, 400, "")
		got, _ := poll(t, b)
		conformanceRequire(t, len(got) == 1, "invalid ack removed envelope")
		equal(t, got[0], e, a)
		ack(t, fresh(t, true), []string{id}, 0)
		got, _ = poll(t, b)
		conformanceRequire(t, len(got) == 1, "other install ack removed envelope")
		equal(t, got[0], e, a)
	})
	startPoll := func(key ed25519.PrivateKey, wait int) <-chan response {
		ch := make(chan response, 1)
		go func() { ch <- exchange("GET", fmt.Sprintf("/v1/poll?wait=%d", wait), nil, nil, key, time.Now()) }()
		return ch
	}
	await := func(t *testing.T, ch <-chan response, timeout time.Duration) []byte {
		select {
		case r := <-ch:
			conformanceRequire(t, r.err == nil, r.err)
			check(t, r.status, 200, r.body, "")
			return r.body
		case <-time.After(timeout):
			t.Fatal("long poll did not return in time")
			return nil
		}
	}
	received := func(t *testing.T, body []byte, e Envelope, a ed25519.PrivateKey) {
		var out struct {
			Envelopes []Envelope `json:"envelopes"`
		}
		conformanceRequire(t, json.Unmarshal(body, &out) == nil && len(out.Envelopes) == 1, fmt.Sprintf("bad delivery: %.200s", body))
		equal(t, out.Envelopes[0], e, a)
	}
	t.Run("long poll wakes", func(t *testing.T) {
		a, b := pair(t)
		start := time.Now()
		ch := startPoll(b, 10)
		time.Sleep(300 * time.Millisecond)
		e := env(a, b, []byte("wake"))
		send(t, a, e, 202, "queued")
		body := await(t, ch, 5*time.Second-time.Since(start))
		received(t, body, e, a)
	})
	t.Run("long poll replaced", func(t *testing.T) {
		for attempt := 0; attempt < 3; attempt++ {
			a, b := pair(t)
			ch := startPoll(b, 10)
			time.Sleep(300 * time.Millisecond)
			second := startPoll(b, 5)
			select {
			case r := <-ch:
				conformanceRequire(t, r.err == nil, r.err)
				check(t, r.status, 200, r.body, `{"envelopes":[]}`)
			case <-time.After(3 * time.Second):
				await(t, second, 12*time.Second)
				await(t, ch, 12*time.Second)
				continue
			}
			select {
			case r := <-second:
				t.Fatalf("second poll returned before send: status=%d body=%.200s err=%v", r.status, r.body, r.err)
			default:
			}
			e := env(a, b, []byte("replacement"))
			send(t, a, e, 202, "queued")
			received(t, await(t, second, 5*time.Second), e, a)
			return
		}
		t.Fatal("first poll was not replaced within 3s in three attempts")
	})
	t.Run("wait clamp/timeout", func(t *testing.T) {
		b := fresh(t, true)
		start := time.Now()
		call(t, "GET", "/v1/poll?wait=1", nil, b, 200, `{"envelopes":[]}`)
		d := time.Since(start)
		conformanceRequire(t, d >= 500*time.Millisecond && d <= 5*time.Second, fmt.Sprintf("wait took %s", d))
		call(t, "GET", "/v1/poll?wait=-1", nil, b, 200, `{"envelopes":[]}`)
	})
	t.Run("pair count limit", func(t *testing.T) {
		a, b := pair(t)
		first := env(a, b, []byte("small"))
		send(t, a, first, 202, "queued")
		for i := 1; i < 64; i++ {
			send(t, a, env(a, b, []byte("small")), 202, "queued")
		}
		call(t, "POST", "/v1/send", marshal(t, env(a, b, nil)), a, 507, "")
		send(t, a, first, 200, "duplicate")
		got, _ := poll(t, b)
		conformanceRequire(t, len(got) == MaxPoll, fmt.Sprintf("poll count %d, want %d", len(got), MaxPoll))
		ack(t, b, []string{got[0].ID}, 1)
		send(t, a, env(a, b, nil), 202, "queued")
	})
	t.Run("pair bytes limit and poll byte limit", func(t *testing.T) {
		a, b := pair(t)
		count, total := 0, 0
		full := false
		for i := 0; i < 30; i++ {
			data := marshal(t, env(a, b, bytes.Repeat([]byte("x"), 45<<10)))
			conformanceRequire(t, !(len(data) > MaxBody), "test request exceeds MaxBody")
			status, body := raw(t, "POST", "/v1/send", data, a, time.Now())
			if status == 507 {
				check(t, status, 507, body, "")
				full = true
				break
			}
			check(t, status, 202, body, `{"status":"queued"}`)
			count++
			total += len(data)
		}
		conformanceRequire(t, !(!full || total > 1<<20), fmt.Sprintf("quota: accepted=%d bytes=%d full=%v", count, total, full))
		got, body := poll(t, b)
		conformanceRequire(t, !(len(body) > MaxPollBytes+1024 || len(got) == 0 || len(got) >= count), fmt.Sprintf("poll: bytes=%d envelopes=%d queued=%d", len(body), len(got), count))
	})
	t.Run("unknown route", func(t *testing.T) {
		status, _ := raw(t, "GET", "/v1/nope", nil, nil, time.Now())
		conformanceRequire(t, status == 404, fmt.Sprintf("unknown route: %d", status))
	})
}
