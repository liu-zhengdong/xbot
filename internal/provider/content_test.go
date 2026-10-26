package provider

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestLegacyContentBecomesReplayableParts(t *testing.T) {
	for _, raw := range []string{
		`{"role":"user","content":""}`,
		`{"role":"user","content":null}`,
		`{"role":"user","content":"旧话"}`,
		`{"role":"assistant","content":"先查","tool_calls":[{"id":"c1","type":"function","function":{"name":"shell","arguments":"{}"}}]}`,
		`{"role":"tool","name":"shell","tool_call_id":"c1","content":"查到了"}`,
		`{"role":"assistant","tool_calls":[{"id":"c2","type":"function","function":{"name":"shell","arguments":"{}"}}]}`,
	} {
		var m Message
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			t.Fatal(err)
		}
		before, err := toWire(m, false)
		if err != nil {
			t.Fatal(err)
		}
		stored, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(stored, &fields); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(string(fields["content"]), "[") {
			t.Fatalf("not block storage: %s", stored)
		}
		if _, ok := fields["tool_calls"]; ok {
			t.Fatalf("duplicate tool state: %s", stored)
		}
		if _, ok := fields["tool_call_id"]; ok {
			t.Fatalf("duplicate result state: %s", stored)
		}
		var replay Message
		if err := json.Unmarshal(stored, &replay); err != nil {
			t.Fatal(err)
		}
		after, err := toWire(replay, false)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, after) {
			t.Fatalf("legacy request changed: %#v -> %#v", before, after)
		}
	}
}

func TestContentPartsKeepOrderAndOpaqueReplayData(t *testing.T) {
	raw := `{"role":"assistant","content":[{"type":"text","text":"先说"},{"type":"reasoning","vendor":"other","text":"已分析","vendor_raw":{"signature":"signed","items":[1,2]}},{"type":"tool_call","tool_call":{"id":"c1","type":"function","function":{"name":"shell","arguments":"{}"}}},{"type":"text","text":"再说"},{"type":"future","vendor":"future"}]}`
	var m Message
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	stored, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var replay Message
	if err := json.Unmarshal(stored, &replay); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(m.Content, replay.Content) {
		t.Fatalf("lost replay parts: %s", stored)
	}
	w, err := toWire(replay, false)
	if err != nil {
		t.Fatal(err)
	}
	var text string
	if err := json.Unmarshal(w.Content, &text); err != nil {
		t.Fatal(err)
	}
	if text != "先说已分析再说[future content from future]" || len(w.ToolCalls) != 1 {
		t.Fatalf("projection: %#v", w)
	}
	if string(replay.Content[1].VendorRaw) != `{"signature":"signed","items":[1,2]}` {
		t.Fatalf("lost raw: %s", stored)
	}
}

