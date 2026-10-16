package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client calls an OpenAI-compatible /chat/completions endpoint.
type Client struct {
	BaseURL    string
	APIKey     string
	Model      string
	HTTPClient *http.Client
	// retryWait is the first backoff. The second retry waits twice as long.
	// Zero uses the default. Tests set a short wait.
	retryWait time.Duration
	// RequestTimeout caps one attempt, from the request going out to its last
	// byte. Zero uses defaultRequestTimeout.
	RequestTimeout time.Duration
	// StreamIdleTimeout caps the gap between two chunks of one attempt: a
	// stream that stops sending without closing is cut off and retried rather
	// than waiting forever. Zero uses defaultStreamIdleTimeout; a negative
	// value turns the check off.
	StreamIdleTimeout time.Duration
}

var defaultHTTP = &http.Client{
	Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ResponseHeaderTimeout: 90 * time.Second,
	},
}

const (
	// defaultRequestTimeout caps one whole attempt. A request that hangs for
	// minutes without ever erroring is what this bounds.
	defaultRequestTimeout = 3 * time.Minute
	// defaultStreamIdleTimeout caps the gap between two stream chunks, so a
	// stalled connection is retried long before the whole-request timeout.
	defaultStreamIdleTimeout = 90 * time.Second
)

type apiTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Parameters  any    `json:"parameters"`
	} `json:"function"`
}

type apiRequest struct {
	Model      string    `json:"model"`
	Messages   []Message `json:"messages"`
	Tools      []apiTool `json:"tools,omitempty"`
	ToolChoice string    `json:"tool_choice,omitempty"`
	Stream     bool      `json:"stream"`
}

// Chat streams a completion and returns the assembled assistant message.
// Each attempt is bounded twice: RequestTimeout covers the whole request from
// the call going out to its last byte, StreamIdleTimeout covers the gap between
// two chunks. Either timeout is retried like unexpected EOF, HTTP 429, and HTTP
// 5xx, twice, with backoff.
// Deltas are delivered only after an attempt succeeds, so a retry does not repeat them.
func (c *Client) Chat(ctx context.Context, req Request, onDelta func(string)) (Message, error) {
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			if err := c.pause(ctx, attempt); err != nil {
				return Message{}, err
			}
		}
		var deltas []string
		msg, err := c.chatAttempt(ctx, req, func(s string) { deltas = append(deltas, s) })
		if err == nil {
			if onDelta != nil {
				for _, s := range deltas {
					onDelta(s)
				}
			}
			return msg, nil
		}
		last = err
		if !retryable(err) {
			return Message{}, err
		}
	}
	return Message{}, last
}

// requestTimeout is the whole-attempt budget in effect.
func (c *Client) requestTimeout() time.Duration {
	if c.RequestTimeout != 0 {
		return c.RequestTimeout
	}
	return defaultRequestTimeout
}

// streamIdleTimeout is the between-chunks budget in effect. A negative value
// disables it.
func (c *Client) streamIdleTimeout() time.Duration {
	if c.StreamIdleTimeout != 0 {
		return c.StreamIdleTimeout
	}
	return defaultStreamIdleTimeout
}

// chatAttempt bounds one attempt with the whole-request timeout and reports a
// timeout as ErrRequestTimeout, which retryable accepts.
func (c *Client) chatAttempt(ctx context.Context, req Request, onDelta func(string)) (Message, error) {
	ctx, cancel := context.WithTimeout(ctx, c.requestTimeout())
	defer cancel()
	msg, err := c.chatOnce(ctx, req, onDelta, c.streamIdleTimeout())
	if err != nil && ctx.Err() != nil && ctx.Err() == context.DeadlineExceeded {
		// The whole-request budget expired. Say so, instead of the bare
		// "context deadline exceeded" the transport returns, so the retry and
		// the report name the real cause.
		return Message{}, fmt.Errorf("%w after %s: %v", ErrRequestTimeout, c.requestTimeout(), err)
	}
	return msg, err
}

func (c *Client) pause(ctx context.Context, attempt int) error {
	wait := c.retryWait
	if wait <= 0 {
		wait = 200 * time.Millisecond
	}
	wait *= time.Duration(attempt)
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// ErrRequestTimeout says one attempt used up its whole-request budget.
var ErrRequestTimeout = errors.New("provider: request timeout")

// ErrStreamIdleTimeout says one attempt went too long without a new chunk.
var ErrStreamIdleTimeout = errors.New("provider: stream idle timeout")

// idleReader fails a read that waits longer than the idle budget for data, and
// cancels the attempt so the stalled connection is torn down. A budget of zero
// or less reads straight through.
type idleReader struct {
	r      io.Reader
	idle   time.Duration
	cancel context.CancelFunc
}

func (rd *idleReader) Read(p []byte) (int, error) {
	if rd.idle <= 0 {
		return rd.r.Read(p)
	}
	type result struct {
		n   int
		err error
	}
	ch := make(chan result, 1)
	buf := make([]byte, len(p))
	go func() {
		n, err := rd.r.Read(buf)
		ch <- result{n, err}
	}()
	timer := time.NewTimer(rd.idle)
	defer timer.Stop()
	select {
	case r := <-ch:
		copy(p, buf[:r.n])
		return r.n, r.err
	case <-timer.C:
		rd.cancel()
		return 0, ErrStreamIdleTimeout
	}
}

func retryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrRequestTimeout) || errors.Is(err, ErrStreamIdleTimeout) {
		return true
	}
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		return true
	}
	if strings.Contains(err.Error(), "unexpected EOF") {
		return true
	}
	var status interface{ StatusCode() int }
	if errors.As(err, &status) {
		code := status.StatusCode()
		return code == http.StatusTooManyRequests || code >= 500
	}
	return false
}

