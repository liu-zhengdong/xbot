package web

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/compact"
	"xbot/internal/kernel"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/mockopenai"
	"xbot/internal/provider"
)

func TestDoneTextSkipsPreamble(t *testing.T) {
	root := t.TempDir()
	model := httptest.NewServer(mockopenai.NewMux(t.TempDir()))
	defer model.Close()
	client := &provider.Client{BaseURL: model.URL + "/v1", APIKey: "test", Model: "mock", HTTPClient: model.Client()}
	s := &Server{Root: root, Owner: "u1"}
	tok, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := bot.Create(root, "ada", "persona", []string{link.NameWeb, link.NameMemory})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	s.NewKernel = func(b *bot.Bot) (*kernel.Kernel, error) {
		return &kernel.Kernel{
			Bot: b, Provider: client, Memory: memory.Open(root), MaxSteps: 4,
			Budget: compact.DefaultBudget(),
			Links:  []link.Link{link.Web{}, link.Memory{}},
		}, nil
	}
	ui := httptest.NewServer(muxOf(s))
	defer ui.Close()
	ch := postSSE(t, ui.URL, loginCookie(t, ui.URL, tok), "PREAMBLE_ZH")
	var done string
	for ev := range ch {
		if ev.name == "done" {
			done = ev.data
			break
		}
	}
	var body struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(done), &body); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body.Text, "I'll remember") || !strings.Contains(body.Text, "已记住") {
		t.Fatalf("done %s", done)
	}
}
