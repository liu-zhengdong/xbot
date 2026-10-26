package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// Responses speaks the native Responses protocol. Hosted calls are output
// items, never local ToolCalls, so they cannot enter the tool executor or Jobs.
type Responses struct {
	Client
	WebSearch bool
}

func (c *Responses) Chat(ctx context.Context, req Request, delta func(string)) (Message, error) {
	visible := false
	observer, _ := ctx.Value(partObserverKey{}).(func(ContentPart))
	ctx = WithPartObserver(ctx, func(p ContentPart) {
		visible = true
		if observer != nil {
			observer(p)
		}
	})
	return c.complete(ctx, req, delta, func(ctx context.Context, r Request, d func(string)) (Message, error) {
		m, err := c.responseAttempt(ctx, r, d)
		if err != nil && visible {
			return m, &visibleResponseError{err}
		}
		return m, err
	})
}

func responseInput(messages []Message) ([]json.RawMessage, error) {
	out := []json.RawMessage{}
	add := func(v any) error {
		b, e := json.Marshal(v)
		if e == nil {
			out = append(out, b)
		}
		return e
	}
	for _, m := range messages {
		// Preserve block order, flushing portable text before native items/calls.
		text := ""
		flush := func() error {
			if text == "" {
				return nil
			}
			role := m.Role
			if role == RoleTool {
				role = RoleUser
			}
			err := add(map[string]any{"role": role, "content": text})
			text = ""
			return err
		}
		for _, p := range m.Content {
			if p.Vendor == "openai_responses" && len(p.VendorRaw) > 0 {
				if err := flush(); err != nil {
					return nil, err
				}
				out = append(out, p.VendorRaw)
				continue
			}
			switch p.Type {
			case PartToolCall:
				if err := flush(); err != nil {
					return nil, err
				}
				if p.ToolCall == nil {
					return nil, errors.New("responses: empty tool call")
				}
				t := p.ToolCall
				if err := add(map[string]any{"type": "function_call", "call_id": t.ID, "name": t.Function.Name, "arguments": t.Function.Arguments}); err != nil {
					return nil, err
				}
			case PartToolResult:
				if err := flush(); err != nil {
					return nil, err
				}
				if err := add(map[string]any{"type": "function_call_output", "call_id": p.ToolCallID, "output": p.Text}); err != nil {
					return nil, err
				}
			default:
				if p.Type == PartCitation && p.Vendor == "openai_responses" {
					continue
				}
				text += (Message{Content: []ContentPart{p}}).PortableText()
			}
		}
		if len(m.Images) > 0 {
			parts := []any{}
			if text != "" {
				parts = append(parts, map[string]any{"type": "input_text", "text": text})
			}
			text = ""
			for _, img := range m.Images {
				mime := img.MIME
				if mime == "" {
					mime = "image/png"
				}
				parts = append(parts, map[string]any{"type": "input_image", "image_url": "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(img.Data)})
			}
			role := m.Role
			if role == RoleTool {
				role = RoleUser
			}
			if err := add(map[string]any{"role": role, "content": parts}); err != nil {
				return nil, err
			}
		}
		if err := flush(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (c *Responses) responseAttempt(ctx context.Context, req Request, delta func(string)) (Message, error) {
	ctx, cancel := context.WithTimeout(ctx, c.requestTimeout())
	defer cancel()
	msg, err := c.responseOnce(ctx, req, delta)
	if err != nil && ctx.Err() == context.DeadlineExceeded {
		return Message{}, fmt.Errorf("%w: %v", ErrRequestTimeout, err)
	}
	return msg, err
}

func (c *Responses) responseOnce(ctx context.Context, req Request, delta func(string)) (Message, error) {
	if strings.TrimSpace(c.APIKey) == "" || strings.TrimSpace(c.Model) == "" || strings.TrimSpace(c.BaseURL) == "" {
		return Message{}, errors.New("responses: endpoint, model and API key are required")
	}
	input, err := responseInput(req.Messages)
	if err != nil {
		return Message{}, err
	}
	ts := []any{}
	for _, t := range req.Tools {
		if c.WebSearch && hostedSearchAllowed(ctx) && t.Name == "web_search" {
			return Message{}, errors.New("responses: hosted search cannot coexist with local web_search")
		}
		ts = append(ts, map[string]any{"type": "function", "name": t.Name, "description": t.Description, "parameters": t.Parameters, "strict": false})
	}
	if c.WebSearch && hostedSearchAllowed(ctx) && (req.HostedSearch || len(req.Tools) > 0) {
		ts = append(ts, map[string]any{"type": "web_search"})
		if xaiEndpoint(c.BaseURL) {
			// xAI's Responses endpoint also offers these hosted tools; mounting
			// them does not add local tools or permissions.
			ts = append(ts, map[string]any{"type": "x_search"}, map[string]any{"type": "code_interpreter"})
		}
	}
	body := map[string]any{"model": c.Model, "input": input, "tools": ts, "stream": true, "store": false, "include": []string{"reasoning.encrypted_content"}}
	raw, err := json.Marshal(body)
	if err != nil {
		return Message{}, err
	}
	// Model requests share the existing multimodal/context budgets. The 1 MiB
	// node-call cap is not a cap on this model API (Completions has none either).
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	endpoint := strings.TrimRight(c.BaseURL, "/")
	if !strings.HasSuffix(endpoint, "/responses") {
		endpoint += "/responses"
	}
	r, err := http.NewRequestWithContext(streamCtx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return Message{}, err
	}
	r.Header.Set("Authorization", "Bearer "+c.APIKey)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "text/event-stream")
	h := c.HTTPClient
	if h == nil {
		h = defaultHTTP
	}
	res, err := h.Do(r)
	if err != nil {
		return Message{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return Message{}, &statusError{code: res.StatusCode, body: strings.ReplaceAll(string(b), c.APIKey, "[secret]")}
	}
	msg, err := readResponses(ctx, &idleReader{r: res.Body, idle: c.streamIdleTimeout(), cancel: cancel}, delta)
	var status *statusError
	if errors.As(err, &status) {
		return msg, &statusError{code: status.code, body: strings.ReplaceAll(status.body, c.APIKey, "[secret]")}
	}
	return msg, err
}

// jsonQueryText picks a human-readable query out of JSON tool arguments, for
// custom calls whose parameters arrive as a JSON string.
func jsonQueryText(fields ...string) string {
	for _, raw := range fields {
		if raw == "" || !json.Valid([]byte(raw)) {
			continue
		}
		var v map[string]any
		if json.Unmarshal([]byte(raw), &v) != nil {
			continue
		}
		if q, ok := v["query"].(string); ok && q != "" {
			return q
		}
	}
	return ""
}

type responseItem struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	Status    string `json:"status"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Input     string `json:"input"`
	Action    struct {
		Query   string   `json:"query"`
		Queries []string `json:"queries"`
		Type    string   `json:"type"`
		URL     string   `json:"url"`
	} `json:"action"`
	Content []struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		Refusal     string `json:"refusal"`
		Annotations []struct {
			Type  string `json:"type"`
			URL   string `json:"url"`
			Title string `json:"title"`
			Start int    `json:"start_index"`
			End   int    `json:"end_index"`
		} `json:"annotations"`
	} `json:"content"`
}

// xaiEndpoint reports an xAI Responses endpoint, whose hosted tool set adds
// x_search and code_interpreter to web_search.
func xaiEndpoint(base string) bool {
	u, err := url.Parse(strings.TrimSpace(base))
	return err == nil && strings.EqualFold(u.Hostname(), "api.x.ai")
}

func responseParts(raw json.RawMessage) ([]ContentPart, error) {
	var it responseItem
	if err := json.Unmarshal(raw, &it); err != nil {
		return nil, err
	}
	if it.Type == "" {
		return nil, errors.New("responses: output item has no type")
	}
	p := ContentPart{Vendor: "openai_responses", VendorRaw: raw}
	switch it.Type {
	case "function_call":
		if it.CallID == "" || it.Name == "" || !json.Valid([]byte(it.Arguments)) {
			return nil, errors.New("responses: malformed function_call")
		}
		p.Type = PartToolCall
		p.ToolCall = &ToolCall{ID: it.CallID, Type: "function", Function: FunctionCall{Name: it.Name, Arguments: it.Arguments}}
	case "web_search_call":
		p.Type = PartHosted
		q := it.Action.Query
		if q == "" {
			q = strings.Join(it.Action.Queries, " · ")
		}
		if q == "" {
			q = it.Action.URL
		}
		p.Hosted = &HostedStep{ID: it.ID, Name: "web_search", Status: it.Status, Query: q}
		p.Text = "[厂商搜索: " + q + " · " + it.Status + "]"
	case "x_search_call":
		p.Type = PartHosted
		q := it.Action.Query
		if q == "" {
			q = strings.Join(it.Action.Queries, " · ")
		}
		p.Hosted = &HostedStep{ID: it.ID, Name: "x_search", Status: it.Status, Query: q}
		p.Text = "[X 搜索: " + q + " · " + it.Status + "]"
	case "code_interpreter_call":
		p.Type = PartHosted
		p.Hosted = &HostedStep{ID: it.ID, Name: "code_interpreter", Status: it.Status}
		p.Text = "[代码解释器: " + it.Status + "]"
	case "custom_tool_call":
		// xAI's X search surfaces as custom tool calls named after the real
		// function (x_keyword_search and friends); the xs_ call-id prefix marks
		// them (xAI docs, Tool Usage Details). Other custom calls stay unknown.
		id := it.CallID
		if id == "" {
			id = it.ID
		}
		if strings.HasPrefix(id, "xs_") {
			q := it.Action.Query
			if q == "" {
				q = strings.Join(it.Action.Queries, " · ")
			}
			if q == "" {
				q = jsonQueryText(it.Arguments, it.Input)
			}
			p.Type = PartHosted
			p.Hosted = &HostedStep{ID: id, Name: "x_search", Status: it.Status, Query: q}
			p.Text = "[X 搜索: " + q + " · " + it.Status + "]"
		} else {
			p.Type = it.Type
			p.Text = "[OpenAI " + it.Type + "]"
		}
	case "message":
		p.Type = PartText
		var cites []ContentPart
		for _, c := range it.Content {
			offset := len([]rune(p.Text))
			p.Text += c.Text + c.Refusal
			for _, a := range c.Annotations {
				if a.Type == "url_citation" {
					cites = append(cites, ContentPart{Type: PartCitation, Vendor: "openai_responses", Text: "[来源: " + a.Title + " " + a.URL + "]", Citation: &Citation{URL: a.URL, Title: a.Title, Start: offset + a.Start, End: offset + a.End}})
				}
			}
		}
		return append([]ContentPart{p}, cites...), nil
	case "reasoning":
		p.Type = PartReasoning
		p.Text = "[OpenAI reasoning]"
	default:
		p.Type = it.Type
		p.Text = "[OpenAI " + it.Type + "]"
	}
	return []ContentPart{p}, nil
}

func readResponses(ctx context.Context, r io.Reader, delta func(string)) (Message, error) {
	scan := bufio.NewScanner(r)
	scan.Buffer(make([]byte, 4096), 1<<20)
	// Some streams deliver full items only in output_item.done. The terminal
	// response is not guaranteed to repeat them. Keep their wire order and raw
	// payload for native replay, replacing repeated indices rather than appending.
	items := map[int]json.RawMessage{}
	itemBytes := 0
	var streamed strings.Builder
	for scan.Scan() {
		line := scan.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			continue
		}
		var ev struct {
			Type         string           `json:"type"`
			Code         string           `json:"code"`
			ErrorMessage string           `json:"message"`
			Error        *responseFailure `json:"error"`
			Delta        string           `json:"delta"`
			Item         json.RawMessage  `json:"item"`
			ItemID       string           `json:"item_id"`
			OutputIndex  int              `json:"output_index"`
			Response     struct {
				Status string            `json:"status"`
				Error  *responseFailure  `json:"error"`
				Output []json.RawMessage `json:"output"`
				Usage  *struct {
					Input   int `json:"input_tokens"`
					Output  int `json:"output_tokens"`
					Details *struct {
						Cached int `json:"cached_tokens"`
					} `json:"input_tokens_details"`
				} `json:"usage"`
			} `json:"response"`
		}
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			return Message{}, fmt.Errorf("responses: invalid stream event: %w", err)
		}
		switch ev.Type {
		case "response.output_text.delta", "response.refusal.delta":
			if streamed.Len()+len(ev.Delta) > 1<<20 {
				return Message{}, errors.New("responses: output exceeds 1 MiB")
			}
			streamed.WriteString(ev.Delta)
			if delta != nil {
				delta(ev.Delta)
			}
		case "response.output_item.added", "response.output_item.done":
			if ev.Type == "response.output_item.done" {
				if ev.OutputIndex < 0 {
					return Message{}, errors.New("responses: invalid output index")
				}
				itemBytes += len(ev.Item) - len(items[ev.OutputIndex])
				if itemBytes > 1<<20 {
					return Message{}, errors.New("responses: output exceeds 1 MiB")
				}
				if _, err := responseParts(ev.Item); err != nil {
					return Message{}, err
				}
				items[ev.OutputIndex] = ev.Item
			}
			if len(ev.Item) > 0 {
				ps, err := responseParts(ev.Item)
				if err == nil {
					for _, p := range ps {
						if p.Type == PartHosted {
							emitPart(ctx, p)
						}
					}
				}
			}
		case "response.web_search_call.in_progress", "response.web_search_call.searching", "response.web_search_call.completed":
			status := strings.TrimPrefix(ev.Type, "response.web_search_call.")
			emitPart(ctx, ContentPart{Type: PartHosted, Hosted: &HostedStep{ID: ev.ItemID, Name: "web_search", Status: status}})
		default:
			if tool, status, ok := hostedProgress(ev.Type); ok {
				emitPart(ctx, ContentPart{Type: PartHosted, Hosted: &HostedStep{ID: ev.ItemID, Name: tool, Status: status}})
			}
		case "response.completed":
			if ev.Response.Status != "completed" {
				return Message{}, errors.New("responses: response did not complete")
			}
			msg := Message{Role: RoleAssistant}
			output := ev.Response.Output
			if len(output) == 0 {
				indices := make([]int, 0, len(items))
				for i := range items {
					indices = append(indices, i)
				}
				sort.Ints(indices)
				for _, i := range indices {
					output = append(output, items[i])
				}
			}
			size, textOffset := 0, 0
			for _, raw := range output {
				size += len(raw)
				if size > 1<<20 {
					return Message{}, errors.New("responses: output exceeds 1 MiB")
				}
				ps, err := responseParts(raw)
				if err != nil {
					return Message{}, err
				}
				offset := textOffset
				for i := range ps {
					if ps[i].Type == PartText {
						textOffset += len([]rune(ps[i].Text))
					}
					if ps[i].Citation != nil {
						ps[i].Citation.Start += offset
						ps[i].Citation.End += offset
					}
				}
				msg.Content = append(msg.Content, ps...)
			}
			if len(output) == 0 && streamed.Len() > 0 {
				msg.Content = TextContent(streamed.String())
			}
			if strings.TrimSpace(msg.Text()) == "" && len(msg.Calls()) == 0 {
				return Message{}, errors.New("responses: empty response")
			}
			if u := ev.Response.Usage; u != nil {
				msg.Usage = TokenUsage{PromptTokens: u.Input, CompletionTokens: u.Output, Reported: true}
				if u.Details != nil {
					msg.Usage.CachedTokens = u.Details.Cached
					msg.Usage.CacheReported = true
				}
			}
			return msg, nil
		case "error", "response.failed":
			failure := ev.Error
			if failure == nil {
				failure = ev.Response.Error
			}
			if failure == nil && ev.Code != "" {
				failure = &responseFailure{Code: ev.Code, Message: ev.ErrorMessage}
			}
			if failure == nil {
				return Message{}, errors.New("responses: " + ev.Type)
			}
			return Message{}, failure.err()
		case "response.incomplete":
			return Message{}, errors.New("responses: response.incomplete")
		}
	}
	if err := scan.Err(); err != nil {
		return Message{}, err
	}
	return Message{}, io.ErrUnexpectedEOF
}

type visibleResponseError struct{ error }

// hostedProgress maps a hosted tool progress event to its tool name and
// status. xAI adds x_search and code_interpreter to web_search; unknown tools
// are not hosted calls and stay ignored.
func hostedProgress(event string) (tool, status string, ok bool) {
	if !strings.HasPrefix(event, "response.") {
		return "", "", false
	}
	rest := strings.TrimPrefix(event, "response.")
	for _, name := range []string{"web_search_call", "x_search_call", "code_interpreter_call"} {
		if strings.HasPrefix(rest, name+".") {
			return strings.TrimSuffix(name, "_call"), strings.TrimPrefix(rest, name+"."), true
		}
	}
	return "", "", false
}

func (e *visibleResponseError) Unwrap() error { return e.error }

type responseFailure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (f responseFailure) err() error {
	status := http.StatusBadRequest
	switch f.Code {
	case "server_error":
		status = 500
	case "rate_limit_exceeded":
		status = 429
	case "invalid_api_key":
		status = 401
	}
	return &statusError{code: status, body: "responses: " + f.Code + ": " + f.Message}
}
