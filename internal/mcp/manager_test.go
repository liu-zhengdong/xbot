package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type managerConfig struct {
	mu    sync.Mutex
	specs []Spec
	reads int
	err   error
}

func (c *managerConfig) read() ([]Spec, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reads++
	return append([]Spec(nil), c.specs...), c.err
}

func (c *managerConfig) set(specs ...Spec) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.specs = specs
}

type managerHTTP struct {
	server *httptest.Server
	inits  atomic.Int32
	calls  atomic.Int32
	tokens chan string
}

func newManagerHTTP(t *testing.T, initialize func(*http.Request), call func(*http.Request)) *managerHTTP {
	t.Helper()
	f := &managerHTTP{tokens: make(chan string, 20)}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprintf(w, "data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/message\",\"params\":{\"text\":\"note-%s\"}}\n\n", r.Header.Get("Mcp-Session-Id"))
			return
		}
		var msg rpcMessage
		if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
			http.Error(w, "bad frame", http.StatusBadRequest)
			return
		}
		var result any = map[string]any{}
		switch msg.Method {
		case "initialize":
			n := f.inits.Add(1)
			f.tokens <- r.Header.Get("TOKEN")
			if initialize != nil {
				initialize(r)
			}
			w.Header().Set("Mcp-Session-Id", fmt.Sprint(n))
		case "notifications/initialized":
			w.WriteHeader(http.StatusNoContent)
			return
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "echo", "inputSchema": map[string]any{"type": "object"}}}}
		case "tools/call":
			f.calls.Add(1)
			if call != nil {
				call(r)
			}
			result = map[string]any{"content": []any{map[string]any{"type": "image", "data": "unchanged"}}, "structuredContent": map[string]any{"answer": 42}, "unknown": true}
		default:
			http.Error(w, "unexpected method", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": msg.ID, "result": result})
	}))
	t.Cleanup(f.server.Close)
	return f
}

func TestManagerAsyncBindReuseIsolationAndNotifications(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	f := newManagerHTTP(t, func(r *http.Request) {
		if f := r.Header.Get("Mcp-Session-Id"); f != "" {
			t.Error("initialization reused an old session")
		}
		once.Do(func() { close(entered) })
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}, nil)
	owner := NewManager(context.Background(), Options{})
	var releaseOnce sync.Once
	defer func() { releaseOnce.Do(func() { close(release) }); _ = owner.Close() }()
	config := &managerConfig{}
	config.set(Spec{Name: "docs", URL: f.server.URL})
	notes := make(chan string, 10)
	bound := make(chan *BotRef, 1)
	bindErr := make(chan error, 1)
	go func() {
		ref, err := owner.Bind(Binding{Key: "one", ReadConfig: config.read, Notify: func(server, text string) error { notes <- server + ":" + text; return nil }})
		bound <- ref
		bindErr <- err
	}()
	var ref *BotRef
	select {
	case ref = <-bound:
		if err := <-bindErr; err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Bind waited for the transport handshake")
	}
	<-entered
	if specs := ref.Specs(); len(specs) != 1 || specs[0].Name != "docs" {
		t.Fatal(specs)
	}
	if status := ref.Status("docs"); status.State != "configured" {
		t.Fatal(status)
	}
	config.mu.Lock()
	reads := config.reads
	config.mu.Unlock()
	_ = ref.Specs()
	_ = ref.Status("docs")
	config.mu.Lock()
	if config.reads != reads {
		t.Error("cached specs/status reread config")
	}
	config.mu.Unlock()
	releaseOnce.Do(func() { close(release) })
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := ref.LoadTools(ctx, "docs"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		raw, err := ref.CallTool(ctx, "docs", "echo", map[string]any{})
		if err != nil || !strings.Contains(string(raw), "structuredContent") || !strings.Contains(string(raw), "unchanged") || !strings.Contains(string(raw), "unknown") {
			t.Fatalf("raw result %s: %v", raw, err)
		}
	}
	if err := ref.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if err := owner.Rekey(ref, "renamed"); err != nil {
		t.Fatal(err)
	}
	if _, err := ref.LoadTools(ctx, "docs"); err != nil || f.inits.Load() != 1 {
		t.Fatalf("reused binding reinitialized: %d %v", f.inits.Load(), err)
	}
	other, err := owner.Bind(Binding{Key: "two", ReadConfig: config.read})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.LoadTools(ctx, "docs"); err != nil || f.inits.Load() != 2 {
		t.Fatalf("different Bot shared session: %d %v", f.inits.Load(), err)
	}
	select {
	case text := <-notes:
		if !strings.HasPrefix(text, "docs:note-") {
			t.Fatal(text)
		}
	case <-ctx.Done():
		t.Fatal("initial notification was not delivered")
	}
	config.set(Spec{Name: "docs", URL: f.server.URL}, Spec{Name: "added", URL: f.server.URL})
	if err := ref.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := ref.LoadTools(ctx, "added"); err != nil {
		t.Fatal(err)
	}
	select {
	case text := <-notes:
		if !strings.HasPrefix(text, "added:note-") {
			t.Fatal(text)
		}
	case <-ctx.Done():
		t.Fatal("hot-added server notification was not delivered")
	}
	if f.inits.Load() != 3 || f.calls.Load() != 2 {
		t.Fatalf("initializes=%d calls=%d", f.inits.Load(), f.calls.Load())
	}
	if err := owner.Unbind(ref); err != nil {
		t.Fatal(err)
	}
	if _, err := ref.LoadTools(ctx, "docs"); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	if _, err := other.LoadTools(ctx, "docs"); err != nil {
		t.Fatal("unbinding one Bot closed the other", err)
	}
}

