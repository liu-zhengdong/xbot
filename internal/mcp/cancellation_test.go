package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func awaitMCP[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(3 * time.Second):
		t.Fatal("MCP fixture did not reach its synchronization point")
		var zero T
		return zero
	}
}

func writeClient(t *testing.T, conn io.ReadWriteCloser) *mcpClient {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	var client *mcpClient
	client = newMCPClient(conn, ctx, func(error) { cancel(); _ = client.close() })
	t.Cleanup(func() { cancel(); _ = client.close(); client.controls.Wait() })
	return client
}

type observedWriteConn struct {
	net.Conn
	entered chan struct{}
}

func (c *observedWriteConn) Write(p []byte) (int, error) {
	select {
	case c.entered <- struct{}{}:
	default:
	}
	return c.Conn.Write(p)
}

type gateContext struct {
	context.Context
	entered chan struct{}
	once    sync.Once
}

func (c *gateContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.entered) })
	return c.Context.Done()
}

type frameOutcome struct {
	sent bool
	err  error
}

func TestStdioFrameGateCancellationKeepsOtherWrite(t *testing.T) {
	conn, peer := net.Pipe()
	defer peer.Close()
	tracked := &observedWriteConn{Conn: conn, entered: make(chan struct{}, 2)}
	client := writeClient(t, tracked)
	aDone := make(chan frameOutcome, 1)
	go func() {
		sent, err := client.writeContext(context.Background(), map[string]string{"who": "A"})
		aDone <- frameOutcome{sent, err}
	}()
	awaitMCP(t, tracked.entered)
	base, cancel := context.WithCancel(context.Background())
	ctx := &gateContext{Context: base, entered: make(chan struct{})}
	bDone := make(chan frameOutcome, 1)
	go func() {
		sent, err := client.writeContext(ctx, map[string]string{"who": "B"})
		bDone <- frameOutcome{sent, err}
	}()
	awaitMCP(t, ctx.entered)
	cancel()
	if out := awaitMCP(t, bDone); out.sent || !errors.Is(out.err, context.Canceled) {
		t.Fatalf("B sent=%v error=%v", out.sent, out.err)
	}
	select {
	case out := <-aDone:
		t.Fatalf("canceling gate waiter ended A: %+v", out)
	default:
	}
	line, err := bufio.NewReader(peer).ReadString('\n')
	if err != nil || !strings.Contains(line, `"who":"A"`) {
		t.Fatalf("frame=%q error=%v", line, err)
	}
	if out := awaitMCP(t, aDone); !out.sent || out.err != nil || client.failure() != nil {
		t.Fatalf("A sent=%v error=%v failure=%v", out.sent, out.err, client.failure())
	}
}

func TestStdioZeroByteDeadlineCancellationKeepsStream(t *testing.T) {
	conn, peer := net.Pipe()
	defer peer.Close()
	tracked := &observedWriteConn{Conn: conn, entered: make(chan struct{}, 2)}
	client := writeClient(t, tracked)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan frameOutcome, 1)
	go func() {
		sent, err := client.writeContext(ctx, map[string]string{"who": "canceled"})
		done <- frameOutcome{sent, err}
	}()
	awaitMCP(t, tracked.entered)
	cancel()
	if out := awaitMCP(t, done); out.sent || !errors.Is(out.err, context.Canceled) || client.failure() != nil {
		t.Fatalf("zero-byte cancellation: sent=%v err=%v failure=%v", out.sent, out.err, client.failure())
	}
	go func() {
		sent, err := client.writeContext(context.Background(), map[string]string{"who": "next"})
		done <- frameOutcome{sent, err}
	}()
	line, err := bufio.NewReader(peer).ReadString('\n')
	if err != nil || !strings.Contains(line, `"who":"next"`) || strings.Contains(line, "canceled") {
		t.Fatalf("stream after cancellation=%q err=%v", line, err)
	}
	if out := awaitMCP(t, done); !out.sent || out.err != nil {
		t.Fatal(out)
	}
}

