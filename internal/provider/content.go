package provider

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	PartText       = "text"
	PartToolCall   = "tool_call"
	PartToolResult = "tool_result"
	PartHosted     = "server_tool"
	PartCitation   = "citation"
	PartReasoning  = "reasoning"
)

// ContentPart is one ordered piece of a message, not a compaction SummaryBlock.
// Text on an unfamiliar type is its portable summary. VendorRaw is an opaque
// replay payload; only an adapter that understands Vendor may interpret it.
// Image bytes remain request-local in Message.Images, never in this record.
type ContentPart struct {
	Type       string          `json:"type"`
	Text       string          `json:"text,omitempty"`
	ToolCall   *ToolCall       `json:"tool_call,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	Vendor     string          `json:"vendor,omitempty"`
	VendorRaw  json.RawMessage `json:"vendor_raw,omitempty"`
	Hosted     *HostedStep     `json:"hosted,omitempty"`
	Citation   *Citation       `json:"citation,omitempty"`
}

type HostedStep struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Query  string `json:"query,omitempty"`
}

type Citation struct {
	URL   string `json:"url"`
	Title string `json:"title"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

func TextContent(text string) []ContentPart {
	if text == "" {
		return nil
	}
	return []ContentPart{{Type: PartText, Text: text}}
}

func ToolContent(calls []ToolCall) []ContentPart {
	parts := make([]ContentPart, 0, len(calls))
	for _, call := range calls {
		c := call
		parts = append(parts, ContentPart{Type: PartToolCall, ToolCall: &c})
	}
	return parts
}

func ToolResult(id, text string) []ContentPart {
	return []ContentPart{{Type: PartToolResult, ToolCallID: id, Text: text}}
}

// Text returns ordinary text and local tool output. Opaque vendor content is
// not assistant speech and must not be parsed as kernel control lines.
func (m Message) Text() string { return m.contentText(false) }

// PortableText adds summaries for blocks Completions cannot replay natively.
func (m Message) PortableText() string { return m.contentText(true) }

func (m Message) contentText(portable bool) string {
	var first string
	var out strings.Builder
	for _, p := range m.Content {
		text := p.Text
		switch p.Type {
		case PartToolCall:
			continue
		case PartText, PartToolResult:
		default:
			if !portable {
				continue
			}
			if text == "" {
				text = fmt.Sprintf("[%s content from %s]", p.Type, p.Vendor)
			}
		}
		if text == "" {
			continue
		}
		if first == "" {
			first = text
			continue
		}
		if out.Len() == 0 {
			out.WriteString(first)
		}
		out.WriteString(text)
	}
	if out.Len() == 0 {
		return first
	}
	return out.String()
}

func (m Message) Calls() []ToolCall {
	var calls []ToolCall
	for _, p := range m.Content {
		if p.Type == PartToolCall && p.ToolCall != nil {
			calls = append(calls, *p.ToolCall)
		}
	}
	return calls
}

func (m Message) CallID() string {
	for _, p := range m.Content {
		if p.Type == PartToolResult {
			return p.ToolCallID
		}
	}
	return ""
}

// SetText applies a text edit without moving calls or opaque vendor parts.
// Shared transcript slices remain unchanged. Common text on either side of
// the edit stays in its original parts, including text after a tool call.
func (m *Message) SetText(text string) {
	old := m.Text()
	if text == old {
		return
	}
	start := 0
	for start < len(old) && start < len(text) {
		a, n := utf8.DecodeRuneInString(old[start:])
		b, m := utf8.DecodeRuneInString(text[start:])
		if a != b || n != m {
			break
		}
		start += n
	}
	end, newEnd := len(old), len(text)
	for end > start && newEnd > start {
		a, n := utf8.DecodeLastRuneInString(old[:end])
		b, m := utf8.DecodeLastRuneInString(text[:newEnd])
		if a != b || n != m {
			break
		}
		end -= n
		newEnd -= m
	}
	replacement := text[start:newEnd]
	parts := make([]ContentPart, 0, len(m.Content)+1)
	offset := 0
	inserted := false
	for _, p := range m.Content {
		if p.Type == PartText || p.Type == PartToolResult {
			next := offset + len(p.Text)
			if !inserted && start <= next {
				lo := max(0, start-offset)
				hi := min(len(p.Text), end-offset)
				p.Text = p.Text[:lo] + replacement + p.Text[hi:]
				inserted = true
			} else if inserted && offset < end {
				p.Text = p.Text[min(len(p.Text), end-offset):]
			}
			offset = next
		}
		parts = append(parts, p)
	}
	if !inserted {
		parts = append(TextContent(replacement), parts...)
	}
	remapCitations(parts, old, text)
	m.Content = parts
}

func (m *Message) SetCalls(calls []ToolCall) {
	parts := make([]ContentPart, 0, len(m.Content)+len(calls))
	next := 0
	for _, p := range m.Content {
		if p.Type == PartToolCall {
			if next < len(calls) {
				c := calls[next]
				p.ToolCall = &c
				parts = append(parts, p)
				next++
			}
		} else {
			parts = append(parts, p)
		}
	}
	parts = append(parts, ToolContent(calls[next:])...)
	m.Content = parts
}

// MarshalJSON always writes block arrays, including an empty message body.
func (m Message) MarshalJSON() ([]byte, error) {
	type plain Message
	if m.Content == nil {
		m.Content = []ContentPart{}
	}
	return json.Marshal(plain(m))
}

// UnmarshalJSON reads both append-only legacy strings and content-block rows.
// New writes use Message's ordinary JSON encoding and contain only blocks.
func (m *Message) UnmarshalJSON(data []byte) error {
	type plain Message
	var row struct {
		*plain
		Content    json.RawMessage `json:"content"`
		ToolCalls  []ToolCall      `json:"tool_calls"`
		ToolCallID string          `json:"tool_call_id"`
	}
	var value Message
	row.plain = (*plain)(&value)
	if err := json.Unmarshal(data, &row); err != nil {
		return err
	}
	raw := bytes.TrimSpace(row.Content)
	if len(raw) > 0 && raw[0] == '"' {
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return err
		}
		if row.ToolCallID != "" {
			value.Content = ToolResult(row.ToolCallID, text)
		} else {
			value.Content = TextContent(text)
		}
	} else if len(raw) > 0 && !bytes.Equal(raw, []byte("null")) {
		if len(row.ToolCalls) > 0 || row.ToolCallID != "" {
			return fmt.Errorf("provider: block content cannot carry legacy tool fields")
		}
		if err := json.Unmarshal(raw, &value.Content); err != nil {
			return fmt.Errorf("provider: message content: %w", err)
		}
	}
	for _, p := range value.Content {
		if p.Type == "" || (p.Type == PartToolCall && p.ToolCall == nil) {
			return fmt.Errorf("provider: malformed content part %q", p.Type)
		}
	}

	if row.ToolCallID != "" && len(value.Content) == 0 {
		value.Content = ToolResult(row.ToolCallID, "")
	}
	if len(row.ToolCalls) > 0 {
		value.Content = append(value.Content, ToolContent(row.ToolCalls)...)
	}
	*m = value
	return nil
}

