package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVisionContentIsImageURLParts(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("body: %v", err)
		}
		if strings.Contains(string(raw), "search-key") {
			t.Error("unexpected secret in body")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, APIKey: "secret", Model: "deepseek-flash", HTTPClient: srv.Client()}
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	_, err := c.Chat(context.Background(), Request{Messages: []Message{
		{Role: RoleUser, Content: TextContent("ping")},
		{Role: RoleTool, Name: "view_image", Images: []Image{{MIME: "image/png", Data: png}}, Content: ToolResult("c1", "path: a.png\nmime: image/png\nbytes: 8")},
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	msgs, _ := body["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("messages %#v", body["messages"])
	}
	first := msgs[0].(map[string]any)
	if first["content"] != "ping" {
		t.Fatalf("text content %#v", first["content"])
	}
	second := msgs[1].(map[string]any)
	parts, _ := second["content"].([]any)
	if len(parts) != 2 {
		t.Fatalf("parts %#v", second["content"])
	}
	text := parts[0].(map[string]any)
	if text["type"] != "text" || !strings.Contains(text["text"].(string), "a.png") {
		t.Fatalf("text part %#v", text)
	}
	img := parts[1].(map[string]any)
	if img["type"] != "image_url" {
		t.Fatalf("image part %#v", img)
	}
	url := img["image_url"].(map[string]any)["url"].(string)
	if !strings.HasPrefix(url, "data:image/png;base64,") {
		t.Fatalf("url %q", url)
	}
}