func TestStdioPartialFrameCancellationAbortsAllPendingCalls(t *testing.T) {
	conn, peer := net.Pipe()
	defer peer.Close()
	client := writeClient(t, conn)
	firstRead, prefixRead := make(chan struct{}), make(chan struct{})
	go func() {
		reader := bufio.NewReader(peer)
		_, _ = reader.ReadString('\n')
		close(firstRead)
		// A buffered reader could prefetch the entire second frame.
		_, _ = io.ReadFull(peer, make([]byte, 7))
		close(prefixRead)
	}()
	firstDone := make(chan error, 1)
	go func() {
		_, err := client.call(context.Background(), "tools/call", map[string]string{"who": "waiting-for-reply"})
		firstDone <- err
	}()
	awaitMCP(t, firstRead)
	ctx, cancel := context.WithCancel(context.Background())
	partialDone := make(chan error, 1)
	go func() {
		_, err := client.call(ctx, "tools/call", map[string]string{"who": "partial"})
		partialDone <- err
	}()
	awaitMCP(t, prefixRead)
	cancel()
	for _, done := range []chan error{partialDone, firstDone} {
		if err := awaitMCP(t, done); err == nil || !strings.Contains(err.Error(), "outcome_unknown") {
			t.Fatalf("damaged transport did not report unknown outcome: %v", err)
		}
	}
	if client.failure() == nil || len(client.pending) != 0 {
		t.Fatal("damaged frame left a reusable transport or pending request")
	}
}

type failedFrameConn struct {
	n      int
	err    error
	writes int
	closed bool
}

func (c *failedFrameConn) Read([]byte) (int, error)  { return 0, io.EOF }
func (c *failedFrameConn) Write([]byte) (int, error) { c.writes++; return c.n, c.err }
func (c *failedFrameConn) Close() error              { c.closed = true; return nil }

func TestStdioHardZeroAndShortWritesAbortTransport(t *testing.T) {
	for _, tc := range []struct {
		name string
		n    int
		err  error
	}{
		{"epipe", 0, syscall.EPIPE}, {"closed", 0, io.ErrClosedPipe}, {"short", 1, nil}, {"zero-short", 0, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn := &failedFrameConn{n: tc.n, err: tc.err}
			client := writeClient(t, conn)
			_, err := client.writeContext(context.Background(), map[string]string{"frame": "complete"})
			if err == nil || !strings.Contains(err.Error(), "outcome_unknown") || !conn.closed {
				t.Fatalf("write=%v closed=%v", err, conn.closed)
			}
			_, _ = client.writeContext(context.Background(), map[string]string{"frame": "must-not-send"})
			if conn.writes != 1 {
				t.Fatal("a damaged stream accepted a second frame")
			}
		})
	}
}

// This scripted child deliberately stays alive while not consuming stdin. It
// proves that abort kills the owning process before waiting for it to exit.
func TestMCPWriteChildProcess(t *testing.T) {
	if len(os.Args) < 2 {
		return
	}
	mode := os.Args[len(os.Args)-1]
	if mode != "blocked-stdin" && mode != "closed-stdin-request" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var msg rpcMessage
		if json.Unmarshal(scanner.Bytes(), &msg) != nil {
			os.Exit(2)
		}
		if msg.Method == "initialize" {
			raw, _ := json.Marshal(rpcOK(msg.ID, `{}`))
			fmt.Println(string(raw))
		}
		if msg.Method == "notifications/initialized" {
			if mode == "closed-stdin-request" {
				_ = os.Stdin.Close()
				fmt.Println(`{"jsonrpc":"2.0","id":9000,"method":"unsupported/server-request","params":{}}`)
			} else {
				fmt.Println(`{"jsonrpc":"2.0","method":"notifications/message","params":{"text":"stdin-paused"}}`)
			}
			for {
				time.Sleep(time.Minute)
			}
		}
	}
	os.Exit(3)
}

type noDeadlineWriter struct{ io.WriteCloser }

func TestStdioBlockedProcessCancellationReapsChild(t *testing.T) {
	for _, mode := range []string{"deadline", "no-deadline"} {
		t.Run(mode, func(t *testing.T) {
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			session := NewSession(Spec{Name: "blocked", Command: exe, Args: []string{"-test.run=^TestMCPWriteChildProcess$", "blocked-stdin"}}, Options{})
			defer session.Close()
			if err := session.Connect(context.Background()); err != nil {
				t.Fatal(err)
			}
			if note := awaitMCP(t, session.Notifications()); note != "stdin-paused" {
				t.Fatal(note)
			}
			transport := session.client.conn.(*stdioConn)
			if mode == "no-deadline" {
				transport.w = noDeadlineWriter{transport.w}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				_, err := session.CallTool(ctx, "blocked", map[string]string{"payload": strings.Repeat("x", 256<<10)})
				done <- err
			}()
			if err := awaitMCP(t, done); err == nil || !strings.Contains(err.Error(), "outcome_unknown") {
				t.Fatal(err)
			}
			_ = session.Close()
			reader := transport.Reader.(*mcpProcessReader)
			if session.ctx.Err() == nil || reader.cmd.ProcessState == nil {
				t.Fatal("cancellation left the child alive or unreaped")
			}
		})
	}
}

