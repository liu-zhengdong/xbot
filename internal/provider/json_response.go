package provider

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

// responseStream lets endpoints that ignore stream:true use the same protocol
// parsers and stop-reason handling. JSON bodies are bounded before decoding.
func responseStream(r io.Reader, contentType, protocol string) (io.Reader, error) {
	if !strings.Contains(strings.ToLower(contentType), "application/json") {
		return r, nil
	}
	raw, err := io.ReadAll(io.LimitReader(r, (1<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > 1<<20 {
		return nil, errors.New("provider: JSON response exceeds 1 MiB")
	}
	var body map[string]json.RawMessage
	if json.Unmarshal(raw, &body) != nil || body == nil || len(body["error"]) > 0 && string(body["error"]) != "null" {
		return nil, errors.New("provider: invalid JSON response")
	}
	var stream bytes.Buffer
	emit := func(v any) {
		b, _ := json.Marshal(v)
		stream.WriteString("data: ")
		stream.Write(b)
		stream.WriteString("\n\n")
	}
	switch protocol {
	case ChatCompletions:
		var choices []struct {
			Message map[string]json.RawMessage `json:"message"`
			Finish  string                     `json:"finish_reason"`
		}
		if json.Unmarshal(body["choices"], &choices) != nil || len(choices) == 0 || choices[0].Message == nil {
			return nil, errors.New("provider: invalid JSON choices")
		}
		m := choices[0].Message
		if calls, ok := m["tool_calls"]; ok {
			var ts []map[string]json.RawMessage
			if json.Unmarshal(calls, &ts) != nil {
				return nil, errors.New("provider: invalid JSON tool calls")
			}
			for i := range ts {
				if ts[i] == nil {
					return nil, errors.New("provider: invalid JSON tool call")
				}
				ts[i]["index"], _ = json.Marshal(i)
			}
			m["tool_calls"], _ = json.Marshal(ts)
		}
		emit(map[string]any{"choices": []any{map[string]any{"delta": m, "finish_reason": choices[0].Finish}}, "usage": body["usage"]})
		stream.WriteString("data: [DONE]\n\n")
	case AnthropicMessages:
		var blocks []json.RawMessage
		if json.Unmarshal(body["content"], &blocks) != nil {
			return nil, errors.New("messages: invalid JSON content")
		}
		emit(map[string]any{"type": "message_start", "message": map[string]any{"usage": body["usage"]}})
		for i, block := range blocks {
			emit(map[string]any{"type": "content_block_start", "index": i, "content_block": block})
			emit(map[string]any{"type": "content_block_stop", "index": i})
		}
		emit(map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": body["stop_reason"]}})
		emit(map[string]any{"type": "message_stop"})
	case ResponsesProtocol:
		var status string
		_ = json.Unmarshal(body["status"], &status)
		if status != "completed" && status != "incomplete" && status != "failed" {
			return nil, errors.New("responses: invalid JSON status")
		}
		emit(map[string]any{"type": "response." + status, "response": body})
	default:
		return nil, errors.New("provider: unknown JSON protocol")
	}
	return &stream, nil
}