func TestDecodeStreamPreservesContentOrder(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"before"}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"shell","arguments":"{"}}]}}]}`,
		`data: {"choices":[{"delta":{"content":"middle"}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"}"}},{"index":1,"id":"c2","function":{"name":"read_file","arguments":"{}"}}]}}]}`,
		`data: {"choices":[{"delta":{"content":"after"}}]}`,
		`data: [DONE]`, "",
	}, "\n")
	m, err := decodeStream(strings.NewReader(stream), nil)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, p := range m.Content {
		kinds = append(kinds, p.Type)
	}
	if !reflect.DeepEqual(kinds, []string{PartText, PartToolCall, PartText, PartToolCall, PartText}) {
		t.Fatalf("order: %v", kinds)
	}
	if m.Text() != "beforemiddleafter" || m.Calls()[0].Function.Arguments != "{}" {
		t.Fatalf("stream: %#v", m)
	}
	stored, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var replay Message
	if err := json.Unmarshal(stored, &replay); err != nil {
		t.Fatal(err)
	}
	w, err := toWire(replay, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(w.ToolCalls) != 2 || string(w.Content) != `"beforemiddleafter"` {
		t.Fatalf("replay: %#v", w)
	}
}

func TestContentEditsDoNotMutateSource(t *testing.T) {
	m := Message{Role: RoleAssistant, Content: append(TextContent("original"), ToolContent([]ToolCall{{ID: "c1", Function: FunctionCall{Name: "shell", Arguments: `{"secret":"SECRET"}`}}})...)}
	m.Content = append(m.Content, ContentPart{Type: "reasoning", Vendor: "other", Text: "summary", VendorRaw: json.RawMessage(`{"token":"SECRET","n":1234567890123456789}`)})
	redacted := m.MapText(func(s string) string { return strings.ReplaceAll(s, "SECRET", "hidden") })
	if strings.Contains(redacted.Calls()[0].Function.Arguments, "SECRET") || strings.Contains(string(redacted.Content[2].VendorRaw), "SECRET") {
		t.Fatal("secret retained")
	}
	if !strings.Contains(string(redacted.Content[2].VendorRaw), "1234567890123456789") {
		t.Fatal("raw number changed")
	}
	if !strings.Contains(m.Calls()[0].Function.Arguments, "SECRET") || !strings.Contains(string(m.Content[2].VendorRaw), "SECRET") {
		t.Fatal("source mutated")
	}
	copy := m
	copy.SetCalls(nil)
	copy.SetText("changed")
	if len(m.Calls()) != 1 || m.Content[0].Text != "original" {
		t.Fatal("shared content edited")
	}
}

func TestMalformedContentRejected(t *testing.T) {
	for _, raw := range []string{`{"content":123}`, `{"content":{}}`, `{"content":[123]}`, `{"content":[{}]}`, `{"content":[{"type":"tool_call"}]}`, `{"content":[{"type":"text","text":"x"}],"tool_call_id":"c"}`} {
		var m Message
		if err := json.Unmarshal([]byte(raw), &m); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}

func BenchmarkContentReplay(b *testing.B) {
	m := Message{Role: RoleAssistant}
	for i := 0; i < 100; i++ {
		m.Content = append(m.Content, ContentPart{Type: PartText, Text: strings.Repeat("x", 256)})
	}
	raw, err := json.Marshal(m)
	if err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(len(raw)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var replay Message
		if err := json.Unmarshal(raw, &replay); err != nil {
			b.Fatal(err)
		}
		if _, err := toWire(replay, false); err != nil {
			b.Fatal(err)
		}
	}
}

func TestTextEditKeepsPartsAroundCalls(t *testing.T) {
	source := Message{Content: []ContentPart{
		{Type: PartText, Text: "引用：甲"},
		{Type: PartToolCall, ToolCall: &ToolCall{ID: "c1"}},
		{Type: PartText, Text: "乙丙"},
		{Type: "reasoning", Vendor: "other", Text: "opaque", VendorRaw: json.RawMessage(`{"signature":"signed"}`)},
	}}
	for _, text := range []string{"甲乙丙", "引用：甲乙丙提醒", "引用：甲丁丙", "改写", "", "引用：甲乙丙"} {
		m := source
		m.SetText(text)
		if m.Text() != text {
			t.Fatalf("text: %q want %q", m.Text(), text)
		}
		if m.Content[1].Type != PartToolCall || m.Content[3].Type != "reasoning" || string(m.Content[3].VendorRaw) != `{"signature":"signed"}` {
			t.Fatalf("part order lost: %#v", m.Content)
		}
	}
	m := source
	m.SetText("引用：甲乙丙提醒")
	if m.Content[0].Text != "引用：甲" || m.Content[2].Text != "乙丙提醒" {
		t.Fatalf("suffix moved across call: %#v", m.Content)
	}
	if source.Text() != "引用：甲乙丙" {
		t.Fatal("source changed")
	}
}

func TestContentRedactionAcrossTextParts(t *testing.T) {
	m := Message{Content: []ContentPart{
		{Type: PartText, Text: "before token-"},
		{Type: PartToolCall, ToolCall: &ToolCall{ID: "c1"}},
		{Type: PartText, Text: "joined after"},
	}}
	redacted := m.MapText(func(s string) string { return strings.ReplaceAll(s, "token-joined", "[secret]") })
	if redacted.Text() != "before [secret] after" {
		t.Fatalf("cross-part secret escaped: %q", redacted.Text())
	}
	if len(redacted.Content) != 3 || redacted.Content[1].Type != PartToolCall || m.Text() != "before token-joined after" {
		t.Fatal("redaction changed the source or block order")
	}
	raw, err := json.Marshal(redacted)
	if err != nil {
		t.Fatal(err)
	}
	var replay Message
	if err := json.Unmarshal(raw, &replay); err != nil {
		t.Fatal(err)
	}
	wire, err := toWire(replay, false)
	if err != nil {
		t.Fatal(err)
	}
	if string(wire.Content) != `"before [secret] after"` {
		t.Fatalf("replay leaked: %s", wire.Content)
	}
}

func BenchmarkStreamSmallDeltas(b *testing.B) {
	delta := `data: {"choices":[{"delta":{"content":"0123456789abcdef"}}]}` + "\n"
	raw := strings.Repeat(delta, 4096) + "data: [DONE]\n"
	b.SetBytes(4096 * 16)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m, err := decodeStream(strings.NewReader(raw), nil)
		if err != nil || len(m.Text()) != 4096*16 {
			b.Fatalf("stream: %d %v", len(m.Text()), err)
		}
	}
}

func TestContentRedactionAcrossOpaqueSummary(t *testing.T) {
	m := Message{Content: []ContentPart{{Type: PartText, Text: "token-"}, {Type: "reasoning", Vendor: "other", Text: "joined", VendorRaw: json.RawMessage(`{"signature":"signed"}`)}}}
	redacted := m.MapText(func(s string) string { return strings.ReplaceAll(s, "token-joined", "[secret]") })
	if redacted.PortableText() != "[secret][reasoning content from other]" || redacted.Content[1].Type != "reasoning" || string(redacted.Content[1].VendorRaw) != `{"signature":"signed"}` {
		t.Fatalf("redaction lost content shape: %#v", redacted)
	}
	if m.PortableText() != "token-joined" {
		t.Fatal("source mutated")
	}
}