func TestStdioUnsupportedReplyFailureDoesNotJoinItsOwnReader(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	session := NewSession(Spec{Name: "closed-stdin", Command: exe, Args: []string{"-test.run=^TestMCPWriteChildProcess$", "closed-stdin-request"}}, Options{})
	defer session.Close()
	if err := session.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	awaitMCP(t, session.readDone)
	_ = session.Close()
	reader := session.client.conn.(*stdioConn).Reader.(*mcpProcessReader)
	if reader.cmd.ProcessState == nil || !strings.Contains(session.Status().Error, "outcome_unknown") {
		t.Fatal("read-loop abort did not stop/reap its child or preserve transport failure")
	}
}

type requestRoundTripper func(*http.Request) (*http.Response, error)

func (fn requestRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestHTTPToolRequestKeepsCallerDeadlineAndRejectsOversize(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	conn, err := openStreamable(ctx, Spec{URL: "http://fixture.invalid/mcp"}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	h := conn.(*httpConn)
	wantDeadline := time.Now().Add(time.Hour)
	h.hc.Transport = requestRoundTripper(func(r *http.Request) (*http.Response, error) {
		deadline, ok := r.Context().Deadline()
		if !ok || !deadline.Equal(wantDeadline) {
			t.Error("HTTP request replaced the caller deadline", deadline)
		}
		return &http.Response{StatusCode: http.StatusNoContent, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	callCtx, stop := context.WithDeadline(ctx, wantDeadline)
	defer stop()
	if _, err := h.WriteRequest(callCtx, []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call"}`)); err != nil {
		t.Fatal(err)
	}
	h.hc.Transport = requestRoundTripper(func(r *http.Request) (*http.Response, error) {
		if _, ok := r.Context().Deadline(); ok {
			t.Error("HTTP tool request imposed a timeout without a caller deadline")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", (1<<20)+1)))}, nil
	})
	if _, err := h.WriteRequest(ctx, []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call"}`)); err == nil || !strings.Contains(err.Error(), "exceeds 1 MiB") {
		t.Fatal("oversized HTTP response was silently truncated", err)
	}
}

func TestToolSpecPreservesRemoteJSON(t *testing.T) {
	remote := `{"name":"custom","description":"raw definition","inputSchema":{"type":"object","properties":{"x":{"customSchema":true}}},"annotations":{"readOnlyHint":true,"destructiveHint":false,"custom":{"risk":7}},"customSpec":{"unknown":[null,1e2]}}`
	session := NewSession(Spec{Name: "raw", Dial: dialFake(t, func(msg rpcMessage) []rpcMessage {
		switch msg.Method {
		case "initialize":
			return []rpcMessage{rpcOK(msg.ID, `{}`)}
		case "tools/list":
			return []rpcMessage{rpcOK(msg.ID, `{"tools":[`+remote+`]}`)}
		}
		return nil
	})}, Options{})
	defer session.Close()
	list, err := session.LoadTools(context.Background())
	if err != nil || len(list) != 1 {
		t.Fatalf("tools=%d error=%v", len(list), err)
	}
	tool := list[0]
	if !tool.Annotations.ReadOnlyHint || !strings.Contains(string(tool.Raw), "customSpec") || !strings.Contains(string(tool.Annotations.Raw), "destructiveHint") {
		t.Fatal("tool projection lost complete wire data")
	}
	out, err := json.Marshal(tool)
	if err != nil {
		t.Fatal(err)
	}
	var before, after any
	_ = json.Unmarshal([]byte(remote), &before)
	_ = json.Unmarshal(out, &after)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("remote spec did not roundtrip: %s", out)
	}
	annotations, err := json.Marshal(tool.Annotations)
	if err != nil || !strings.Contains(string(annotations), `"custom":{"risk":7}`) {
		t.Fatalf("annotations=%s error=%v", annotations, err)
	}
}
