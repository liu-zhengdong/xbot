package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/config"
	"xbot/internal/exec"
	"xbot/internal/kernel"
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
	s := &Server{Root: root, Owner: "u1"}
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
	if code := call(`{"provider":"anthropic","model":"claude-sonnet-4-5"}`); code != 400 {
		t.Fatalf("unimplemented protocol accepted %d", code)
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
