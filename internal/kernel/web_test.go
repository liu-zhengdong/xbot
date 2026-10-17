package kernel

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/config"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

func TestWebSearchAndFetchStayOffTheNetwork(t *testing.T) {
	b := testBot(t, link.NameTerminal, link.NameTools)
	dir := t.TempDir()
	tl := link.NewToolsLinkAt(dir, "")
	when := time.Date(2026, 10, 17, 3, 4, 5, 0, time.UTC)
	for _, tool := range tl.Tools() {
		switch f := tool.(type) {
		case *tools.WebSearch:
			f.Now = func() time.Time { return when }
			f.Backend = tools.FuncSearch(func(context.Context, string, int) ([]tools.Hit, error) {
				return []tools.Hit{{Title: "Result", URL: "https://example.com/a", Snippet: "a snippet"}}, nil
			})
		case *tools.WebFetch:
			f.Now = func() time.Time { return when }
			f.Fetcher = fetchPage{text: strings.Repeat("page ", 2000), url: "https://example.com/a", title: "A page"}
		}
	}
	p := &scripted{resps: []provider.Message{
		toolCall("c1", "web_search", `{"query":"xbot","n":1}`),
		toolCall("c2", "web_fetch", `{"url":"https://example.com/a"}`),
		{Role: provider.RoleAssistant, Content: "done"},
	}}
	k := &Kernel{
		Bot: b, Provider: p, Memory: memory.NopStore{}, MaxSteps: 4,
		Links: []link.Link{link.Terminal{}, tl},
		Now:   func() time.Time { return when },
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("owner", "search then fetch", when)); err != nil {
		t.Fatal(err)
	}
	search := toolNamed(t, b, "web_search", false)
	fetch, blob := toolNamedBlob(t, b, "web_fetch")
	if !strings.Contains(search, `source="web_search"`) || !strings.Contains(search, "https://example.com/a") {
		t.Fatalf("search %q", search)
	}
	if blob == "" || !strings.Contains(fetch, `source="web_fetch"`) || len(fetch) > 600 {
		t.Fatalf("fetch blob %q preview %q", blob, fetch)
	}
}

func TestViewImageErrorsWithoutVisionAndSendsPartsWithIt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.png"), tinyPNG(), 0600); err != nil {
		t.Fatal(err)
	}
	b := testBot(t, link.NameTerminal, link.NameTools)
	if err := b.SetBinding("deepseek", "deepseek-v4-pro"); err != nil {
		t.Fatal(err)
	}
	p := &scripted{resps: []provider.Message{
		toolCall("c1", "view_image", `{"path":"a.png"}`),
		{Role: provider.RoleAssistant, Content: "cannot see"},
	}}
	k := &Kernel{
		Bot: b, Provider: p, Memory: memory.NopStore{}, MaxSteps: 3,
		Links:   []link.Link{link.Terminal{}, link.NewToolsLinkAt(dir, "")},
		WorkDir: dir,
		Now:     func() time.Time { return time.Unix(10, 0).UTC() },
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("owner", "look", time.Unix(10, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(toolNamed(t, b, "view_image", false), "deepseek-v4-pro does not support vision") {
		t.Fatal(toolNamed(t, b, "view_image", false))
	}
	for _, req := range p.reqs {
		for _, m := range req.Messages {
			if len(m.Images) != 0 {
				t.Fatal("non-vision model was sent an image")
			}
		}
	}

	flash := testBot(t, link.NameTerminal, link.NameTools)
	if err := flash.SetBinding("deepseek", "deepseek-flash"); err != nil {
		t.Fatal(err)
	}
	p = &scripted{resps: []provider.Message{
		toolCall("c1", "view_image", `{"path":"a.png"}`),
		{Role: provider.RoleAssistant, Content: "saw it"},
	}}
	k = &Kernel{
		Bot: flash, Provider: p, Memory: memory.NopStore{}, MaxSteps: 3,
		Links:   []link.Link{link.Terminal{}, link.NewToolsLinkAt(dir, "")},
		WorkDir: dir,
		Now:     func() time.Time { return time.Unix(11, 0).UTC() },
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("owner", "look", time.Unix(11, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(toolNamed(t, flash, "view_image", false), "does not support vision") {
		t.Fatal(toolNamed(t, flash, "view_image", false))
	}
	var saw bool
	if len(p.reqs) < 2 {
		t.Fatalf("requests %d", len(p.reqs))
	}
	for _, m := range p.reqs[1].Messages {
		if m.Name == "view_image" && len(m.Images) == 1 && m.Images[0].MIME == "image/png" {
			saw = true
		}
	}
	if !saw {
		t.Fatal("vision model did not receive the image")
	}

	off := false
	if err := config.Add(flash.Root, config.Provider{
		Name: "deepseek", BaseURL: "https://api.deepseek.com/v1", APIKeyEnv: "DEEPSEEK_API_KEY",
	}, config.ModelSpec{Name: "deepseek-flash", Context: 1000, Vision: &off}); err != nil {
		t.Fatal(err)
	}
	p = &scripted{resps: []provider.Message{
		toolCall("c1", "view_image", `{"path":"a.png"}`),
		{Role: provider.RoleAssistant, Content: "nope"},
	}}
	k = &Kernel{
		Bot: flash, Provider: p, Memory: memory.NopStore{}, MaxSteps: 3,
		Links:   []link.Link{link.Terminal{}, link.NewToolsLinkAt(dir, "")},
		WorkDir: dir,
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("owner", "again", time.Unix(12, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(toolNamed(t, flash, "view_image", true), "does not support vision") {
		t.Fatal(toolNamed(t, flash, "view_image", true))
	}
}

type fetchPage struct {
	text, url, title string
}

func (f fetchPage) Fetch(context.Context, string) (tools.Page, error) {
	return tools.Page{URL: f.url, Status: 200, Title: f.title, Text: f.text}, nil
}

func toolNamed(t *testing.T, b *bot.Bot, name string, last bool) string {
	t.Helper()
	text, _ := toolNamedBlob(t, b, name)
	if !last {
		entries, err := b.Entries()
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if e.Message.Name == name {
				return e.Message.Content
			}
		}
		t.Fatalf("no %s result", name)
	}
	return text
}

func toolNamedBlob(t *testing.T, b *bot.Bot, name string) (string, string) {
	t.Helper()
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	var text, blob string
	var found bool
	for _, e := range entries {
		if e.Message.Name == name {
			text = e.Message.Content
			blob = e.Blob
			found = true
		}
	}
	if !found {
		t.Fatalf("no %s result", name)
	}
	return text, blob
}

func tinyPNG() []byte {
	return []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0, 'I', 'H', 'D', 'R'}
}
