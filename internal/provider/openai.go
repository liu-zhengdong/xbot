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
}

var defaultHTTP = &http.Client{
	Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ResponseHeaderTimeout: 90 * time.Second,
	},
}

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
func (c *Client) Chat(ctx context.Context, req Request, onDelta func(string)) (Message, error) {
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
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(), bytes.NewReader(raw))
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
		return Message{}, fmt.Errorf("provider: http %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	msg, err := decodeStream(resp.Body, onDelta)
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
