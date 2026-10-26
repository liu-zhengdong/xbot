package provider

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

type anthropicPause struct{}

func (*anthropicPause) Error() string { return "messages: server tool continuation limit reached" }

func anthropicParts(raw json.RawMessage, offset int) ([]ContentPart, error) {
	var b struct {
		Type      string          `json:"type"`
		ID        string          `json:"id"`
		Name      string          `json:"name"`
		Text      string          `json:"text"`
		Input     json.RawMessage `json:"input"`
		ToolUseID string          `json:"tool_use_id"`
		Content   json.RawMessage `json:"content"`
		Citations []struct {
			URL   string `json:"url"`
			Title string `json:"title"`
		} `json:"citations"`
	}
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, err
	}
	if b.Type == "" {
		return nil, errors.New("messages: content block has no type")
	}
	p := ContentPart{Vendor: AnthropicMessages, VendorRaw: raw}
	switch b.Type {
	case "text":
		p.Type, p.Text = PartText, b.Text
		parts := []ContentPart{p}
		for _, c := range b.Citations {
			if c.URL != "" {
				parts = append(parts, ContentPart{Type: PartCitation, Vendor: AnthropicMessages, Text: "[来源: " + c.Title + " " + c.URL + "]", Citation: &Citation{URL: c.URL, Title: c.Title, Start: offset, End: offset + len([]rune(b.Text))}})
			}
		}
		return parts, nil
	case "tool_use":
		var obj map[string]any
		if b.ID == "" || b.Name == "" || json.Unmarshal(b.Input, &obj) != nil || obj == nil {
			return nil, errors.New("messages: malformed tool_use")
		}
		p.Type = PartToolCall
		p.ToolCall = &ToolCall{ID: b.ID, Type: "function", Function: FunctionCall{Name: b.Name, Arguments: string(b.Input)}}
	case "thinking", "redacted_thinking":
		p.Type, p.Text = PartReasoning, "[Claude thinking]"
	case "server_tool_use":
		if b.ID == "" || b.Name == "" {
			return nil, errors.New("messages: malformed server_tool_use")
		}
		var input struct {
			Query string `json:"query"`
		}
		_ = json.Unmarshal(b.Input, &input)
		p.Type = PartHosted
		p.Hosted = &HostedStep{ID: b.ID, Name: b.Name, Status: "in_progress", Query: input.Query}
		p.Text = "[厂商工具: " + b.Name + " " + input.Query + "]"
	case "web_search_tool_result":
		// The call alone owns Hosted, so trace accounting counts it once.
		p.Type, p.Text = PartHosted, "[厂商搜索结果]"
		var failure struct {
			Type string `json:"type"`
			Code string `json:"error_code"`
		}
		if json.Unmarshal(b.Content, &failure) == nil && failure.Code != "" {
			p.Text = "[厂商搜索失败: " + failure.Code + "]"
		}
		var results []struct {
			Title string `json:"title"`
			URL   string `json:"url"`
		}
		if json.Unmarshal(b.Content, &results) == nil {
			for _, r := range results {
				p.Text += "[" + r.Title + " " + r.URL + "]"
			}
		}
	default:
		p.Type, p.Text = PartText, "[Claude "+b.Type+" 内容]"
		// Unknown types are not replayable even on the same protocol.
	}
	return []ContentPart{p}, nil
}

type anthropicUsage struct {
	Input   *int `json:"input_tokens"`
	Output  *int `json:"output_tokens"`
	Cached  *int `json:"cache_read_input_tokens"`
	Created *int `json:"cache_creation_input_tokens"`
}

