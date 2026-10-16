package vault

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// daemonHarness is one running daemon plus a raw unix-socket HTTP client.
type daemonHarness struct {
	socket  string
	dataDir string
	client  *Client
	raw     *http.Client
}

func startDaemon(t *testing.T, srv *Server) *daemonHarness {
	t.Helper()
	base := t.TempDir()
	socket := filepath.Join(base, "v.sock")
	srv.DataDir = filepath.Join(base, "data")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Skipf("unix socket unavailable: %v", err)
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = ln.Close() })
	waitSocket(t, socket)
	return &daemonHarness{
		socket:  socket,
		dataDir: srv.DataDir,
		raw:     rawClient(t, socket),
	}
}

func newDaemon(t *testing.T) *daemonHarness {
	return startDaemon(t, &Server{AllowUIDs: []int{os.Getuid()}})
}

func waitSocket(t *testing.T, socket string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.Dial("unix", socket)
		if err == nil {
			_ = c.Close()
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("daemon socket %s never became ready", socket)
}

func rawClient(t *testing.T, socket string) *http.Client {
	t.Helper()
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", socket)
			},
		},
		Timeout: 5 * time.Second,
	}
}

func (h *daemonHarness) openClient(t *testing.T, bot string) *Client {
	t.Helper()
	c, err := OpenClient(h.socket, bot)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// mapTransport routes fake hostnames to local httptest servers.
type mapTransport struct {
	routes map[string]string
}

func (m mapTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base, ok := m.routes[req.URL.Host]
	if !ok {
		return nil, fmt.Errorf("no route for %s", req.URL.Host)
	}
	u, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	out := req.Clone(req.Context())
	out.URL.Scheme = u.Scheme
	out.URL.Host = u.Host
	return http.DefaultTransport.RoundTrip(out)
}

func TestDaemonListNeverHasValues(t *testing.T) {
	h := newDaemon(t)
	c := h.openClient(t, "bot1")
	const value = "s3cr3t-value-xyz"
	if err := c.Set("TOKEN", value, []string{"a.com"}); err != nil {
		t.Fatal(err)
	}

	list := c.List()
	if len(list) != 1 || list[0].Name != "TOKEN" {
		t.Fatalf("list %+v", list)
	}
	if list[0].Value != "" {
		t.Fatalf("client list leaked a value: %+v", list[0])
	}
	if names := c.Names(); len(names) != 1 || names[0] != "TOKEN" {
		t.Fatalf("names %v", names)
	}

	// The raw wire response must not carry the value or a value field.
	req, _ := http.NewRequest(http.MethodGet, "http://vault/v1/list", nil)
	req.Header.Set(headerBot, "bot1")
	resp, err := h.raw.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(value)) {
		t.Fatalf("wire list leaked the value: %s", raw)
	}
	if bytes.Contains(raw, []byte(`"value"`)) {
		t.Fatalf("wire list has a value field: %s", raw)
	}
	var entries []map[string]any
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatalf("list json %q: %v", raw, err)
	}
	if len(entries) != 1 || entries[0]["name"] != "TOKEN" {
		t.Fatalf("entries %v", entries)
	}
}

func TestDaemonForwardInjectsToBoundHost(t *testing.T) {
	const value = "inject-value-42"
	var seenHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenHeader = r.Header.Get("X-Token")
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	h := startDaemon(t, &Server{
		AllowUIDs: []int{os.Getuid()},
		Base:      mapTransport{routes: map[string]string{"bound.example": srv.URL}},
	})
	c := h.openClient(t, "bot1")
	if err := c.Set("TOKEN", value, []string{"bound.example"}); err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequest(http.MethodGet, "http://bound.example/x", nil)
	req.Header.Set("X-Token", "{{secret:TOKEN}}")
	resp, err := c.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if seenHeader != value {
		t.Fatalf("server saw %q", seenHeader)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" {
		t.Fatalf("body %q", body)
	}
}

func TestDaemonForwardRefusesUnboundHost(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
	}))
	defer srv.Close()

	h := startDaemon(t, &Server{
		AllowUIDs: []int{os.Getuid()},
		Base:      mapTransport{routes: map[string]string{"evil.example": srv.URL}},
	})
	c := h.openClient(t, "bot1")
	if err := c.Set("TOKEN", "value-9", []string{"bound.example"}); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, "http://evil.example/x", nil)
	req.Header.Set("X-Token", "{{secret:TOKEN}}")
	if _, err := c.RoundTrip(req); err == nil {
		t.Fatal("unbound host was allowed")
	}
	if hits != 0 {
		t.Fatalf("unbound server was hit %d times", hits)
	}
}

