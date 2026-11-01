package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	mcpProtocol    = "2024-11-05"
	mcpNoteCap     = 32
	mcpPageCap     = 20
	mcpRPCWait     = 15 * time.Second
	mcpControlWait = 250 * time.Millisecond
)

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string {
	if e == nil {
		return "mcp: error"
	}
	if e.Message == "" {
		return fmt.Sprintf("mcp: error %d", e.Code)
	}
	return "mcp: " + e.Message
}

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcResult struct {
	result json.RawMessage
	err    error
}

type mcpClient struct {
	conn           io.ReadWriteCloser
	ctx            context.Context
	frameGate      chan struct{}
	abortFn        func(error)
	abortOnce      sync.Once
	controls       sync.WaitGroup
	controlsClosed bool
	mu             sync.Mutex
	next           int64
	pending        map[string]chan rpcResult
	notes          chan string
	closeOnce      sync.Once
	closeErr       error
	terminalErr    error
}

func newMCPClient(conn io.ReadWriteCloser, ctx context.Context, abort func(error)) *mcpClient {
	return &mcpClient{
		conn:      conn,
		ctx:       ctx,
		frameGate: make(chan struct{}, 1),
		abortFn:   abort,
		next:      1,
		pending:   map[string]chan rpcResult{},
		notes:     make(chan string, mcpNoteCap),
	}
}

func (c *mcpClient) close() error {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.controlsClosed = true
		c.mu.Unlock()
		c.closeErr = c.conn.Close()
	})
	return c.closeErr
}

func (c *mcpClient) readLoop() {
	defer close(c.notes)
	sc := bufio.NewScanner(c.conn)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := append([]byte(nil), bytes.TrimSpace(sc.Bytes())...)
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var msg rpcMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}
		c.dispatch(msg)
	}
	err := sc.Err()
	if err == nil {
		err = io.EOF
	}
	c.fail(err)
}

func (c *mcpClient) failure() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.terminalErr
}

func (c *mcpClient) fail(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.terminalErr != nil {
		return
	}
	c.terminalErr = err
	for id, ch := range c.pending {
		select {
		case ch <- rpcResult{err: err}:
		default:
		}
		delete(c.pending, id)
	}
}

func (c *mcpClient) dispatch(msg rpcMessage) {
	if msg.Method != "" && !hasID(msg.ID) {
		c.pushNote(msg)
		return
	}
	if msg.Method != "" && hasID(msg.ID) {
		c.replyUnsupported(msg.ID)
		return
	}
	if !hasID(msg.ID) {
		return
	}
	res := rpcResult{result: append(json.RawMessage(nil), msg.Result...)}
	if msg.Error != nil {
		res.err = msg.Error
		res.result = nil
	}
	key := string(bytes.TrimSpace(msg.ID))
	c.mu.Lock()
	ch := c.pending[key]
	c.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- res:
	default:
	}
}

func (c *mcpClient) pushNote(msg rpcMessage) {
	switch msg.Method {
	case "notifications/xbot/event", "notifications/message":
	default:
		return
	}
	text := strings.TrimSpace(noteText(msg.Params))
	if text == "" {
		return
	}
	select {
	case c.notes <- text:
	default:
	}
}

func noteText(params json.RawMessage) string {
	if len(bytes.TrimSpace(params)) == 0 {
		return ""
	}
	var p struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return ""
	}
	return p.Text
}

func hasID(id json.RawMessage) bool {
	s := bytes.TrimSpace(id)
	return len(s) > 0 && !bytes.Equal(s, []byte("null"))
}

func (c *mcpClient) replyUnsupported(id json.RawMessage) {
	_ = c.write(rpcMessage{
		JSONRPC: "2.0",
		ID:      append(json.RawMessage(nil), bytes.TrimSpace(id)...),
		Error:   &rpcError{Code: -32601, Message: "method not found"},
	})
}

func (c *mcpClient) write(v any) error {
	ctx, cancel := context.WithTimeout(c.ctx, mcpControlWait)
	defer cancel()
	_, err := c.writeContext(ctx, v)
	return err
}

// writeContext reports whether the frame might have been sent. HTTP requests
// are independent; only stdio shares a complete-frame gate.
func (c *mcpClient) writeContext(ctx context.Context, v any) (bool, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return false, err
	}
	raw = append(raw, '\n')
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := c.ctx.Err(); err != nil {
		return false, err
	}
	if writer, ok := c.conn.(interface {
		WriteRequest(context.Context, []byte) (int, error)
	}); ok {
		_, err := writer.WriteRequest(ctx, raw)
		return true, err
	}
	select {
	case c.frameGate <- struct{}{}:
		defer func() { <-c.frameGate }()
	case <-ctx.Done():
		return false, ctx.Err()
	case <-c.ctx.Done():
		return false, c.ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := c.failure(); err != nil {
		return false, err
	}
	var n int
	if writer, ok := c.conn.(interface {
		WriteContext(context.Context, []byte) (int, error)
	}); ok {
		n, err = writer.WriteContext(ctx, raw)
	} else {
		n, err = writeWithContext(ctx, c.conn, raw, c.abortTransport)
	}
	if err == nil && n == len(raw) {
		return true, nil
	}
	var canceled *zeroWriteCanceled
	if n == 0 && errors.As(err, &canceled) && c.failure() == nil {
		return false, canceled.cause
	}
	if err == nil {
		err = io.ErrShortWrite
	}
	c.abortTransport(err)
	return n > 0, c.failure()
}