func readAnthropic(ctx context.Context, r io.Reader, delta func(string)) (Message, error) {
	scan := bufio.NewScanner(r)
	scan.Buffer(make([]byte, 4096), 1<<20)
	msg := Message{Role: RoleAssistant}
	var block map[string]json.RawMessage
	index := -1
	nextIndex, size, textOffset := 0, 0, 0
	inputJSON := ""
	stop := ""
	started, finished := false, false
	input, cached, created := 0, 0, 0
	usage := func(u *anthropicUsage) {
		if u == nil {
			return
		}
		msg.Usage.Reported = true
		if u.Input != nil {
			input = *u.Input
		}
		if u.Output != nil {
			msg.Usage.CompletionTokens = *u.Output
		}
		if u.Cached != nil {
			cached = *u.Cached
			msg.Usage.CacheReported = true
		}
		if u.Created != nil {
			created = *u.Created
		}
		msg.Usage.PromptTokens = input + cached + created
		msg.Usage.CachedTokens = cached
	}
	for scan.Scan() {
		line := scan.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		var ev struct {
			Type    string          `json:"type"`
			Index   int             `json:"index"`
			Block   json.RawMessage `json:"content_block"`
			Message struct {
				Usage *anthropicUsage `json:"usage"`
			} `json:"message"`
			Usage *anthropicUsage `json:"usage"`
			Delta struct {
				Type      string          `json:"type"`
				Text      string          `json:"text"`
				Thinking  string          `json:"thinking"`
				Signature string          `json:"signature"`
				Partial   string          `json:"partial_json"`
				Citation  json.RawMessage `json:"citation"`
				Stop      string          `json:"stop_reason"`
			} `json:"delta"`
			Error struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			return Message{}, fmt.Errorf("messages: invalid stream event: %w", err)
		}
		switch ev.Type {
		case "message_start":
			if started {
				return Message{}, errors.New("messages: duplicate start")
			}
			started = true
			usage(ev.Message.Usage)
		case "content_block_start":
			if !started || block != nil || ev.Index != nextIndex || finished {
				return Message{}, errors.New("messages: invalid block start")
			}
			size += len(ev.Block)
			if size > 1<<20 {
				return Message{}, errors.New("messages: output exceeds 1 MiB")
			}
			if err := json.Unmarshal(ev.Block, &block); err != nil || block == nil {
				return Message{}, errors.New("messages: malformed block")
			}
			index = ev.Index
			inputJSON = ""
			var typ, name, id string
			_ = json.Unmarshal(block["type"], &typ)
			_ = json.Unmarshal(block["name"], &name)
			_ = json.Unmarshal(block["id"], &id)
			if typ == "server_tool_use" {
				emitPart(ctx, ContentPart{Type: PartHosted, Hosted: &HostedStep{ID: id, Name: name, Status: "in_progress"}})
			}
		case "content_block_delta":
			if block == nil || ev.Index != index {
				return Message{}, errors.New("messages: delta without matching block")
			}
			size += len(data)
			if size > 1<<20 {
				return Message{}, errors.New("messages: output exceeds 1 MiB")
			}
			appendString := func(key, value string) {
				var old string
				_ = json.Unmarshal(block[key], &old)
				block[key], _ = json.Marshal(old + value)
			}
			switch ev.Delta.Type {
			case "text_delta":
				appendString("text", ev.Delta.Text)
				if delta != nil {
					delta(ev.Delta.Text)
				}
			case "thinking_delta":
				appendString("thinking", ev.Delta.Thinking)
			case "signature_delta":
				appendString("signature", ev.Delta.Signature)
			case "input_json_delta":
				inputJSON += ev.Delta.Partial
			case "citations_delta":
				var cs []json.RawMessage
				_ = json.Unmarshal(block["citations"], &cs)
				cs = append(cs, ev.Delta.Citation)
				block["citations"], _ = json.Marshal(cs)
			default:
				var typ string
				_ = json.Unmarshal(block["type"], &typ)
				if typ == "text" || typ == "tool_use" || typ == "thinking" || typ == "server_tool_use" {
					return Message{}, fmt.Errorf("messages: unsupported delta %q", ev.Delta.Type)
				}
				// Unknown block types retain their portable summary.

			}
		case "content_block_stop":
			if block == nil || ev.Index != index {
				return Message{}, errors.New("messages: stop without matching block")
			}
			if inputJSON != "" {
				if !json.Valid([]byte(inputJSON)) {
					return Message{}, errors.New("messages: invalid streamed tool input")
				}
				block["input"] = json.RawMessage(inputJSON)
			}
			raw, err := json.Marshal(block)
			if err != nil {
				return Message{}, err
			}
			ps, err := anthropicParts(raw, textOffset)
			if err != nil {
				return Message{}, err
			}
			for _, p := range ps {
				if p.Type == PartText {
					textOffset += len([]rune(p.Text))
				}
				if p.Hosted != nil {
					emitPart(ctx, p)
				}
			}
			settleAnthropicSearch(ctx, msg.Content, raw)
			msg.Content = append(msg.Content, ps...)
			block = nil
			nextIndex++
		case "message_delta":
			if !started || block != nil {
				return Message{}, errors.New("messages: invalid message delta")
			}
			usage(ev.Usage)
			if ev.Delta.Stop != "" {
				stop = ev.Delta.Stop
				finished = true
			}
		case "message_stop":
			if !started || !finished || block != nil {
				return Message{}, io.ErrUnexpectedEOF
			}
			msg.StopReason = stop
			switch stop {
			case "end_turn", "tool_use", "stop_sequence", "refusal", "max_tokens":
				return msg, nil
			case "pause_turn":
				return msg, &anthropicPause{}
			default:
				return Message{}, fmt.Errorf("messages: incomplete response (%s)", stop)
			}
		case "error":
			code := 400
			switch ev.Error.Type {
			case "overloaded_error", "api_error":
				code = 500
			case "rate_limit_error":
				code = 429
			case "authentication_error":
				code = 401
			}
			return Message{}, &statusError{code: code, body: "messages: " + ev.Error.Type + ": " + ev.Error.Message}
			// ping and new event types carry no completed content.
		}
	}
	if err := scan.Err(); err != nil {
		return Message{}, err
	}
	return Message{}, io.ErrUnexpectedEOF
}

// Update the call block rather than adding another HostedStep for its result.
func settleAnthropicSearch(ctx context.Context, parts []ContentPart, raw json.RawMessage) {
	var result struct {
		Type    string          `json:"type"`
		ID      string          `json:"tool_use_id"`
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(raw, &result) != nil || result.Type != "web_search_tool_result" {
		return
	}
	status := "completed"
	var failure struct {
		Code string `json:"error_code"`
	}
	if json.Unmarshal(result.Content, &failure) == nil && failure.Code != "" {
		status = "failed"
	}
	for i := range parts {
		p := &parts[i]
		if p.Hosted == nil || p.Vendor != AnthropicMessages || p.Hosted.ID != result.ID || p.Hosted.Status == status {
			continue
		}
		step := *p.Hosted
		step.Status = status
		p.Hosted = &step
		p.Text = "[厂商工具: " + step.Name + " " + step.Query + " · " + status + "]"
		emitPart(ctx, *p)
	}
}