type statusError struct {
	code int
	body string
}

func (e *statusError) Error() string {
	return fmt.Sprintf("provider: http %d: %s", e.code, e.body)
}

func (e *statusError) StatusCode() int { return e.code }

func (c *Client) chatOnce(ctx context.Context, req Request, onDelta func(string), idle time.Duration) (Message, error) {
	if strings.TrimSpace(c.APIKey) == "" {
		return Message{}, errors.New("provider: API key is empty")
	}
	if strings.TrimSpace(c.Model) == "" {
		return Message{}, errors.New("provider: model is empty")
	}
	if strings.TrimSpace(c.BaseURL) == "" {
		return Message{}, errors.New("provider: base URL is empty")
	}
	body := apiRequest{
		Model:    c.Model,
		Messages: req.Messages,
		Stream:   true,
	}
	if len(req.Tools) > 0 {
		body.ToolChoice = "auto"
		for _, t := range req.Tools {
			var at apiTool
			at.Type = "function"
			at.Function.Name = t.Name
			at.Function.Description = t.Description
			at.Function.Parameters = t.Parameters
			body.Tools = append(body.Tools, at)
		}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return Message{}, err
	}
	// streamCtx is canceled when the stream goes idle, which tears down the
	// connection instead of leaving the read blocked on it.
	streamCtx, cancelStream := context.WithCancel(ctx)
	defer cancelStream()
	httpReq, err := http.NewRequestWithContext(streamCtx, http.MethodPost, c.endpoint(), bytes.NewReader(raw))
	if err != nil {
		return Message{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("User-Agent", "xbot/m1")

	resp, err := c.http().Do(httpReq)
	if err != nil {
		return Message{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return Message{}, &statusError{code: resp.StatusCode, body: strings.TrimSpace(string(b))}
	}
	stream := io.ReadCloser(resp.Body)
	if idle > 0 {
		stream = io.NopCloser(&idleReader{r: resp.Body, idle: idle, cancel: cancelStream})
	}
	msg, err := decodeStream(stream, onDelta)
	if err != nil {
		return Message{}, err
	}
	if msg.Role == "" {
		msg.Role = RoleAssistant
	}
	if msg.Content == "" && len(msg.ToolCalls) == 0 {
		return Message{}, errors.New("provider: empty completion")
	}
	return msg, nil
}

func (c *Client) endpoint() string {
	return strings.TrimRight(c.BaseURL, "/") + "/chat/completions"
}

func (c *Client) http() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return defaultHTTP
}

type streamChunk struct {
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
	Choices []struct {
		Delta struct {
			Role      string `json:"role"`
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
	} `json:"choices"`
}

type toolBuf struct {
	id, name, typ string
	args          strings.Builder
}

func decodeStream(r io.Reader, onDelta func(string)) (Message, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var content strings.Builder
	role := RoleAssistant
	tools := map[int]*toolBuf{}
	var order []int
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var chunk streamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return Message{}, fmt.Errorf("provider: stream: %w", err)
		}
		if chunk.Error != nil && chunk.Error.Message != "" {
			return Message{}, fmt.Errorf("provider: %s", chunk.Error.Message)
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		d := chunk.Choices[0].Delta
		if d.Role != "" {
			role = d.Role
		}
		if d.Content != "" {
			content.WriteString(d.Content)
			if onDelta != nil {
				onDelta(d.Content)
			}
		}
		for _, tc := range d.ToolCalls {
			buf, ok := tools[tc.Index]
			if !ok {
				buf = &toolBuf{}
				tools[tc.Index] = buf
				order = append(order, tc.Index)
			}
			if tc.ID != "" {
				buf.id = tc.ID
			}
			if tc.Type != "" {
				buf.typ = tc.Type
			}
			if tc.Function.Name != "" {
				buf.name += tc.Function.Name
			}
			if tc.Function.Arguments != "" {
				buf.args.WriteString(tc.Function.Arguments)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return Message{}, err
	}
	msg := Message{Role: role, Content: content.String()}
	for i, idx := range order {
		buf := tools[idx]
		id := buf.id
		if id == "" {
			id = fmt.Sprintf("call_local_%d", i+1)
		}
		typ := buf.typ
		if typ == "" {
			typ = "function"
		}
		msg.ToolCalls = append(msg.ToolCalls, ToolCall{
			ID:   id,
			Type: typ,
			Function: FunctionCall{
				Name:      buf.name,
				Arguments: buf.args.String(),
			},
		})
	}
	return msg, nil
}
