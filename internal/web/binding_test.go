package web

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/compact"
	"xbot/internal/config"
	"xbot/internal/exec"
	"xbot/internal/kernel"
	"xbot/internal/link"
	"xbot/internal/provider"
)

func TestBindingAPIProtocolsPersistenceBusyAndPublicParts(t *testing.T) {
	t.Setenv("XBOT_B2_FAKE_KEY", "not-a-real-key")
	root := t.TempDir()
	b, err := bot.Create(root, "ada", "persona", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []config.Provider{{Name: "openai", Protocol: config.Responses, BaseURL: "http://localhost/v1", APIKeyEnv: "XBOT_B2_FAKE_KEY"}, {Name: "anthropic", Protocol: config.AnthropicMessages, BaseURL: "http://localhost/v1", APIKeyEnv: "XBOT_B2_FAKE_KEY"}} {
		m := config.ModelSpec{Name: "gpt-5.5", Context: 1000000}
		if p.Name == "anthropic" {
			m.Name = "claude-sonnet-4-5"
		}
		if err := config.Add(root, p, m); err != nil {
			t.Fatal(err)
		}
	}
	// Replacing an unavailable old client with a same/larger window needs no
	// model construction and must remain usable to repair credentials.
	s := &Server{Root: root, Owner: "u1", NewKernel: func(*bot.Bot) (*kernel.Kernel, error) { return nil, errors.New("old client unavailable") }}
	token, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	ui := httptest.NewServer(muxOf(s))
	defer ui.Close()
	cookie := loginCookie(t, ui.URL, token)
	call := func(body string) int {
		t.Helper()
		r, _ := http.NewRequest("PUT", ui.URL+"/api/bots/ada/binding", strings.NewReader(body))
		r.AddCookie(cookie)
		res, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		io.Copy(io.Discard, res.Body)
		return res.StatusCode
	}
	if code := call(`{"provider":"anthropic","model":"claude-sonnet-4-5"}`); code != 200 {
		t.Fatalf("Messages binding rejected %d", code)
	}
	lock, err := b.LockHandle()
	if err != nil {
		t.Fatal(err)
	}
	if code := call(`{"provider":"openai","model":"gpt-5.5"}`); code != 409 {
		t.Fatalf("busy binding %d", code)
	}
	lock.Release()
	if code := call(`{"provider":"openai","model":"gpt-5.5"}`); code != 200 {
		t.Fatalf("binding %d", code)
	}
	saved, err := bot.Load(root, "ada")
	if err != nil || saved.Config().Model != "gpt-5.5" {
		t.Fatalf("not persisted %v", err)
	}
	data := getAuth(t, ui.URL+"/api/providers", cookie)
	if strings.Contains(string(data), "not-a-real-key") || !strings.Contains(string(data), `"protocol":"responses"`) || !strings.Contains(string(data), `"hosted_web_search":true`) {
		t.Fatalf("bad public capabilities %s", data)
	}
	msg := provider.Message{Content: []provider.ContentPart{{Type: provider.PartReasoning, VendorRaw: json.RawMessage(`{"encrypted_content":"private-reasoning"}`)}, {Type: provider.PartHosted, VendorRaw: json.RawMessage(`{"private":"raw"}`), Hosted: &provider.HostedStep{ID: "ws1", Name: "web_search", Status: "completed"}}, {Type: provider.PartCitation, Citation: &provider.Citation{URL: "https://example.org", Title: "来源"}}}}
	parts := publicParts(msg)
	raw, _ := json.Marshal(parts)
	if len(parts) != 2 || strings.Contains(string(raw), "vendor_raw") || strings.Contains(string(raw), "reasoning") {
		t.Fatalf("private raw exposed %s", raw)
	}
}

type bindingTasks struct{ tasks []exec.Task }

func (b *bindingTasks) Tasks() ([]exec.Task, error) { return b.tasks, nil }
func (*bindingTasks) Start(context.Context, exec.StartRequest) (exec.Task, error) {
	return exec.Task{}, nil
}
func (*bindingTasks) Control(context.Context, string, exec.Control) error { return nil }
func (*bindingTasks) Events() <-chan exec.Event                           { return nil }

func TestBindingPreservesRunningAndQueuedExecutor(t *testing.T) {
	t.Setenv("XBOT_API_KEY", "fake")
	root := t.TempDir()
	b, err := bot.Create(root, "ada", "persona", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	jobs := &bindingTasks{}
	k := &kernel.Kernel{Bot: b, Exec: jobs}
	s := &Server{Root: root, Owner: "u1", kernels: map[string]*kernel.Kernel{"ada": k}}
	token, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	ui := httptest.NewServer(muxOf(s))
	defer ui.Close()
	cookie := loginCookie(t, ui.URL, token)
	for _, status := range []string{"running", "queued"} {
		jobs.tasks = []exec.Task{{ID: "ag_fixture", Status: status}}
		req, _ := http.NewRequest("PUT", ui.URL+"/api/bots/ada/binding", strings.NewReader(`{"provider":"","model":""}`))
		req.AddCookie(cookie)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 409 {
			t.Fatalf("%s executor orphaned: %d", status, res.StatusCode)
		}
		if s.kernels["ada"] != k {
			t.Fatal("running executor was replaced")
		}
	}
}

func TestKernelRebuildReadsCurrentBindingAfterStaleProfile(t *testing.T) {
	root := t.TempDir()
	b, err := bot.Create(root, "ada", "persona", nil)
	if err != nil {
		t.Fatal(err)
	}
	stale, err := bot.Load(root, "ada")
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetBinding("openai", "gpt-5.5"); err != nil {
		t.Fatal(err)
	}
	s := &Server{Root: root, NewKernel: func(b *bot.Bot) (*kernel.Kernel, error) { return &kernel.Kernel{Bot: b}, nil }}
	k, _, err := s.kernel(stale)
	if err != nil {
		t.Fatal(err)
	}
	if k.Bot.Config().Provider != "openai" || k.Bot.Config().Model != "gpt-5.5" {
		t.Fatalf("stale binding cached: %+v", k.Bot.Config())
	}
}

// A cold server must preflight too: no cached kernel is not an empty context.
func TestBindingSmallWindowRejectsColdOversize(t *testing.T) {
	t.Setenv("XBOT_B2_FAKE_KEY", "not-a-real-key")
	root := t.TempDir()
	b, err := bot.Create(root, "ada", "persona", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	for _, m := range []config.ModelSpec{{Name: "large", Context: 1000000}, {Name: "small", Context: 8000}} {
		if err := config.Add(root, config.Provider{Name: "p", BaseURL: "http://localhost/v1", APIKeyEnv: "XBOT_B2_FAKE_KEY"}, m); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.SetBinding("p", "large"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		if err := b.Append(bot.Entry{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent(strings.Repeat("history", 1000))}}); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{Root: root, Owner: "u1"}
	token, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	ui := httptest.NewServer(muxOf(s))
	defer ui.Close()
	cookie := loginCookie(t, ui.URL, token)
	req, _ := http.NewRequest("PUT", ui.URL+"/api/bots/ada/binding", strings.NewReader(`{"provider":"p","model":"small"}`))
	req.AddCookie(cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != 409 || !strings.Contains(string(body), "上下文仍超新窗口") {
		t.Fatalf("status %d body %s", res.StatusCode, body)
	}
	saved, err := bot.Load(root, "ada")
	if err != nil || saved.Config().Model != "large" {
		t.Fatal("failed switch changed binding")
	}
}

type bindingCompactor struct {
	b     *bot.Bot
	calls int
	old   bool
}

func (c *bindingCompactor) Compact(context.Context, string, []provider.Message) (string, error) {
	c.calls++
	saved, err := bot.Load(c.b.Root, c.b.Name())
	c.old = err == nil && saved.Config().Model == "large"
	return "kept decisions", nil
}

type bindingWindowModel struct{ calls int }

func (m *bindingWindowModel) Chat(_ context.Context, r provider.Request, _ func(string)) (provider.Message, error) {
	m.calls++
	_, ceiling := compact.Lines(40000)
	if compact.EstimateMessages(r.Messages) > ceiling {
		return provider.Message{}, provider.NewStatusError(400, "maximum context length")
	}
	return provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("ok")}, nil
}

func TestBindingSmallWindowCompactsBeforeSwitchAndFirstRequestFits(t *testing.T) {
	t.Setenv("XBOT_B2_FAKE_KEY", "not-a-real-key")
	root := t.TempDir()
	b, err := bot.Create(root, "ada", "persona", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	for _, m := range []config.ModelSpec{{Name: "large", Context: 1000000}, {Name: "small", Context: 40000}} {
		if err := config.Add(root, config.Provider{Name: "p", BaseURL: "http://localhost/v1", APIKeyEnv: "XBOT_B2_FAKE_KEY"}, m); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.SetBinding("p", "large"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 30; i++ {
		if err := b.Append(bot.Entry{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent(strings.Repeat("history ", 500))}}); err != nil {
			t.Fatal(err)
		}
	}
	c := &bindingCompactor{b: b}
	model := &bindingWindowModel{}
	s := &Server{Root: root, Owner: "u1", NewKernel: func(b *bot.Bot) (*kernel.Kernel, error) {
		return &kernel.Kernel{Bot: b, Compactor: c, Provider: model}, nil
	}}
	token, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	ui := httptest.NewServer(muxOf(s))
	defer ui.Close()
	cookie := loginCookie(t, ui.URL, token)
	req, _ := http.NewRequest("PUT", ui.URL+"/api/bots/ada/binding", strings.NewReader(`{"provider":"p","model":"small"}`))
	req.AddCookie(cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 || c.calls != 1 || !c.old {
		t.Fatalf("status=%d summaryCalls=%d old=%v body=%s", res.StatusCode, c.calls, c.old, body)
	}
	saved, err := bot.Load(root, "ada")
	if err != nil || saved.Config().Model != "small" {
		t.Fatal("new binding not saved")
	}
	k, _, err := s.kernel(saved)
	if err != nil {
		t.Fatal(err)
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "continue", time.Now())); err != nil {
		t.Fatal(err)
	}
	if model.calls != 1 {
		t.Fatalf("first request retried: calls=%d", model.calls)
	}
}