func TestManagerReplacementInterruptsCallWithoutRetry(t *testing.T) {
	entered := make(chan struct{})
	f := newManagerHTTP(t, nil, func(r *http.Request) { close(entered); <-r.Context().Done() })
	owner := NewManager(context.Background(), Options{})
	defer owner.Close()
	config := &managerConfig{}
	config.set(Spec{Name: "docs", URL: f.server.URL})
	ref, err := owner.Bind(Binding{Key: "one", ReadConfig: config.read})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := ref.LoadTools(ctx, "docs"); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := ref.CallTool(ctx, "docs", "echo", map[string]any{}); done <- err }()
	<-entered
	config.set(Spec{Name: "docs", URL: f.server.URL, Args: []string{"changed"}})
	if err := ref.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, ErrConfigChanged) || !strings.Contains(err.Error(), "outcome_unknown") {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("replacement did not interrupt old call")
	}
	if _, err := ref.LoadTools(ctx, "docs"); err != nil {
		t.Fatal(err)
	}
	if f.inits.Load() != 2 || f.calls.Load() != 1 {
		t.Fatalf("replacement retried: initializes=%d calls=%d", f.inits.Load(), f.calls.Load())
	}
}

type gatedManagerConn struct {
	io.ReadWriteCloser
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (c *gatedManagerConn) Close() error {
	c.once.Do(func() { close(c.entered) })
	<-c.release
	return c.ReadWriteCloser.Close()
}

func TestManagerReplacementJoinsTransportAndNotificationPump(t *testing.T) {
	closing, releaseClose := make(chan struct{}), make(chan struct{})
	notifying, releaseNote := make(chan struct{}), make(chan struct{})
	starts := make(chan int32, 5)
	var dials atomic.Int32
	dial := func(ctx context.Context) (io.ReadWriteCloser, error) {
		n := dials.Add(1)
		starts <- n
		conn, err := dialFake(t, func(msg rpcMessage) []rpcMessage {
			switch msg.Method {
			case "initialize":
				return []rpcMessage{rpcOK(msg.ID, `{}`)}
			case "notifications/initialized":
				if n == 1 {
					return []rpcMessage{rpcNote("notifications/message", `{"text":"old"}`)}
				}
			case "tools/list":
				return []rpcMessage{rpcOK(msg.ID, `{"tools":[{"name":"echo"}]}`)}
			}
			return nil
		})(ctx)
		if n == 1 {
			return &gatedManagerConn{ReadWriteCloser: conn, entered: closing, release: releaseClose}, err
		}
		return conn, err
	}
	owner := NewManager(context.Background(), Options{})
	var closeOnce, noteOnce sync.Once
	defer func() {
		closeOnce.Do(func() { close(releaseClose) })
		noteOnce.Do(func() { close(releaseNote) })
		_ = owner.Close()
	}()
	config := &managerConfig{}
	config.set(Spec{Name: "docs", Dial: dial})
	ref, err := owner.Bind(Binding{Key: "one", ReadConfig: config.read, Notify: func(string, string) error { close(notifying); <-releaseNote; return nil }})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := ref.LoadTools(ctx, "docs"); err != nil {
		t.Fatal(err)
	}
	<-notifying
	<-starts
	config.set(Spec{Name: "docs", Args: []string{"changed"}, Dial: dial})
	if err := ref.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	<-closing
	// These cache reads must remain available while old Close is blocked.
	_ = ref.Specs()
	_ = ref.Status("docs")
	select {
	case <-starts:
		t.Fatal("new connection opened before old transport closed")
	default:
	}
	closeOnce.Do(func() { close(releaseClose) })
	select {
	case <-starts:
		t.Fatal("new connection opened before old notification callback returned")
	default:
	}
	noteOnce.Do(func() { close(releaseNote) })
	if _, err := ref.LoadTools(ctx, "docs"); err != nil {
		t.Fatal(err)
	}
	if n := <-starts; n != 2 {
		t.Fatal(n)
	}
}

func TestManagerCredentialSnapshotRotationAndFailureRecovery(t *testing.T) {
	t.Setenv("TOKEN", "environment-must-not-leak")
	f := newManagerHTTP(t, nil, nil)
	var mu sync.Mutex
	values := map[string]string{"TOKEN": "first-private", "UNRELATED": "ignored"}
	var resolveErr error
	owner := NewManager(context.Background(), Options{ResolveSecrets: func(context.Context, []string) (map[string]string, error) {
		mu.Lock()
		defer mu.Unlock()
		out := map[string]string{}
		for key, value := range values {
			out[key] = value
		}
		return out, resolveErr
	}})
	defer owner.Close()
	config := &managerConfig{}
	config.set(Spec{Name: "docs", URL: f.server.URL, SecretEnvs: []string{"TOKEN"}})
	ref, err := owner.Bind(Binding{Key: "one", ReadConfig: config.read})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	load := func() {
		t.Helper()
		if _, err := ref.LoadTools(ctx, "docs"); err != nil {
			t.Fatal(err)
		}
	}
	load()
	if token := <-f.tokens; token != "first-private" {
		t.Fatal("initial header did not use snapshot")
	}
	mu.Lock()
	values["UNRELATED"] = "changed"
	mu.Unlock()
	if err := ref.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	load()
	if f.inits.Load() != 1 {
		t.Fatal("unrelated secret restarted session")
	}
	mu.Lock()
	values["TOKEN"] = "rotated-private"
	mu.Unlock()
	if err := ref.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	load()
	if token := <-f.tokens; token != "rotated-private" || f.inits.Load() != 2 {
		t.Fatal("rotation did not replace the header/session")
	}
	configErr := errors.New("invalid external URL")
	config.mu.Lock()
	config.err = configErr
	config.mu.Unlock()
	if err := ref.Reconcile(ctx); !errors.Is(err, configErr) {
		t.Fatal(err)
	}
	load()
	if f.inits.Load() != 2 {
		t.Fatal("bad config replaced the last effective session")
	}
	mu.Lock()
	delete(values, "TOKEN")
	mu.Unlock()
	if err := ref.Reconcile(ctx); !errors.Is(err, configErr) {
		t.Fatal(err)
	}
	if _, err := ref.LoadTools(ctx, "docs"); err == nil || !strings.Contains(err.Error(), "credentials") || !strings.Contains(err.Error(), "TOKEN") {
		t.Fatalf("missing credential hidden: %v", err)
	}
	mu.Lock()
	resolveErr = errors.New("plaintext unavailable")
	mu.Unlock()
	for range 2 {
		if err := ref.Reconcile(ctx); !errors.Is(err, configErr) {
			t.Fatal(err)
		}
		if _, err := ref.LoadTools(ctx, "docs"); err == nil || !strings.Contains(err.Error(), "plaintext unavailable") {
			t.Fatal(err)
		}
	}
	if f.inits.Load() != 2 {
		t.Fatal("failed resolution fell back to environment or respawned")
	}
	mu.Lock()
	resolveErr = nil
	values["TOKEN"] = "restored-private"
	mu.Unlock()
	config.mu.Lock()
	config.err = nil
	config.mu.Unlock()
	if err := ref.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	load()
	if token := <-f.tokens; token != "restored-private" || f.inits.Load() != 3 {
		t.Fatal("restored credentials did not recover the session")
	}
}

func TestManagerPauseKeepsSessionDiscoveryAndNotifications(t *testing.T) {
	var inits, calls atomic.Int32
	notes := make(chan string, 1)
	spec := Spec{Name: "docs", Dial: dialFake(t, func(msg rpcMessage) []rpcMessage {
		switch msg.Method {
		case "initialize":
			inits.Add(1)
			return []rpcMessage{rpcOK(msg.ID, `{}`)}
		case "tools/list":
			return []rpcMessage{rpcNote("notifications/message", `{"text":"still-live"}`), rpcOK(msg.ID, `{"tools":[{"name":"echo"}]}`)}
		case "tools/call":
			calls.Add(1)
			return []rpcMessage{rpcOK(msg.ID, `{"content":[{"type":"text","text":"same-session"}]}`)}
		}
		return nil
	})}
	lifetime, stop := context.WithCancel(context.Background())
	defer stop()
	owner := NewManager(lifetime, Options{})
	defer owner.Close()
	ref, err := owner.Bind(Binding{Key: "one", ReadConfig: func() ([]Spec, error) { return []Spec{spec}, nil }, Notify: func(server, text string) error { notes <- server + ":" + text; return nil }})
	if err != nil {
		t.Fatal(err)
	}
	if err := ref.Pause(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := ref.LoadTools(ctx, "docs"); err != nil {
		t.Fatal("pause blocked discovery", err)
	}
	if status := ref.Status("docs"); status.State != "ready" || status.ToolCount != 1 {
		t.Fatal(status)
	}
	if text := awaitMCP(t, notes); text != "docs:still-live" {
		t.Fatal("pause stopped notifications", text)
	}
	if _, err := ref.CallTool(ctx, "docs", "echo", map[string]any{}); !errors.Is(err, ErrPaused) {
		t.Fatal(err)
	}
	if _, err := ref.CallToolForSpec(ctx, spec, "echo", map[string]any{}); !errors.Is(err, ErrPaused) {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatal("paused reference sent tools/call")
	}
	if err := ref.Resume(); err != nil {
		t.Fatal(err)
	}
	if _, err := ref.CallToolForSpec(ctx, spec, "echo", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || inits.Load() != 1 {
		t.Fatal("resume recreated the session or retried the call")
	}
	stop()
	if err := ref.Resume(); !errors.Is(err, ErrClosed) {
		t.Fatal("host shutdown was resumable", err)
	}
}
