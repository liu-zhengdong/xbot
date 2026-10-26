package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// fakePNG is a PNG signature padded to n bytes; content sniffing needs only
// the signature.
func fakePNG(n int) []byte {
	b := make([]byte, n)
	copy(b, "\x89PNG\r\n\x1a\n")
	return b
}

type sseTransport struct {
	stream string
	bodies []map[string]any
}

func (s *sseTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	if json.Unmarshal(raw, &body) != nil {
		return nil, fmt.Errorf("bad request body")
	}
	s.bodies = append(s.bodies, body)
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(s.stream)), Header: http.Header{"Content-Type": []string{"text/event-stream"}}}, nil
}

func imageTools(body map[string]any) []map[string]any {
	var out []map[string]any
	tools, _ := body["tools"].([]any)
	for _, t := range tools {
		if m, ok := t.(map[string]any); ok && m["type"] == "image_generation" {
			out = append(out, m)
		}
	}
	return out
}

func TestResponsesMountsImageGeneration(t *testing.T) {
	save := func(context.Context, []byte) (string, error) { return "generated/x.png", nil }
	cases := []struct {
		name     string
		base     string
		search   bool
		wrap     bool
		gen      bool
		tools    []ToolDef
		want     []map[string]any
		allTypes []string
	}{
		{"openai with tool model", "https://api.openai.com/v1", true, false, true, []ToolDef{{Name: "shell"}}, []map[string]any{{"type": "image_generation", "model": "gpt-image-2.5-flare"}}, []string{"function", "web_search", "image_generation"}},
		{"search off still generates", "https://api.openai.com/v1", false, false, true, []ToolDef{{Name: "shell"}}, []map[string]any{{"type": "image_generation", "model": "gpt-image-2.5-flare"}}, []string{"function", "image_generation"}},
		{"hosted search suppressed still generates", "https://api.openai.com/v1", true, true, true, []ToolDef{{Name: "shell"}}, []map[string]any{{"type": "image_generation", "model": "gpt-image-2.5-flare"}}, []string{"function", "image_generation"}},
		{"xai takes no openai options", "https://api.x.ai/v1", true, false, true, []ToolDef{{Name: "shell"}}, []map[string]any{{"type": "image_generation"}}, []string{"function", "web_search", "x_search", "code_interpreter", "image_generation"}},
		{"off without workspace sink", "https://api.openai.com/v1", true, false, false, []ToolDef{{Name: "shell"}}, nil, []string{"function", "web_search"}},
		{"no local tools, no generation", "https://api.openai.com/v1", false, false, true, nil, nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := &captureTransport{}
			var p Provider = &Responses{Client: Client{BaseURL: tc.base, APIKey: "fake", Model: "gpt-5.5", HTTPClient: &http.Client{Transport: tr}}, WebSearch: tc.search}
			if tc.wrap {
				p = WithoutHostedSearch(p)
			}
			ctx := context.Background()
			if tc.gen {
				ctx = WithImageGen(ctx, ImageGen{Model: "gpt-image-2.5-flare", Save: save})
			}
			if _, err := p.Chat(ctx, Request{HostedSearch: tc.search, Tools: tc.tools}, nil); err != nil {
				t.Fatal(err)
			}
			input, _ := json.Marshal(tr.bodies[0]["input"])
			if guided := strings.Contains(string(input), "自动挂到本轮最终回复气泡") && strings.Contains(string(input), "真实路径"); guided != (len(tc.want) > 0) {
				t.Fatalf("image delivery guidance mounted=%v, tools=%v", guided, tc.want)
			}
			got := imageTools(tr.bodies[0])
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("image tools %v want %v", got, tc.want)
			}
			if types := toolTypes(tr.bodies[0]); !reflect.DeepEqual(types, tc.allTypes) {
				t.Fatalf("tools %v want %v", types, tc.allTypes)
			}
		})
	}
}

// imageStream mimics a real stream: added, a progress event, done carrying the
// full base64 result, then completed repeating the same item.
func imageStream(result string) string {
	item := `{"type":"image_generation_call","id":"ig_1","status":"completed","revised_prompt":"一只猫","result":"` + result + `"}`
	return `data: {"type":"response.output_item.added","output_index":0,"item":{"type":"image_generation_call","id":"ig_1","status":"in_progress"}}

data: {"type":"response.image_generation_call.generating","item_id":"ig_1"}

data: {"type":"response.output_item.done","output_index":0,"item":` + item + `}

data: {"type":"response.output_text.delta","delta":"画好了"}

data: {"type":"response.completed","response":{"status":"completed","output":[` + item + `,{"type":"message","id":"m1","content":[{"type":"output_text","text":"画好了"}]}]}}

`
}

