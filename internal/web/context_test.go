package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/compact"
	"xbot/internal/inbox"
	"xbot/internal/kernel"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

// ctxModel is a scripted provider: one assistant step with a tool call, then a
// text answer. No network.
type ctxModel struct {
	mu sync.Mutex
	n  int
}

func (m *ctxModel) Chat(_ context.Context, _ provider.Request, onDelta func(string)) (provider.Message, error) {
	m.mu.Lock()
	m.n++
	n := m.n
	m.mu.Unlock()
	if n == 1 {
		return provider.Message{
			Role: provider.RoleAssistant,
			ToolCalls: []provider.ToolCall{{
				ID:       "c1",
				Type:     "function",
				Function: provider.FunctionCall{Name: "ctxprobe", Arguments: `{"q":"x"}`},
			}},
			Usage: provider.TokenUsage{PromptTokens: 4242, CompletionTokens: 7, Reported: true},
		}, nil
	}
	text := "done"
	if onDelta != nil {
		onDelta(text)
	}
	return provider.Message{
		Role:    provider.RoleAssistant,
		Content: text,
		Usage:   provider.TokenUsage{PromptTokens: 4500, CompletionTokens: 2, Reported: true},
	}, nil
}

// ctxLink exposes one tool so a turn has a real tool result to inspect.
type ctxLink struct{}

func (ctxLink) Name() string { return "ctxlink" }

func (ctxLink) Tools() []tools.Tool { return []tools.Tool{ctxProbe{}} }

type ctxProbe struct{}

func (ctxProbe) Name() string        { return "ctxprobe" }
func (ctxProbe) Description() string { return "return a short string" }
func (ctxProbe) Parameters() any     { return map[string]any{"type": "object"} }

func (ctxProbe) Confirm(json.RawMessage) (bool, string, error) { return false, "", nil }

func (ctxProbe) Run(context.Context, json.RawMessage) (string, error) {
	return "probe output: " + strings.Repeat("ok ", 40), nil
}

// contextFixture builds a bot whose kernel made one real turn: a tool call, the
// tool result, then the answer. The provider is scripted, so no model is called.
func contextFixture(t *testing.T) (*Server, string, *bot.Bot, *kernel.Kernel) {
	t.Helper()
	root := t.TempDir()
	s := &Server{Root: root, Owner: "u1"}
	tok, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := bot.Create(root, "idlers", "You are a test bot.", []string{link.NameTerminal, link.NameWeb})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	model := &ctxModel{}
	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	k := &kernel.Kernel{
		Bot:       b,
		Provider:  model,
		Memory:    memory.Open(root),
		MaxSteps:  4,
		Inbox:     box,
		Links:     []link.Link{link.Terminal{}, ctxLink{}},
		Compactor: compact.LLM{P: model},
		Approve:   func(context.Context, string, string) (bool, error) { return true, nil },
		Budget:    kernel.BudgetFor(b.Config()),
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "查一下上下文", time.Now())); err != nil {
		t.Fatal(err)
	}
	return s, tok, b, k
}

// contextServer wires the kernel in the way serve does, so the handler reads a
// live snapshot rather than a fabricated one.
func contextServer(s *Server, b *bot.Bot, k *kernel.Kernel) *Server {
	s.NewKernel = func(*bot.Bot) (*kernel.Kernel, error) { return k, nil }
	s.mu.Lock()
	if s.kernels == nil {
		s.kernels = map[string]*kernel.Kernel{}
	}
	s.kernels[b.Name()] = k
	s.mu.Unlock()
	return s
}

// TestContextAPIShape pins the fields the panel reads.
func TestContextAPIShape(t *testing.T) {
	s, tok, b, k := contextFixture(t)
	s = contextServer(s, b, k)
	srv := httptest.NewServer(muxOf(s))
	defer srv.Close()
	cookie := loginCookie(t, srv.URL, tok)

	var got kernel.ContextSnap
	raw := getAuth(t, srv.URL+"/api/bots/"+b.Name()+"/context", cookie)
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("%v: %s", err, raw)
	}
	if got.Bot != b.Name() {
		t.Fatalf("bot %q, want %q", got.Bot, b.Name())
	}
	if got.Target <= 0 || got.Ceiling <= got.Target {
		t.Fatalf("target %d ceiling %d: want two lines, the soft one below the hard one", got.Target, got.Ceiling)
	}
	if len(got.Requests) == 0 {
		t.Fatal("no requests recorded: the turn did not reach the provider callback")
	}
	last := got.Requests[len(got.Requests)-1]
	if last.Step < 1 {
		t.Fatalf("step %d, want 1 or more", last.Step)
	}
	if last.Prompt <= 0 {
		t.Fatalf("prompt %d, want a positive estimate", last.Prompt)
	}
	if last.At == "" {
		t.Fatal("no timestamp on the request")
	}
	if len(last.Layers) == 0 || last.Layers[0].Key != "sys" {
		t.Fatalf("layers %+v, want a leading sys band", last.Layers)
	}
	if got.Detail == nil || len(got.Detail.ToolDefs) == 0 {
		t.Fatalf("detail %+v: the panel has nothing to drill into", got.Detail)
	}
	if !strings.Contains(string(raw), `"turn_steps":`) || !strings.Contains(string(raw), `"steps":[`) {
		t.Fatalf("missing the fields the request picker reads: %s", raw)
	}
}

