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

func TestBotRefReadOnlyExactHintAndRawArguments(t *testing.T) {
	var calls atomic.Int32
	frames := make(chan json.RawMessage, 4)
	spec := Spec{Name: "docs", Dial: dialFake(t, func(msg rpcMessage) []rpcMessage {
		switch msg.Method {
		case "initialize":
			return []rpcMessage{rpcOK(msg.ID, `{}`)}
		case "tools/list":
			return []rpcMessage{rpcOK(msg.ID, `{"tools":[{"name":"query/get","annotations":{"readOnlyHint":true}},{"name":"write","annotations":{"readOnlyHint":false}},{"name":"unknown"}]}`)}
		case "tools/call":
			calls.Add(1)
			frames <- msg.Params
			return []rpcMessage{rpcOK(msg.ID, `{"structuredContent":{"answer":42},"custom":{"kept":true}}`)}
		}
		return nil
	})}
	owner := NewManager(context.Background(), Options{})
	defer owner.Close()
	ref, err := owner.Bind(Binding{Key: "one", ReadConfig: func() ([]Spec, error) { return []Spec{spec}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for _, remote := range []string{"write", "unknown", "missing", "query_get", " query/get"} {
		if _, err := ref.CallTool(ctx, "docs", remote, json.RawMessage(`{}`), CallOptions{ReadOnly: true}); !errors.Is(err, ErrNotReadOnly) {
			t.Fatalf("%q: %v", remote, err)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("a missing/false hint or normalized name sent an RPC")
	}
	arguments := json.RawMessage(`{"large":9007199254740993,"nested":{"custom":[null,true]}}`)
	raw, err := ref.CallTool(ctx, "docs", "query/get", arguments, CallOptions{ReadOnly: true})
	if err != nil || !strings.Contains(string(raw), `"custom":{"kept":true}`) {
		t.Fatalf("raw=%s error=%v", raw, err)
	}
	var frame struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(awaitMCP(t, frames), &frame); err != nil {
		t.Fatal(err)
	}
	if frame.Name != "query/get" || string(frame.Arguments) != string(arguments) {
		t.Fatalf("frame=%s", frame.Arguments)
	}
	if _, err := ref.CallTool(ctx, "docs", "write", json.RawMessage(`{}`), CallOptions{}); err != nil {
		t.Fatal("ordinary call inherited readonly restriction", err)
	}
	if calls.Load() != 2 {
		t.Fatal("unexpected call count", calls.Load())
	}
}

func TestBotRefReadOnlyReplacementDuringListNeverReroutes(t *testing.T) {
	listing, releaseList := make(chan struct{}), make(chan struct{})
	closing, releaseClose := make(chan struct{}), make(chan struct{})
	var oldCalls, newCalls atomic.Int32
	oldDial := dialFake(t, func(msg rpcMessage) []rpcMessage {
		switch msg.Method {
		case "initialize":
			return []rpcMessage{rpcOK(msg.ID, `{}`)}
		case "tools/list":
			close(listing)
			<-releaseList
			return []rpcMessage{rpcOK(msg.ID, `{"tools":[{"name":"lookup","annotations":{"readOnlyHint":true}}]}`)}
		case "tools/call":
			oldCalls.Add(1)
		}
		return nil
	})
	old := Spec{Name: "docs", Args: []string{"old"}, Dial: func(ctx context.Context) (io.ReadWriteCloser, error) {
		conn, err := oldDial(ctx)
		return &gatedManagerConn{ReadWriteCloser: conn, entered: closing, release: releaseClose}, err
	}}
	next := Spec{Name: "docs", Args: []string{"new"}, Dial: dialFake(t, func(msg rpcMessage) []rpcMessage {
		switch msg.Method {
		case "initialize":
			return []rpcMessage{rpcOK(msg.ID, `{}`)}
		case "tools/list":
			return []rpcMessage{rpcOK(msg.ID, `{"tools":[{"name":"lookup","annotations":{"readOnlyHint":false}}]}`)}
		case "tools/call":
			newCalls.Add(1)
		}
		return nil
	})}
	config := &managerConfig{}
	config.set(old)
	owner := NewManager(context.Background(), Options{})
	var listOnce, closeOnce sync.Once
	defer func() {
		listOnce.Do(func() { close(releaseList) })
		closeOnce.Do(func() { close(releaseClose) })
		_ = owner.Close()
	}()
	ref, err := owner.Bind(Binding{Key: "one", ReadConfig: config.read})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := ref.CallTool(ctx, "docs", "lookup", json.RawMessage(`{}`), CallOptions{ReadOnly: true})
		done <- err
	}()
	awaitMCP(t, listing)
	config.set(next)
	if err := ref.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	awaitMCP(t, closing)
	listOnce.Do(func() { close(releaseList) })
	// The new session cannot start until releaseClose. An implementation that
	// reroutes after checking the old hint would wait here or hit the new tool.
	if err := awaitMCP(t, done); !errors.Is(err, ErrConfigChanged) {
		t.Fatal(err)
	}
	closeOnce.Do(func() { close(releaseClose) })
	if _, err := ref.LoadTools(ctx, "docs"); err != nil {
		t.Fatal(err)
	}
	if oldCalls.Load() != 0 || newCalls.Load() != 0 {
		t.Fatal("readonly discovery switched execution sessions")
	}
	if _, err := ref.CallTool(ctx, "docs", "lookup", json.RawMessage(`{}`), CallOptions{ReadOnly: true}); !errors.Is(err, ErrNotReadOnly) {
		t.Fatal(err)
	}
}

func TestOpenFreezesPendingCredentialsAndLifetime(t *testing.T) {
	t.Setenv("TOKEN", "fixture-environment")
	t.Setenv("PROVIDER", "fixture-provider")
	pending := map[string]string{"TOKEN": "fixture-pending"}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	var resolverCalls, legacyCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.Header.Get("TOKEN") != "fixture-pending" || r.Header.Get("PROVIDER") != "" {
			t.Error("probe did not use its frozen pending credentials")
		}
		var msg rpcMessage
		if json.NewDecoder(r.Body).Decode(&msg) != nil {
			t.Error("invalid RPC")
			return
		}
		if msg.Method == "initialize" {
			once.Do(func() { close(entered) })
			<-release
		}
		if msg.Method == "notifications/initialized" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		result := json.RawMessage(`{}`)
		if msg.Method == "tools/list" {
			result = json.RawMessage(`{"tools":[{"name":"probe"}]}`)
		}
		if msg.Method == "tools/call" {
			result = json.RawMessage(`{"content":[{"type":"text","text":"ok"}]}`)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": msg.ID, "result": result})
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	opened := make(chan *Session, 1)
	openErr := make(chan error, 1)
	go func() {
		session, err := Open(ctx, Spec{Name: "probe", URL: srv.URL, SecretEnvs: []string{"TOKEN", "PROVIDER"}, Secret: func(string) (string, bool) { legacyCalls.Add(1); return "fixture-old", true }}, Options{
			IsProviderKey: func(name string) bool { return name == "PROVIDER" },
			ResolveSecrets: func(_ context.Context, names []string) (map[string]string, error) {
				resolverCalls.Add(1)
				if len(names) != 1 || names[0] != "TOKEN" {
					t.Error("provider name reached resolver")
				}
				return pending, nil
			},
			Redact: func(text string) string { return strings.ReplaceAll(text, "fixture-rotated", "[secret]") },
		})
		opened <- session
		openErr <- err
	}()
	awaitMCP(t, entered)
	pending["TOKEN"] = "fixture-rotated"
	close(release)
	session := awaitMCP(t, opened)
	if err := awaitMCP(t, openErr); err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if _, err := session.LoadTools(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := session.CallTool(ctx, "probe", json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	if resolverCalls.Load() != 1 || legacyCalls.Load() != 0 {
		t.Fatal("probe reread credentials or used the legacy fallback")
	}
	redacted := session.diagnostic("probe", fmt.Errorf("fixture-pending fixture-rotated"))
	if strings.Contains(redacted.Error(), "fixture-pending") || strings.Contains(redacted.Error(), "fixture-rotated") {
		t.Fatal("probe diagnostics lost snapshot/current redaction")
	}
	cancel()
	_ = session.Close()
	if session.ctx.Err() == nil {
		t.Fatal("probe lifetime outlived its context")
	}
}

func TestOpenResolverFailureNeverFallsBack(t *testing.T) {
	t.Setenv("TOKEN", "fixture-environment")
	for _, unavailable := range []bool{false, true} {
		t.Run(fmt.Sprint(unavailable), func(t *testing.T) {
			var dials, legacy atomic.Int32
			problem := errors.New("plaintext unavailable")
			session, err := Open(context.Background(), Spec{Name: "probe", SecretEnvs: []string{"TOKEN"}, Secret: func(string) (string, bool) { legacy.Add(1); return "fixture-old", true }, Dial: func(context.Context) (io.ReadWriteCloser, error) {
				dials.Add(1)
				return nil, errors.New("unexpected dial")
			}}, Options{ResolveSecrets: func(context.Context, []string) (map[string]string, error) {
				if unavailable {
					return nil, problem
				}
				return map[string]string{}, nil
			}})
			if err == nil || session != nil || dials.Load() != 0 || legacy.Load() != 0 {
				t.Fatalf("session=%v error=%v dials=%d legacy=%d", session, err, dials.Load(), legacy.Load())
			}
			if unavailable && !errors.Is(err, problem) {
				t.Fatal(err)
			}
		})
	}
}
