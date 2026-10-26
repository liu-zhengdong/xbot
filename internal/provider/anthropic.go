package provider

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Anthropic speaks Messages. Hosted blocks are never executable local calls.
type Anthropic struct {
	Client
	WebSearch bool
}

type anthropicMessage struct {
	Role    string            `json:"role"`
	Content []json.RawMessage `json:"content"`
}

func anthropicInput(messages []Message, toolsEnabled bool) ([]json.RawMessage, []anthropicMessage, error) {
	system := []json.RawMessage{}
	out := []anthropicMessage{}
	for _, m := range messages {
		role := m.Role
		if role == RoleTool {
			role = RoleUser
		}
		if role != RoleSystem && role != RoleUser && role != RoleAssistant {
			return nil, nil, fmt.Errorf("messages: unsupported role %q", role)
		}
		parts := []json.RawMessage{}
		add := func(v any) error {
			b, e := json.Marshal(v)
			if e == nil {
				parts = append(parts, b)
			}
			return e
		}
		images, imageErr := anthropicImages(m.Images)
		if imageErr != nil {
			if m.Role != RoleTool {
				return nil, nil, imageErr
			}
			images = []any{map[string]any{"type": "text", "text": imageErr.Error()}}
		}
		for _, p := range m.Content {
			// Summaries have no tool definitions and cannot contain native
			// tool blocks. Keep their history readable without opening tools.
			if !toolsEnabled {
				switch p.Type {
				case PartToolCall:
					if p.ToolCall == nil {
						return nil, nil, errors.New("messages: empty tool call")
					}
					p = ContentPart{Type: PartText, Text: "[工具调用: " + p.ToolCall.Function.Name + " " + p.ToolCall.Function.Arguments + "]"}
				case PartToolResult:
					p = ContentPart{Type: PartText, Text: "[工具结果: " + p.ToolCallID + " " + p.Text + "]"}
				case PartHosted:
					p = ContentPart{Type: PartText, Text: (Message{Content: []ContentPart{p}}).PortableText()}
				}
			}
			// Only replay recognized assistant blocks; unknown native blocks are also
			// portable summaries, so a future server type cannot break old histories.
			if role == RoleAssistant && p.Vendor == AnthropicMessages && anthropicReplayable(p.VendorRaw) {
				if p.Type != PartText && p.Type != PartToolCall {
					parts = append(parts, p.VendorRaw)
					continue
				}
			}
			switch p.Type {
			case PartToolCall:
				if role != RoleAssistant || p.ToolCall == nil {
					return nil, nil, errors.New("messages: invalid tool call")
				}
				t := p.ToolCall
				var input map[string]json.RawMessage
				if err := json.Unmarshal([]byte(t.Function.Arguments), &input); err != nil || input == nil || t.ID == "" || t.Function.Name == "" {
					return nil, nil, errors.New("messages: malformed tool input")
				}
				if err := add(map[string]any{"type": "tool_use", "id": t.ID, "name": t.Function.Name, "input": input}); err != nil {
					return nil, nil, err
				}
			case PartToolResult:
				if role != RoleUser || p.ToolCallID == "" {
					return nil, nil, errors.New("messages: invalid tool result")
				}
				var content any = p.Text
				if len(images) > 0 {
					nested := []any{}
					if p.Text != "" {
						nested = append(nested, map[string]any{"type": "text", "text": p.Text})
					}
					content = append(nested, images...)
					images = nil
				}
				result := map[string]any{"type": "tool_result", "tool_use_id": p.ToolCallID, "content": content}
				if imageErr != nil {
					result["is_error"] = true
				}
				if err := add(result); err != nil {
					return nil, nil, err
				}
			default:
				if p.Type == PartCitation && p.Vendor == AnthropicMessages {
					continue
				}
				// Native text is rebuilt from the current projection because the kernel
				// may remove control lines or scrub secrets before replay.
				if p.Type == PartText && role == RoleAssistant && p.Vendor == AnthropicMessages && anthropicReplayable(p.VendorRaw) {
					var block map[string]json.RawMessage
					if err := json.Unmarshal(p.VendorRaw, &block); err != nil {
						return nil, nil, err
					}
					block["text"], _ = json.Marshal(p.Text)
					if p.Text != "" {
						if err := add(block); err != nil {
							return nil, nil, err
						}
					}
					continue
				}
				text := (Message{Content: []ContentPart{p}}).PortableText()
				if text != "" {
					if err := add(map[string]any{"type": "text", "text": text}); err != nil {
						return nil, nil, err
					}
				}
			}
		}
		for _, img := range images {
			if err := add(img); err != nil {
				return nil, nil, err
			}
		}
		if len(parts) == 0 {
			continue
		}
		if role == RoleSystem {
			system = append(system, parts...)
			continue
		}
		// Adjacent local results must share one user message, ahead of owner text.
		if len(out) > 0 && out[len(out)-1].Role == role {
			out[len(out)-1].Content = append(out[len(out)-1].Content, parts...)
		} else {
			out = append(out, anthropicMessage{Role: role, Content: parts})
		}
	}
	return system, out, nil
}