// TestContextAPIToolBand covers the figure the panel adds on its own: tool
// definitions are sent but the usage record leaves them out of its prompt
// count, so this band must come from the request instead.
func TestContextAPIToolBand(t *testing.T) {
	s, tok, b, k := contextFixture(t)
	s = contextServer(s, b, k)
	srv := httptest.NewServer(muxOf(s))
	defer srv.Close()
	cookie := loginCookie(t, srv.URL, tok)

	var got kernel.ContextSnap
	if err := json.Unmarshal(getAuth(t, srv.URL+"/api/bots/"+b.Name()+"/context", cookie), &got); err != nil {
		t.Fatal(err)
	}
	last := got.Requests[len(got.Requests)-1]
	if last.Tools <= 0 {
		t.Fatalf("tools %d, want the tool definitions counted", last.Tools)
	}
	sum := last.Tools
	for _, l := range last.Layers {
		sum += l.Tokens
	}
	if sum != last.Prompt {
		t.Fatalf("layers+tools %d != prompt %d", sum, last.Prompt)
	}
}

// TestContextAPIToolResultPresent checks the current turn's steps carry the
// tool call with its result folded in, which is what an audit looks for.
func TestContextAPIToolResultPresent(t *testing.T) {
	s, tok, b, k := contextFixture(t)
	s = contextServer(s, b, k)
	srv := httptest.NewServer(muxOf(s))
	defer srv.Close()
	cookie := loginCookie(t, srv.URL, tok)

	var got kernel.ContextSnap
	if err := json.Unmarshal(getAuth(t, srv.URL+"/api/bots/"+b.Name()+"/context", cookie), &got); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, st := range got.Detail.Steps {
		if st.Tool == "ctxprobe" && strings.Contains(st.Preview, "probe output") {
			found = true
			if st.What != "x" {
				t.Fatalf("what %q, want the call's argument", st.What)
			}
			if st.Tokens <= 0 {
				t.Fatal("the step has no estimate")
			}
		}
	}
	if !found {
		t.Fatalf("no tool step with its result: %+v", got.Detail.Steps)
	}
	if last := got.Requests[len(got.Requests)-1]; last.TurnSteps != len(got.Detail.Steps)+got.Detail.SkippedSteps {
		t.Fatalf("turn_steps %d, steps %d", last.TurnSteps, len(got.Detail.Steps))
	}
}

// TestContextAPIRedacts checks that the snapshot goes through the vault
// replacer. The rule is the one the transcript and export already follow.
func TestContextAPIRedacts(t *testing.T) {
	restore := bot.SetRedactText(func(s string) string {
		return strings.ReplaceAll(s, "probe output", "[redacted]")
	})
	defer restore()

	s, tok, b, k := contextFixture(t)
	s = contextServer(s, b, k)
	srv := httptest.NewServer(muxOf(s))
	defer srv.Close()
	cookie := loginCookie(t, srv.URL, tok)

	raw := getAuth(t, srv.URL+"/api/bots/"+b.Name()+"/context", cookie)
	if strings.Contains(string(raw), "probe output") {
		t.Fatalf("the tool result reached the snapshot unredacted: %s", raw)
	}
	if !strings.Contains(string(raw), "[redacted]") {
		t.Fatalf("redaction did not apply: %s", raw)
	}
}

// TestContextAPIEmpty covers a bot with no snapshot: the answer is an empty
// list, not an error, because "has not woken yet" is a normal state.
func TestContextAPIEmpty(t *testing.T) {
	root := t.TempDir()
	s := &Server{Root: root, Owner: "u1"}
	tok, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := bot.Create(root, "fresh", "p", []string{link.NameTerminal, link.NameWeb})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(muxOf(s))
	defer srv.Close()
	cookie := loginCookie(t, srv.URL, tok)

	raw := getAuth(t, srv.URL+"/api/bots/fresh/context", cookie)
	if !strings.Contains(string(raw), `"requests":[]`) {
		t.Fatalf("want an empty requests list, got %s", raw)
	}
}

// TestContextAPIRequiresLogin keeps the snapshot behind the same gate as the
// transcript: it carries the assembled prompt, memory included.
func TestContextAPIRequiresLogin(t *testing.T) {
	s, _, b, k := contextFixture(t)
	s = contextServer(s, b, k)
	srv := httptest.NewServer(muxOf(s))
	defer srv.Close()
	res, err := http.Get(srv.URL + "/api/bots/" + b.Name() + "/context")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", res.StatusCode)
	}
}