func TestResponsesSavesImageGenerationCall(t *testing.T) {
	// Over 1 MiB of base64 on one line: the image line cap must allow it.
	png := fakePNG(1 << 20)
	b64 := base64.StdEncoding.EncodeToString(png)
	for _, base := range []string{"https://api.openai.com/v1", "https://api.x.ai/v1"} {
		t.Run(base, func(t *testing.T) {
			tr := &sseTransport{stream: imageStream(b64)}
			var saved [][]byte
			ctx := WithImageGen(context.Background(), ImageGen{Model: "gpt-image-2.5-sunburst", Save: func(_ context.Context, b []byte) (string, error) {
				saved = append(saved, b)
				return fmt.Sprintf("generated/%d.png", len(saved)), nil
			}})
			var seen []ContentPart
			ctx = WithPartObserver(ctx, func(p ContentPart) { seen = append(seen, p) })
			c := &Responses{Client: Client{BaseURL: base, APIKey: "fake", Model: "m", HTTPClient: &http.Client{Transport: tr}}}
			msg, err := c.Chat(ctx, Request{Tools: []ToolDef{{Name: "shell"}}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(saved) != 1 || string(saved[0]) != string(png) {
				t.Fatalf("saved %d images", len(saved))
			}
			first := msg.Content[0]
			if first.Type != PartHosted || first.Hosted.Name != "image_generation" || first.Hosted.Status != "completed" || first.Hosted.Query != "一只猫" {
				t.Fatalf("image part %+v %+v", first, first.Hosted)
			}
			if !strings.Contains(first.Text, "generated/1.png") || len(first.VendorRaw) != 0 {
				t.Fatalf("image part text %q raw %d", first.Text, len(first.VendorRaw))
			}
			if !strings.Contains(first.Text, "已挂本条回复") {
				t.Fatalf("receipt lacks truthful delivery instructions: %q", first.Text)
			}
			// The bytes stay out of the record and out of the replayed request.
			raw, _ := json.Marshal(msg)
			if strings.Contains(string(raw), b64[:64]) {
				t.Fatal("base64 kept in message")
			}
			input, err := responseInput([]Message{msg})
			if err != nil {
				t.Fatal(err)
			}
			replay, _ := json.Marshal(input)
			if !strings.Contains(string(replay), "generated/1.png") || strings.Contains(string(replay), b64[:64]) {
				t.Fatalf("replay %s", replay[:min(len(replay), 400)])
			}
			done := false
			for _, p := range seen {
				if p.Hosted != nil && p.Hosted.Name == "image_generation" && strings.Contains(p.Text, "generated/1.png") {
					done = true
				}
				if strings.Contains(p.Text, b64[:64]) || strings.Contains(string(p.VendorRaw), b64[:64]) {
					t.Fatal("base64 reached the progress observer")
				}
			}
			if !done {
				t.Fatalf("no completed image progress in %+v", seen)
			}
			if got := imageTools(tr.bodies[0]); len(got) != 1 || (strings.Contains(base, "x.ai") && got[0]["model"] != nil) {
				t.Fatalf("image tool %v", got)
			}
		})
	}
}

func TestResponsesImageGenerationFailuresAreHonest(t *testing.T) {
	ok := base64.StdEncoding.EncodeToString(fakePNG(64))
	cases := []struct {
		name   string
		item   string
		gen    bool
		save   error
		expect string
	}{
		{"bad base64", `{"type":"image_generation_call","id":"ig","status":"completed","result":"!!!"}`, true, nil, "图片数据无法解码"},
		{"vendor returned nothing", `{"type":"image_generation_call","id":"ig","status":"completed"}`, true, nil, "厂商未返回图片"},
		{"vendor failed", `{"type":"image_generation_call","id":"ig","status":"failed"}`, true, nil, "厂商未返回图片"},
		{"save refused", `{"type":"image_generation_call","id":"ig","status":"completed","result":"` + ok + `"}`, true, errors.New("disk full"), "保存失败: disk full"},
		{"no workspace", `{"type":"image_generation_call","id":"ig","status":"completed","result":"` + ok + `"}`, false, nil, "没有可写的工作区"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			if tc.gen {
				ctx = WithImageGen(ctx, ImageGen{Save: func(context.Context, []byte) (string, error) {
					if tc.save != nil {
						return "", tc.save
					}
					return "generated/a.png", nil
				}})
			}
			stream := `data: {"type":"response.completed","response":{"status":"completed","output":[` + tc.item + `]}}

`
			msg, err := readResponses(ctx, strings.NewReader(stream), nil)
			if err != nil {
				t.Fatal(err)
			}
			p := msg.Content[0]
			if p.Hosted == nil || p.Hosted.Status != "failed" || !strings.Contains(p.Text, "厂商生图失败") || !strings.Contains(p.Text, tc.expect) {
				t.Fatalf("part %+v %+v", p, p.Hosted)
			}
			if strings.Contains(p.Text, "已保存") {
				t.Fatal("claimed a save")
			}
		})
	}
}

func TestResponsesImageGenerationCapsPerResponse(t *testing.T) {
	b64 := base64.StdEncoding.EncodeToString(fakePNG(64))
	var items []string
	for i := 0; i <= maxImagesPerResponse; i++ {
		items = append(items, fmt.Sprintf(`{"type":"image_generation_call","id":"ig_%d","status":"completed","result":"%s"}`, i, b64))
	}
	n := 0
	ctx := WithImageGen(context.Background(), ImageGen{Save: func(context.Context, []byte) (string, error) {
		n++
		return fmt.Sprintf("generated/%d.png", n), nil
	}})
	stream := `data: {"type":"response.completed","response":{"status":"completed","output":[` + strings.Join(items, ",") + `]}}

`
	msg, err := readResponses(ctx, strings.NewReader(stream), nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != maxImagesPerResponse {
		t.Fatalf("saved %d", n)
	}
	if last := msg.Content[len(msg.Content)-1]; !strings.Contains(last.Text, "单次回复最多保存") {
		t.Fatalf("last %+v", last)
	}
}

func TestOversizedLineStillRefusedWithoutImageGeneration(t *testing.T) {
	stream := `data: {"type":"response.output_item.done","output_index":0,"item":{"type":"image_generation_call","id":"ig","status":"completed","result":"` + strings.Repeat("A", 1<<20) + `"}}

`
	if _, err := readResponses(context.Background(), strings.NewReader(stream), nil); err == nil {
		t.Fatal("1 MiB line accepted without image generation")
	}
}

func TestImageGenerationTerminalShapes(t *testing.T) {
	b64 := base64.StdEncoding.EncodeToString(fakePNG(64))
	for _, tc := range []struct {
		name, stream string
		wantSaved    int
		wantText     string
	}{
		{"done only", `data: {"type":"response.output_item.done","output_index":0,"item":{"type":"image_generation_call","id":"ig","status":"completed","result":"` + b64 + `"}}

data: {"type":"response.completed","response":{"status":"completed"}}

`, 1, "generated/a.png"},
		{"terminal omits result", `data: {"type":"response.output_item.done","output_index":0,"item":{"type":"image_generation_call","id":"ig","status":"completed","result":"` + b64 + `"}}

data: {"type":"response.completed","response":{"status":"completed","output":[{"type":"image_generation_call","id":"ig","status":"completed"}]}}

`, 1, "generated/a.png"},
		{"unfinished final", `data: {"type":"response.completed","response":{"status":"completed","output":[{"type":"image_generation_call","id":"ig","status":"in_progress"}]}}

`, 0, "厂商生图失败"},
		{"untrusted path", `data: {"type":"response.completed","response":{"status":"completed","output":[{"type":"image_generation_call","id":"ig","status":"completed","xbot_path":"generated/fiction.png"}]}}

`, 0, "厂商生图失败"},
		{"failed with partial image", `data: {"type":"response.completed","response":{"status":"completed","output":[{"type":"image_generation_call","id":"ig","status":"failed","result":"` + b64 + `"}]}}

`, 0, "厂商生图失败"},
		{"incomplete with partial image", `data: {"type":"response.completed","response":{"status":"completed","output":[{"type":"image_generation_call","id":"ig","status":"incomplete","result":"` + b64 + `"}]}}

`, 0, "厂商生图失败"},
		{"unfinished with partial image", `data: {"type":"response.completed","response":{"status":"completed","output":[{"type":"image_generation_call","id":"ig","status":"in_progress","result":"` + b64 + `"}]}}

`, 0, "厂商生图失败"},
		{"terminal failure overrides cached save", `data: {"type":"response.output_item.done","output_index":0,"item":{"type":"image_generation_call","id":"ig","status":"completed","result":"` + b64 + `"}}

data: {"type":"response.completed","response":{"status":"completed","output":[{"type":"image_generation_call","id":"ig","status":"failed","result":"` + b64 + `"}]}}

`, 1, "厂商生图失败"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			saved := 0
			ctx := WithImageGen(context.Background(), ImageGen{Save: func(context.Context, []byte) (string, error) { saved++; return "generated/a.png", nil }})
			msg, err := readResponses(ctx, strings.NewReader(tc.stream), nil)
			if err != nil {
				t.Fatal(err)
			}
			if saved != tc.wantSaved || !strings.Contains(msg.Text(), tc.wantText) {
				t.Fatalf("saved=%d text=%s", saved, msg.PortableText())
			}
		})
	}
}

func TestImageGenerationMissingIDsStillBounded(t *testing.T) {
	b64 := base64.StdEncoding.EncodeToString(fakePNG(64))
	item := `{"type":"image_generation_call","status":"completed","result":"` + b64 + `"}`
	items := make([]string, maxImagesPerResponse+1)
	for i := range items {
		items[i] = item
	}
	stream := `data: {"type":"response.completed","response":{"status":"completed","output":[` + strings.Join(items, ",") + `]}}` + "\n\n"
	saved := 0
	ctx := WithImageGen(context.Background(), ImageGen{Save: func(context.Context, []byte) (string, error) { saved++; return "generated/a.png", nil }})
	msg, err := readResponses(ctx, strings.NewReader(stream), nil)
	if err != nil {
		t.Fatal(err)
	}
	if saved != maxImagesPerResponse || !strings.Contains(msg.PortableText(), "单次回复最多保存") {
		t.Fatalf("saved=%d text=%s", saved, msg.PortableText())
	}
}

func TestImageGenerationAttachmentFailureKeepsPathAndReceipt(t *testing.T) {
	calls := 0
	type requestKey struct{}
	ctx := context.WithValue(context.Background(), requestKey{}, "reply")
	ctx = WithImageGen(ctx, ImageGen{Save: func(ctx context.Context, _ []byte) (string, error) {
		calls++
		if ctx.Value(requestKey{}) != "reply" {
			t.Fatal("save lost the request context")
		}
		return "generated/real.png", errors.New("at most 4 pictures per reply")
	}})
	var receipts []string
	ctx = WithPartObserver(ctx, func(p ContentPart) {
		if strings.Contains(p.Text, "generated/real.png") {
			receipts = append(receipts, p.Text)
		}
	})
	msg, err := readResponses(ctx, strings.NewReader(imageStream(base64.StdEncoding.EncodeToString(fakePNG(64)))), nil)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("save calls = %d", calls)
	}
	receipts = append(receipts, msg.Content[0].Text)
	if !strings.Contains(msg.Text(), "挂图失败") || !strings.Contains(msg.Text(), "generated/real.png") {
		t.Fatalf("failure hidden from reply: %s", msg.Text())
	}
	if len(receipts) < 3 {
		t.Fatalf("missing done/completed receipts: %v", receipts)
	}
	for _, receipt := range receipts {
		if !strings.Contains(receipt, "已保存到工作区 generated/real.png") || !strings.Contains(receipt, "挂图失败: at most 4 pictures per reply") || !strings.Contains(receipt, "文件仍保留") || strings.Contains(receipt, "已挂本条回复") || strings.Contains(receipt, "保存失败") {
			t.Fatalf("dishonest receipt: %s", receipt)
		}
	}
	input, err := responseInput([]Message{msg})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(input)
	if !strings.Contains(string(raw), "generated/real.png") || !strings.Contains(string(raw), "挂图失败") {
		t.Fatalf("replay lost partial success: %s", raw)
	}
}