func anthropicImages(images []Image) ([]any, error) {
	out := make([]any, 0, len(images))
	for _, img := range images {
		// Direct Claude API limits each image to 10 MB after base64 encoding.
		if base64.StdEncoding.EncodedLen(len(img.Data)) > 10_000_000 {
			return nil, errors.New("messages: 图片超过 base64 后 10 MB 的限制，请缩小图片后重试")
		}
		mime := img.MIME
		if mime == "" {
			mime = "image/png"
		}
		out = append(out, map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": mime, "data": base64.StdEncoding.EncodeToString(img.Data)}})
	}
	return out, nil
}

func anthropicReplayable(raw json.RawMessage) bool {
	var b struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(raw, &b) != nil {
		return false
	}
	switch b.Type {
	case "text", "tool_use", "thinking", "redacted_thinking", "server_tool_use", "web_search_tool_result":
		return true
	}
	return false
}

func (c *Anthropic) Chat(ctx context.Context, req Request, delta func(string)) (Message, error) {
	visible := false
	observer, _ := ctx.Value(partObserverKey{}).(func(ContentPart))
	ctx = WithPartObserver(ctx, func(p ContentPart) {
		visible = true
		if observer != nil {
			observer(p)
		}
	})
	return c.complete(ctx, req, delta, func(ctx context.Context, r Request, d func(string)) (Message, error) {
		ctx, cancel := context.WithTimeout(ctx, c.requestTimeout())
		defer cancel()
		var m Message
		var err error
		var billed TokenTotals
		initialPrompt := 0
		allReported := true
		r.Messages = append([]Message(nil), r.Messages...)
		// Anthropic may pause a long server-tool turn. Resume with the exact
		// assistant blocks, under the same timeout, without executing local tools.
		for continuation := 0; continuation < 3; continuation++ {
			var next Message
			next, err = c.messagesOnce(ctx, r, d)
			m.Role = RoleAssistant
			offset := len([]rune(m.Text()))
			for i := range next.Content {
				if next.Content[i].Citation != nil {
					next.Content[i].Citation.Start += offset
					next.Content[i].Citation.End += offset
				}
			}
			m.Content = append(m.Content, next.Content...)
			// Results can arrive after a pause, referencing a previous response.
			for _, p := range next.Content {
				settleAnthropicSearch(ctx, m.Content, p.VendorRaw)
			}
			if continuation == 0 {
				initialPrompt = next.Usage.PromptTokens
			}
			billed.PromptTokens += next.Usage.PromptTokens
			billed.CompletionTokens += next.Usage.CompletionTokens
			billed.CachedTokens += next.Usage.CachedTokens
			allReported = allReported && next.Usage.Reported
			m.Usage = next.Usage
			if continuation > 0 && allReported {
				m.Usage.Billed = &billed
				m.Usage.InitialPromptTokens = initialPrompt
			}

			var pause *anthropicPause
			if !errors.As(err, &pause) {
				break
			}
			if len(next.Calls()) > 0 {
				err = errors.New("messages: paused response contains local calls")
				break
			}
			r.Messages = append(r.Messages, next)
		}
		if err != nil && ctx.Err() == context.DeadlineExceeded {
			err = fmt.Errorf("%w: %v", ErrRequestTimeout, err)
		}
		if err != nil && visible {
			err = &visibleResponseError{err}
		}
		return m, err
	})
}

func (c *Anthropic) messagesOnce(ctx context.Context, req Request, delta func(string)) (Message, error) {
	if strings.TrimSpace(c.APIKey) == "" || strings.TrimSpace(c.Model) == "" || strings.TrimSpace(c.BaseURL) == "" {
		return Message{}, errors.New("messages: endpoint, model and API key are required")
	}
	ts := []any{}
	for _, t := range req.Tools {
		if c.WebSearch && hostedSearchAllowed(ctx) && t.Name == "web_search" {
			return Message{}, errors.New("messages: hosted search cannot coexist with local web_search")
		}
		ts = append(ts, map[string]any{"name": t.Name, "description": t.Description, "input_schema": t.Parameters})
	}
	if c.WebSearch && hostedSearchAllowed(ctx) && (req.HostedSearch || len(req.Tools) > 0) {
		ts = append(ts, map[string]any{"type": "web_search_20250305", "name": "web_search"})
	}
	system, messages, err := anthropicInput(req.Messages, len(ts) > 0)
	if err != nil {
		return Message{}, err
	}
	body := map[string]any{"model": c.Model, "max_tokens": anthropicMaxTokens, "system": system, "messages": messages, "stream": true}
	if len(ts) > 0 {
		body["tools"] = ts
	}
	switch c.Thinking {
	case ThinkingOff:
		body["thinking"] = map[string]any{"type": "disabled"}
	case ThinkingOn:
		// Signed thinking blocks are already replayed from VendorRaw.
		body["thinking"] = map[string]any{"type": "enabled", "budget_tokens": anthropicThinkingBudget}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return Message{}, err
	}
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	endpoint := strings.TrimRight(c.BaseURL, "/")
	if !strings.HasSuffix(endpoint, "/messages") {
		endpoint += "/messages"
	}
	r, err := http.NewRequestWithContext(streamCtx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return Message{}, err
	}
	r.Header.Set("x-api-key", c.APIKey)
	r.Header.Set("anthropic-version", "2023-06-01")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "text/event-stream")
	h := c.HTTPClient
	if h == nil {
		h = defaultHTTP
	}
	// x-api-key is a custom header: net/http does not strip it on redirects.
	// Copy the client so concurrent calls and the shared transport stay intact.
	direct := *h
	direct.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := direct.Do(r)
	if err != nil {
		return Message{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return Message{}, &statusError{code: res.StatusCode, body: strings.ReplaceAll(string(b), c.APIKey, "[secret]")}
	}
	msg, err := readAnthropic(ctx, &idleReader{r: res.Body, idle: c.streamIdleTimeout(), cancel: cancel}, delta)
	var status *statusError
	if errors.As(err, &status) {
		err = &statusError{code: status.code, body: strings.ReplaceAll(status.body, c.APIKey, "[secret]")}
	}
	return msg, err
}

const (
	anthropicMaxTokens = 8192
	// anthropicThinkingBudget must stay below anthropicMaxTokens.
	anthropicThinkingBudget = 4096
)