func (c *mcpClient) abortTransport(cause error) {
	c.abortOnce.Do(func() {
		err := fmt.Errorf("mcp: transport write failed; outcome_unknown: %w", cause)
		c.fail(err)
		c.abortFn(err)
	})
}

func (c *mcpClient) cancelHint(id json.RawMessage) {
	c.mu.Lock()
	if c.controlsClosed || c.terminalErr != nil || c.ctx.Err() != nil {
		c.mu.Unlock()
		return
	}
	c.controls.Add(1)
	c.mu.Unlock()
	go func() {
		defer c.controls.Done()
		ctx, cancel := context.WithTimeout(c.ctx, mcpControlWait)
		defer cancel()
		params, _ := json.Marshal(map[string]any{"requestId": id})
		_, _ = c.writeContext(ctx, rpcMessage{JSONRPC: "2.0", Method: "notifications/cancelled", Params: params})
	}()
}

func (c *mcpClient) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	rawParams, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	if c.terminalErr != nil {
		err := c.terminalErr
		c.mu.Unlock()
		return nil, err
	}
	id := c.next
	c.next++
	key := strconv.FormatInt(id, 10)
	ch := make(chan rpcResult, 1)
	c.pending[key] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, key)
		c.mu.Unlock()
	}()
	sent, err := c.writeContext(ctx, rpcMessage{
		JSONRPC: "2.0", ID: json.RawMessage(key), Method: method, Params: rawParams,
	})
	if err != nil {
		if sent && ctx.Err() != nil && method == "tools/call" {
			c.cancelHint(json.RawMessage(key))
		}
		return nil, err
	}
	select {
	case <-ctx.Done():
		if method == "tools/call" {
			c.cancelHint(json.RawMessage(key))
		}
		return nil, ctx.Err()
	case res := <-ch:
		if res.err != nil {
			return nil, res.err
		}
		return res.result, nil
	}
}

func (c *mcpClient) notify(method string, params any) error {
	rawParams, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return c.write(rpcMessage{JSONRPC: "2.0", Method: method, Params: rawParams})
}

func (c *mcpClient) initialize(ctx context.Context) error {
	if _, err := c.call(ctx, "initialize", map[string]any{
		"protocolVersion": mcpProtocol,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "xbot", "version": "0"},
	}); err != nil {
		return err
	}
	_, err := c.writeContext(ctx, rpcMessage{JSONRPC: "2.0", Method: "notifications/initialized", Params: json.RawMessage(`{}`)})
	return err
}

// ToolSpec keeps the complete remote definition alongside the typed fields
// used by the legacy model adapter. Marshaling a received spec preserves its
// original fields, including extensions unknown to this client.
type ToolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	Annotations ToolAnnotations `json:"annotations"`
	Raw         json.RawMessage `json:"-"`
}

type ToolAnnotations struct {
	ReadOnlyHint bool            `json:"readOnlyHint"`
	Raw          json.RawMessage `json:"-"`
}

func (t *ToolSpec) UnmarshalJSON(raw []byte) error {
	type fields ToolSpec
	var value fields
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	*t = ToolSpec(value)
	t.Raw = append(json.RawMessage(nil), raw...)
	return nil
}

func (t ToolSpec) MarshalJSON() ([]byte, error) {
	if len(t.Raw) != 0 {
		return t.Raw, nil
	}
	type fields ToolSpec
	return json.Marshal(fields(t))
}

func (a *ToolAnnotations) UnmarshalJSON(raw []byte) error {
	type fields ToolAnnotations
	var value fields
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	*a = ToolAnnotations(value)
	a.Raw = append(json.RawMessage(nil), raw...)
	return nil
}

func (a ToolAnnotations) MarshalJSON() ([]byte, error) {
	if len(a.Raw) != 0 {
		return a.Raw, nil
	}
	type fields ToolAnnotations
	return json.Marshal(fields(a))
}

func (c *mcpClient) listTools(ctx context.Context) ([]ToolSpec, error) {
	var all []ToolSpec
	cursor := ""
	for page := 0; page < mcpPageCap; page++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, err := c.call(ctx, "tools/list", params)
		if err != nil {
			return nil, err
		}
		var res struct {
			Tools      []ToolSpec `json:"tools"`
			NextCursor string     `json:"nextCursor"`
		}
		if len(bytes.TrimSpace(raw)) > 0 && string(raw) != "null" {
			if err := json.Unmarshal(raw, &res); err != nil {
				return nil, err
			}
		}
		all = append(all, res.Tools...)
		if res.NextCursor == "" || res.NextCursor == cursor {
			return all, nil
		}
		cursor = res.NextCursor
	}
	return all, nil
}