func TestDaemonRedactsValueInResponseHeader(t *testing.T) {
	const value = "header-echo-secret-77"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Echo", "token="+r.Header.Get("X-Token"))
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	h := startDaemon(t, &Server{
		AllowUIDs: []int{os.Getuid()},
		Base:      mapTransport{routes: map[string]string{"bound.example": srv.URL}},
	})
	c := h.openClient(t, "bot1")
	if err := c.Set("TOKEN", value, []string{"bound.example"}); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, "http://bound.example/x", nil)
	req.Header.Set("X-Token", "{{secret:TOKEN}}")
	resp, err := c.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got := resp.Header.Get("X-Echo")
	if strings.Contains(got, value) {
		t.Fatalf("value leaked in a response header: %q", got)
	}
	if !strings.Contains(got, "[secret]") {
		t.Fatalf("response header not masked: %q", got)
	}
}

func TestDaemonRedirectToUnboundHostCarriesNoValue(t *testing.T) {
	const value = "redirect-value-7"
	unboundHits := 0
	unbound := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		unboundHits++
		_, _ = io.WriteString(w, r.Header.Get("X-Token"))
	}))
	defer unbound.Close()
	bound := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://evil.example/steal", http.StatusFound)
	}))
	defer bound.Close()

	h := startDaemon(t, &Server{
		AllowUIDs: []int{os.Getuid()},
		Base: mapTransport{routes: map[string]string{
			"bound.example": bound.URL,
			"evil.example":  unbound.URL,
		}},
	})
	c := h.openClient(t, "bot1")
	if err := c.Set("TOKEN", value, []string{"bound.example"}); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: c, Timeout: 5 * time.Second}
	req, _ := http.NewRequest(http.MethodGet, "http://bound.example/start", nil)
	req.Header.Set("X-Token", "{{secret:TOKEN}}")
	if _, err := client.Do(req); err == nil {
		t.Fatal("redirect to an unbound host was followed")
	}
	if unboundHits != 0 {
		t.Fatalf("unbound server was hit %d times", unboundHits)
	}
}

func TestDaemonRedactsValueAcrossChunkBoundary(t *testing.T) {
	const value = "straddle-secret-value-123456"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, _ := w.(http.Flusher)
		payload := "before " + value + " after"
		for i := 0; i < len(payload); i++ {
			_, _ = io.WriteString(w, payload[i:i+1])
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
	defer srv.Close()

	h := startDaemon(t, &Server{
		AllowUIDs: []int{os.Getuid()},
		Base:      mapTransport{routes: map[string]string{"bound.example": srv.URL}},
	})
	c := h.openClient(t, "bot1")
	if err := c.Set("TOKEN", value, []string{"bound.example"}); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, "http://bound.example/x", nil)
	req.Header.Set("X-Token", "{{secret:TOKEN}}")
	resp, err := c.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(body), value) {
		t.Fatalf("value leaked across chunks: %q", body)
	}
	if !strings.Contains(string(body), "[secret]") {
		t.Fatalf("value not masked: %q", body)
	}
}

func TestDaemonRejectsDisallowedUID(t *testing.T) {
	h := startDaemon(t, &Server{AllowUIDs: []int{os.Getuid() + 424242}})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://vault/v1/list", nil)
	req.Header.Set(headerBot, "bot1")
	if _, err := h.raw.Do(req); err == nil {
		t.Fatal("disallowed uid was served")
	}
}

func TestDaemonRefusesOversizedPlaceholderBody(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
	}))
	defer srv.Close()

	h := startDaemon(t, &Server{
		AllowUIDs: []int{os.Getuid()},
		Base:      mapTransport{routes: map[string]string{"bound.example": srv.URL}},
	})
	c := h.openClient(t, "bot1")
	if err := c.Set("TOKEN", "v", []string{"bound.example"}); err != nil {
		t.Fatal(err)
	}
	body := strings.Repeat("a", maxForwardBody) + "{{secret:TOKEN}}" + strings.Repeat("b", 1024)
	req, _ := http.NewRequest(http.MethodPost, "http://bound.example/x", strings.NewReader(body))
	req.ContentLength = int64(len(body))
	_, err := c.RoundTrip(req)
	if err == nil {
		t.Fatal("oversized placeholder body was accepted")
	}
	if !strings.Contains(err.Error(), strconv.Itoa(http.StatusRequestEntityTooLarge)) {
		t.Fatalf("oversized body error %q does not report 413", err)
	}
	if strings.Contains(err.Error(), "{{secret:") {
		t.Fatalf("error echoed the placeholder: %q", err)
	}
	if hits != 0 {
		t.Fatalf("server was hit %d times", hits)
	}
}

