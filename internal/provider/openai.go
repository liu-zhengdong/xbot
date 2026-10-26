package provider

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
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
	// Thinking is the per-model thinking switch: ThinkingOn sends
	// {"thinking":{"type":"enabled"}} and replays reasoning_content,
	// ThinkingOff sends {"thinking":{"type":"disabled"}}, ThinkingOmit sends
	// nothing, and empty picks by model name (see thinkingFor).
	Thinking string
	// ReasoningEffort defaults to low; supported configured levels are low/high/max.
	ReasoningEffort string
}

const (
	// ThinkingOn asks the endpoint to think and sends its reasoning back on
	// later requests, as DeepSeek requires on tool turns.
	ThinkingOn = "on"
	// ThinkingOff asks the endpoint to skip its thinking phase.
	ThinkingOff = "off"
	// ThinkingOmit leaves the thinking field out of the request.
	ThinkingOmit = "omit"
)

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
	Model           string         `json:"model"`
	Messages        []wireMessage  `json:"messages"`
	Tools           []apiTool      `json:"tools,omitempty"`
	ToolChoice      string         `json:"tool_choice,omitempty"`
	Stream          bool           `json:"stream"`
	StreamOptions   *streamOptions `json:"stream_options,omitempty"`
	Thinking        *thinkingParam `json:"thinking,omitempty"`
	ReasoningEffort string         `json:"reasoning_effort,omitempty"`
}

