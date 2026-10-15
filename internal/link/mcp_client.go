package link

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	mcpProtocol = "2024-11-05"
	mcpNoteCap  = 32
	mcpPageCap  = 20
	mcpRPCWait  = 15 * time.Second
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
	conn      io.ReadWriteCloser
	wmu       sync.Mutex
	mu        sync.Mutex
	next      int64
	pending   map[string]chan rpcResult
	notes     chan string
	closeOnce sync.Once
	closeErr  error
}

func newMCPClient(conn io.ReadWriteCloser) *mcpClient {
	return &mcpClient{
		conn:    conn,
		next:    1,
		pending: map[string]chan rpcResult{},
		notes:   make(chan string, mcpNoteCap),
	}
}

func (c *mcpClient) close() error {
	c.closeOnce.Do(func() {
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

func (c *mcpClient) fail(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
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
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_, err = c.conn.Write(raw)
	return err
}

func (c *mcpClient) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	rawParams, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
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
	if err := c.write(rpcMessage{
		JSONRPC: "2.0",
		ID:      json.RawMessage(key),
		Method:  method,
		Params:  rawParams,
	}); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
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
	return c.notify("notifications/initialized", map[string]any{})
}

type mcpToolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	Annotations struct {
		ReadOnlyHint bool `json:"readOnlyHint"`
	} `json:"annotations"`
}

func (c *mcpClient) listTools(ctx context.Context) ([]mcpToolSpec, error) {
	var all []mcpToolSpec
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
			Tools      []mcpToolSpec `json:"tools"`
			NextCursor string        `json:"nextCursor"`
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