// MapText returns an independent message with its persisted strings scrubbed.
// It keeps block order and replay payload structure, unlike flattening Text().
func (m Message) MapText(f func(string) string) Message {
	// Scrub the complete portable text, including opaque summaries, then
	// distribute the edit across the same part positions. JSON encoding must
	// not hide a secret by splitting it across string fields.
	parts := append([]ContentPart(nil), m.Content...)
	text := m.PortableText()
	if redacted := f(text); redacted != text {
		view := Message{Content: append([]ContentPart(nil), parts...)}
		for i := range view.Content {
			p := &view.Content[i]
			if p.Type != PartToolCall && p.Type != PartToolResult && p.Type != PartText {
				if p.Text == "" {
					p.Text = fmt.Sprintf("[%s content from %s]", p.Type, p.Vendor)
				}
				p.Type = PartText
			}
		}
		view.SetText(redacted)
		for i := range parts {
			parts[i].Text = view.Content[i].Text
		}
	}
	for i := range parts {
		p := &parts[i]
		if p.Hosted != nil {
			h := *p.Hosted
			h.Query = f(h.Query)
			p.Hosted = &h
		}
		if p.Citation != nil {
			c := *p.Citation
			c.Title = f(c.Title)
			c.URL = f(c.URL)
			p.Citation = &c
		}
		if p.ToolCall != nil {
			call := *p.ToolCall
			call.Function.Arguments = f(call.Function.Arguments)
			p.ToolCall = &call
		}
		if len(p.VendorRaw) > 0 {
			var value any
			dec := json.NewDecoder(bytes.NewReader(p.VendorRaw))
			dec.UseNumber()
			if dec.Decode(&value) == nil {
				changed := false
				var scrub func(any) any
				scrub = func(v any) any {
					switch x := v.(type) {
					case string:
						y := f(x)
						changed = changed || x != y
						return y
					case []any:
						for j := range x {
							x[j] = scrub(x[j])
						}
					case map[string]any:
						for k, v := range x {
							x[k] = scrub(v)
						}
					}
					return v
				}
				value = scrub(value)
				if changed {
					p.VendorRaw, _ = json.Marshal(value)
				}
			}
		}
	}
	remapCitations(parts, m.Text(), (Message{Content: parts}).Text())
	m.Content = parts
	return m
}

// remapCitations edits request-local offsets, never shared transcript objects.
func remapCitations(parts []ContentPart, old, new string) {
	hasCitation := false
	for _, p := range parts {
		if p.Citation != nil {
			hasCitation = true
			break
		}
	}
	if !hasCitation {
		return
	}
	if old == new {
		return
	}
	a, b := []rune(old), []rune(new)
	start := 0
	for start < len(a) && start < len(b) && a[start] == b[start] {
		start++
	}
	end, newEnd := len(a), len(b)
	for end > start && newEnd > start && a[end-1] == b[newEnd-1] {
		end--
		newEnd--
	}
	shift := func(pos int, exclusiveEnd bool) int {
		if pos < start || (exclusiveEnd && pos == start) {
			return pos
		}
		if pos >= end {
			return pos + newEnd - end
		}
		return start + min(pos-start, newEnd-start)
	}
	for i := range parts {
		if parts[i].Citation != nil {
			c := *parts[i].Citation
			c.Start = shift(c.Start, false)
			c.End = shift(c.End, true)
			parts[i].Citation = &c
		}
	}
}