// wireMessage is the Chat Completions message. Content is a string when the
// message is text, and an array of text and image_url parts when it carries
// images. DeepSeek flash accepts that array; a model without vision never
// receives one because view_image refuses first.
type wireMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content,omitempty"`
	Name       string          `json:"name,omitempty"`
	ToolCalls  []ToolCall      `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	// ReasoningContent replays the assistant's own reasoning while thinking
	// is on. Nil leaves the field out, so other requests never carry it.
	ReasoningContent *string `json:"reasoning_content,omitempty"`
}

type textPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type imagePart struct {
	Type     string `json:"type"`
	ImageURL struct {
		URL string `json:"url"`
	} `json:"image_url"`
}

// streamOptions asks the endpoint to include a usage object on the last chunk.
// Endpoints that omit it leave TokenUsage.Reported false.
type thinkingParam struct {
	Type string `json:"type"`
}

// thinkingFor returns the thinking field for one request, or nil to leave it
// out. DeepSeek models think by default and require reasoning_content back on
// tool turns, which XBot replays whenever the field says enabled. So any
// DeepSeek model gets thinking turned on unless the model spec says off or
// omit. Other endpoints may reject the unknown field, so they get nothing
// unless the spec says on or off. deepseek-reasoner is the legacy
// thinking-only id and is left alone.
func thinkingFor(model, setting string) *thinkingParam {
	switch setting {
	case ThinkingOn:
		return &thinkingParam{Type: "enabled"}
	case ThinkingOff:
		return &thinkingParam{Type: "disabled"}
	case ThinkingOmit:
		return nil
	}
	m := strings.ToLower(model)
	if strings.Contains(m, "deepseek") && !strings.Contains(m, "reasoner") {
		return &thinkingParam{Type: "enabled"}
	}
	return nil
}

// completionsReasoning is the VendorRaw of a Chat Completions reasoning part.
type completionsReasoning struct {
	ReasoningContent string `json:"reasoning_content"`
}

// reasoningPart keeps the full reasoning for replay in VendorRaw. Text is only
// a marker: Text() leaves reasoning out, so it never reaches a bubble.
func reasoningPart(text string) ContentPart {
	raw, _ := json.Marshal(completionsReasoning{ReasoningContent: text})
	return ContentPart{Type: PartReasoning, Vendor: ChatCompletions, VendorRaw: raw, Text: "[thinking]"}
}

// replayReasoning returns this message's own Completions reasoning, and
// whether it has any.
func replayReasoning(m Message) (string, bool) {
	var out strings.Builder
	found := false
	for _, p := range m.Content {
		if p.Type != PartReasoning || p.Vendor != ChatCompletions {
			continue
		}
		var r completionsReasoning
		if json.Unmarshal(p.VendorRaw, &r) == nil {
			out.WriteString(r.ReasoningContent)
			found = true
		}
	}
	return out.String(), found
}

// reasoningEffort keeps unset model specs and environment-only bindings on low.
func reasoningEffort(setting string) string {
	if setting == "" {
		return "low"
	}
	return setting
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// wireMessages encodes the request. With thinking on, each assistant message
// carries its own reasoning_content; one with tool calls but no recorded
// reasoning (older history, another vendor) carries an empty one, because
// DeepSeek rejects a tool turn without the field.
func wireMessages(in []Message, thinking bool) ([]wireMessage, error) {
	out := make([]wireMessage, len(in))
	for i, m := range in {
		w, err := toWire(m, thinking)
		if err != nil {
			return nil, err
		}
		out[i] = w
	}
	return out, nil
}

func toWire(m Message, thinking bool) (wireMessage, error) {
	w := wireMessage{
		Role: m.Role, Name: m.Name, ToolCalls: m.Calls(), ToolCallID: m.CallID(),
	}
	reasoning, found := replayReasoning(m)
	if found {
		// Our own reasoning goes in its field, never as a content marker.
		m.Content = withoutCompletionsReasoning(m.Content)
	}
	if thinking && m.Role == RoleAssistant && (found || len(w.ToolCalls) > 0) {
		w.ReasoningContent = &reasoning
	}
	raw, err := contentJSON(m)
	if err != nil {
		return wireMessage{}, err
	}
	w.Content = raw
	return w, nil
}

func withoutCompletionsReasoning(in []ContentPart) []ContentPart {
	out := make([]ContentPart, 0, len(in))
	for _, p := range in {
		if p.Type != PartReasoning || p.Vendor != ChatCompletions {
			out = append(out, p)
		}
	}
	return out
}

func contentJSON(m Message) (json.RawMessage, error) {
	if len(m.Images) == 0 {
		if m.PortableText() == "" {
			return nil, nil
		}
		return json.Marshal(m.PortableText())
	}
	parts := make([]any, 0, 1+len(m.Images))
	if m.PortableText() != "" {
		parts = append(parts, textPart{Type: "text", Text: m.PortableText()})
	}
	for _, img := range m.Images {
		mime := img.MIME
		if mime == "" {
			mime = "image/png"
		}
		var p imagePart
		p.Type = "image_url"
		p.ImageURL.URL = "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(img.Data)
		parts = append(parts, p)
	}
	return json.Marshal(parts)
}

// Chat streams a completion and returns the assembled assistant message.
// Each attempt is bounded twice: RequestTimeout covers the whole request from
// the call going out to its last byte, StreamIdleTimeout covers the gap between
// two chunks. Either timeout is retried like unexpected EOF, HTTP 429, and HTTP
// 5xx, twice, with backoff.
// Deltas go out as they arrive, so the first words of a long answer reach the
// owner before the last is written. A failed attempt is retried only while
// nothing has gone out yet; once text has been streamed, a retry would repeat
// or contradict it, so the error is returned instead.
func (c *Client) Chat(ctx context.Context, req Request, onDelta func(string)) (Message, error) {
	return c.complete(ctx, req, onDelta, c.chatAttempt)
}

func (c *Client) complete(ctx context.Context, req Request, onDelta func(string), attemptCall func(context.Context, Request, func(string)) (Message, error)) (Message, error) {
	var last error
	streamed := false
	live := func(s string) {
		if s != "" {
			streamed = true
		}
		if onDelta != nil {
			onDelta(s)
		}
	}
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			if err := c.pause(ctx, attempt); err != nil {
				return Message{}, err
			}
		}
		msg, err := attemptCall(ctx, req, live)
		if err == nil {
			return msg, nil
		}
		last = err
		if streamed || !retryable(err) {
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
	var visible *visibleResponseError
	if errors.As(err, &visible) {
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
	thinking := thinkingFor(c.Model, c.Thinking)
	// omit leaves the upstream default alone; DeepSeek still thinks by
	// default (including the legacy reasoner) and needs its history replayed.
	replay := thinking != nil && thinking.Type == "enabled" ||
		thinking == nil && strings.Contains(strings.ToLower(c.Model), "deepseek")
	msgs, err := wireMessages(req.Messages, replay)
	if err != nil {
		return Message{}, err
	}
	body := apiRequest{
		Model:         c.Model,
		Messages:      msgs,
		Stream:        true,
		StreamOptions: &streamOptions{IncludeUsage: true},
		Thinking:      thinking,
	}
	if replay {
		body.ReasoningEffort = reasoningEffort(c.ReasoningEffort)
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
	if msg.Text() == "" && len(msg.Calls()) == 0 {
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
			Role             string `json:"role"`
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
			ToolCalls        []struct {
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
	Usage *apiUsage `json:"usage,omitempty"`
}

// apiUsage is the OpenAI-compatible usage object. Cached tokens are read from
// prompt_tokens_details.cached_tokens, or from prompt_cache_hit_tokens when
// that is what the endpoint sends. A present object is real; a missing one is not.
type apiUsage struct {
	PromptTokens     int  `json:"prompt_tokens"`
	CompletionTokens int  `json:"completion_tokens"`
	PromptCacheHit   *int `json:"prompt_cache_hit_tokens"`
	PromptDetails    *struct {
		CachedTokens *int `json:"cached_tokens"`
	} `json:"prompt_tokens_details,omitempty"`
}

func (u apiUsage) tokenUsage() TokenUsage {
	cached, present := 0, false
	if u.PromptCacheHit != nil {
		cached, present = *u.PromptCacheHit, true
	}
	if u.PromptDetails != nil && u.PromptDetails.CachedTokens != nil {
		present = true
		if *u.PromptDetails.CachedTokens > 0 {
			cached = *u.PromptDetails.CachedTokens
		}
	}
	return TokenUsage{
		PromptTokens:     u.PromptTokens,
		CompletionTokens: u.CompletionTokens,
		CachedTokens:     cached,
		Reported:         true,
		CacheReported:    present,
	}
}

type toolBuf struct {
	id, name, typ string
	part          int
	args          strings.Builder
}

func decodeStream(r io.Reader, onDelta func(string)) (Message, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var parts []ContentPart
	var text, reasoning strings.Builder
	flushText := func() {
		if text.Len() > 0 {
			parts = append(parts, ContentPart{Type: PartText, Text: text.String()})
			text = strings.Builder{}
		}
	}
	role := RoleAssistant
	tools := map[int]*toolBuf{}
	var order []int
	var usage TokenUsage
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
		if chunk.Usage != nil {
			usage = chunk.Usage.tokenUsage()
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		d := chunk.Choices[0].Delta
		if d.Role != "" {
			role = d.Role
		}
		// Reasoning is kept for replay only. It never goes to onDelta, so the
		// owner's live stream shows the answer alone.
		if reasoning.Len()+len(d.ReasoningContent) > 1<<20 {
			return Message{}, errors.New("provider: reasoning exceeds 1 MiB")
		}
		reasoning.WriteString(d.ReasoningContent)
		if d.Content != "" {
			text.WriteString(d.Content)
			if onDelta != nil {
				onDelta(d.Content)
			}
		}
		for _, tc := range d.ToolCalls {
			buf, ok := tools[tc.Index]
			if !ok {
				flushText()
				buf = &toolBuf{part: len(parts)}
				parts = append(parts, ContentPart{Type: PartToolCall})
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
	flushText()
	if reasoning.Len() > 0 {
		parts = append([]ContentPart{reasoningPart(reasoning.String())}, parts...)
		for _, idx := range order {
			tools[idx].part++
		}
	}
	msg := Message{Role: role, Usage: usage, Content: parts}
	for i, idx := range order {
		buf := tools[idx]
		id := buf.id
		if id == "" {
			id = fmt.Sprintf("call_local_%s_%d", localCallNonce(), i+1)
		}
		typ := buf.typ
		if typ == "" {
			typ = "function"
		}
		msg.Content[buf.part].ToolCall = &ToolCall{
			ID:   id,
			Type: typ,
			Function: FunctionCall{
				Name:      buf.name,
				Arguments: buf.args.String(),
			},
		}
	}
	return msg, nil
}

// localCallNonce makes a fallback tool call id unique across turns. A fixed
// "call_local_1" repeated in later turns let a result pair with the wrong call.
func localCallNonce() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(b[:])
}

// overflowPhrases are what OpenAI-compatible APIs put in the body when the
// request is longer than the model's window (OpenAI, DeepSeek, vLLM, and the
// gateways in front of them).
var overflowPhrases = []string{
	"context_length_exceeded",
	"maximum context length",
	"context length",
	"context window",
	"prompt is too long",
	"too many tokens",
	"input is too long",
}

// IsContextOverflow reports whether err is the provider rejecting a request
// for being longer than the model's window. The caller can shrink the request
// and send it once more; retryable does not, because the same request would
// fail again.
func IsContextOverflow(err error) bool {
	var se *statusError
	if !errors.As(err, &se) {
		return false
	}
	if se.code != http.StatusBadRequest && se.code != http.StatusRequestEntityTooLarge {
		return false
	}
	if se.code == http.StatusRequestEntityTooLarge {
		return true
	}
	body := strings.ToLower(se.body)
	for _, p := range overflowPhrases {
		if strings.Contains(body, p) {
			return true
		}
	}
	return false
}

// NewStatusError builds the error a non-2xx response produces, for fakes that
// stand in for a real API in tests.
func NewStatusError(code int, body string) error {
	return &statusError{code: code, body: body}
}