func TestDaemonStreamsLargeBodyWithoutPlaceholder(t *testing.T) {
	const size = 2 << 20
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, _ := io.Copy(io.Discard, r.Body)
		_, _ = fmt.Fprintf(w, "%d", n)
	}))
	defer srv.Close()

	h := startDaemon(t, &Server{
		AllowUIDs: []int{os.Getuid()},
		Base:      mapTransport{routes: map[string]string{"bound.example": srv.URL}},
	})
	c := h.openClient(t, "bot1")
	body := strings.Repeat("z", size)
	req, _ := http.NewRequest(http.MethodPost, "http://bound.example/x", strings.NewReader(body))
	req.ContentLength = int64(len(body))
	resp, err := c.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)
	if strings.TrimSpace(string(got)) != fmt.Sprintf("%d", size) {
		t.Fatalf("server read %q bytes, want %d", got, size)
	}
}

// TestDaemonOverheadP95 measures the p95 added latency of the daemon link over
// 300 small warm GETs and fails if it exceeds 5ms.
func TestDaemonOverheadP95(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	h := startDaemon(t, &Server{
		AllowUIDs: []int{os.Getuid()},
		Base:      mapTransport{routes: map[string]string{"bound.example": srv.URL}},
	})
	c := h.openClient(t, "bot1")
	if err := c.Set("TOKEN", "v", []string{"bound.example"}); err != nil {
		t.Fatal(err)
	}
	direct := &http.Client{Timeout: 5 * time.Second, Transport: mapTransport{routes: map[string]string{"bound.example": srv.URL}}}
	viaDaemon := &http.Client{Timeout: 5 * time.Second, Transport: c}

	get := func(client *http.Client) {
		resp, err := client.Get("http://bound.example/warm")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
	for i := 0; i < 20; i++ {
		get(direct)
		get(viaDaemon)
	}

	const n = 300
	overheads := make([]time.Duration, 0, n)
	for i := 0; i < n; i++ {
		t0 := time.Now()
		get(direct)
		directDur := time.Since(t0)

		t1 := time.Now()
		get(viaDaemon)
		viaDur := time.Since(t1)

		overheads = append(overheads, viaDur-directDur)
	}
	sort.Slice(overheads, func(i, j int) bool { return overheads[i] < overheads[j] })
	idx := int(math.Ceil(0.95*float64(len(overheads)))) - 1
	if idx < 0 {
		idx = 0
	}
	p95 := overheads[idx]
	var sum time.Duration
	for _, d := range overheads {
		sum += d
	}
	mean := sum / time.Duration(len(overheads))
	t.Logf("daemon overhead over %d warm GETs: p50=%v p95=%v mean=%v", n, overheads[n/2], p95, mean)
	if p95 > 5*time.Millisecond {
		t.Fatalf("daemon p95 overhead %v exceeds 5ms", p95)
	}
}

func BenchmarkDaemonForward(b *testing.B) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	base := b.TempDir()
	socket := filepath.Join(base, "v.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		b.Fatal(err)
	}
	defer ln.Close()
	server := &Server{
		DataDir:   filepath.Join(base, "data"),
		AllowUIDs: []int{os.Getuid()},
		Base:      mapTransport{routes: map[string]string{"bound.example": srv.URL}},
	}
	go func() { _ = server.Serve(ln) }()
	waitSocketB(b, socket)

	c, err := OpenClient(socket, "bot1")
	if err != nil {
		b.Fatal(err)
	}
	if err := c.Set("TOKEN", "v", []string{"bound.example"}); err != nil {
		b.Fatal(err)
	}
	client := &http.Client{Transport: c, Timeout: 5 * time.Second}
	req, _ := http.NewRequest(http.MethodGet, "http://bound.example/x", nil)
	req.Header.Set("X-Token", "{{secret:TOKEN}}")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := client.Do(req)
		if err != nil {
			b.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
}

func waitSocketB(b *testing.B, socket string) {
	b.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.Dial("unix", socket)
		if err == nil {
			_ = c.Close()
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	b.Fatalf("socket %s never became ready", socket)
}
