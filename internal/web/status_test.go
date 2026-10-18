package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/kernel"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

func statusFixture(t *testing.T, n int) (*Server, string, []*bot.Bot) {
	t.Helper()
	root := t.TempDir()
	s := &Server{Root: root, Owner: "u1"}
	tok, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	var bots []*bot.Bot
	for i := 0; i < n; i++ {
		b, err := bot.Create(root, fmt.Sprintf("bot%d", i), "p", []string{link.NameTerminal, link.NameWeb})
		if err != nil {
			t.Fatal(err)
		}
		if err := b.SetOwners([]string{"u1"}); err != nil {
			t.Fatal(err)
		}
		if err := b.Append(bot.Entry{Time: time.Now(), Message: provider.Message{Role: provider.RoleAssistant, Content: "hello from " + b.Name()}}); err != nil {
			t.Fatal(err)
		}
		bots = append(bots, b)
	}
	return s, tok, bots
}

// stepLink exposes one tool so a real kernel turn records steps.
type stepLink struct{}

func (stepLink) Name() string        { return "steplink" }
func (stepLink) Tools() []tools.Tool { return []tools.Tool{stepTool{}} }

type stepTool struct{}

func (stepTool) Name() string        { return "webstep" }
func (stepTool) Description() string { return "one step" }
func (stepTool) Parameters() any     { return map[string]any{"type": "object"} }
func (stepTool) Confirm(args json.RawMessage) (bool, string, error) {
	var in struct {
		I int `json:"i"`
	}
	_ = json.Unmarshal(args, &in)
	return false, fmt.Sprintf("step %d", in.I), nil
}
func (stepTool) Run(context.Context, json.RawMessage) (string, error) { return "ok", nil }

// stepProvider asks for one webstep call per step until n, then answers.
type stepProvider struct {
	n, i int
}

func (p *stepProvider) Chat(_ context.Context, _ provider.Request, _ func(string)) (provider.Message, error) {
	p.i++
	if p.i > p.n {
		return provider.Message{Role: provider.RoleAssistant, Content: "done"}, nil
	}
	args := fmt.Sprintf(`{"i":%d}`, p.i)
	return provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
		{ID: fmt.Sprintf("c%d", p.i), Type: "function", Function: provider.FunctionCall{Name: "webstep", Arguments: args}},
	}}, nil
}

func TestBotStatusEndpointShapeAndSize(t *testing.T) {
	s, tok, _ := statusFixture(t, 5)
	ui := httptest.NewServer(muxOf(s))
	defer ui.Close()
	cookie := loginCookie(t, ui.URL, tok)
	raw := getAuth(t, ui.URL+"/api/bots/status", cookie)
	if len(raw) >= 2<<10 {
		t.Fatalf("status body %d bytes for 5 bots", len(raw))
	}
	var payload struct {
		Bots []kernel.BotStatus `json:"bots"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Bots) != 5 {
		t.Fatalf("bots %+v", payload.Bots)
	}
	for _, st := range payload.Bots {
		if st.State != kernel.StateIdle {
			t.Fatalf("state %q", st.State)
		}
		if !strings.HasPrefix(st.LastMessage, "hello from") {
			t.Fatalf("last %q", st.LastMessage)
		}
	}
}

func TestBotTodosEndpoint(t *testing.T) {
	s, tok, bots := statusFixture(t, 1)
	raw, err := json.Marshal(map[string]any{
		"items": []kernel.TodoItem{{ID: "t1", Content: "write the tests", Status: "in_progress"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kernel.TodoPath(bots[0]), raw, 0600); err != nil {
		t.Fatal(err)
	}
	ui := httptest.NewServer(muxOf(s))
	defer ui.Close()
	cookie := loginCookie(t, ui.URL, tok)
	body := getAuth(t, ui.URL+"/api/bots/"+bots[0].Name()+"/todos", cookie)
	var payload struct {
		Items []kernel.TodoItem `json:"items"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Items) != 1 || payload.Items[0].Content != "write the tests" || payload.Items[0].Status != "in_progress" {
		t.Fatalf("%+v", payload.Items)
	}
	statusBody := getAuth(t, ui.URL+"/api/bots/status", cookie)
	var status struct {
		Bots []kernel.BotStatus `json:"bots"`
	}
	if err := json.Unmarshal(statusBody, &status); err != nil {
		t.Fatal(err)
	}
	if len(status.Bots) != 1 || !strings.Contains(status.Bots[0].Todo, "write the tests") {
		t.Fatalf("status todo %+v", status.Bots)
	}
}

func TestBotStepsEndpoint(t *testing.T) {
	s, tok, bots := statusFixture(t, 1)
	p := &stepProvider{n: 25}
	k := &kernel.Kernel{
		Bot: bots[0], Provider: p, Memory: memory.NopStore{}, MaxSteps: 40,
		Links: []link.Link{link.Terminal{}, stepLink{}},
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "go", time.Now())); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.kernels = map[string]*kernel.Kernel{bots[0].Name(): k}
	s.gate = map[string]*sync.Mutex{bots[0].Name(): {}}
	s.mu.Unlock()
	ui := httptest.NewServer(muxOf(s))
	defer ui.Close()
	cookie := loginCookie(t, ui.URL, tok)
	raw := getAuth(t, ui.URL+"/api/bots/bot0/steps?n=5", cookie)
	var payload struct {
		Steps []kernel.Step `json:"steps"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Steps) != 5 {
		t.Fatalf("steps %d", len(payload.Steps))
	}
	if payload.Steps[4].Summary != "step 25" || payload.Steps[4].State != kernel.StepDone {
		t.Fatalf("last step %+v", payload.Steps[4])
	}
	// The cap holds even for the default request.
	def := getAuth(t, ui.URL+"/api/bots/bot0/steps", cookie)
	if n := strings.Count(string(def), `"tool":"webstep"`); n != statusStepsMax {
		t.Fatalf("default steps %d", n)
	}
	req, _ := http.NewRequest(http.MethodGet, ui.URL+"/api/bots/bot0/steps?n=0", nil)
	req.AddCookie(cookie)
	res, err := ui.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("n=0 %d", res.StatusCode)
	}
}

func TestBotStatusP95Latency5Bots(t *testing.T) {
	s, tok, _ := statusFixture(t, 5)
	mux := muxOf(s)
	req, _ := http.NewRequest(http.MethodGet, "/api/bots/status", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: tok})
	const n = 500
	lat := make([]time.Duration, 0, n)
	for i := 0; i < n; i++ {
		rec := httptest.NewRecorder()
		start := time.Now()
		mux.ServeHTTP(rec, req)
		lat = append(lat, time.Since(start))
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d", rec.Code)
		}
	}
	sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
	p50 := lat[len(lat)*50/100]
	p95 := lat[len(lat)*95/100]
	t.Logf("/api/bots/status with 5 bots: p50 %v p95 %v (n=%d, target <5ms)", p50, p95, n)
	if p95 > 5*time.Millisecond {
		t.Errorf("p95 %v exceeds the 5ms target", p95)
	}
}

func BenchmarkBotStatusAPI5Bots(b *testing.B) {
	root := b.TempDir()
	s := &Server{Root: root, Owner: "u1"}
	tok, _, err := s.EnsureToken()
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		bb, err := bot.Create(root, fmt.Sprintf("bot%d", i), "p", []string{link.NameTerminal, link.NameWeb})
		if err != nil {
			b.Fatal(err)
		}
		if err := bb.SetOwners([]string{"u1"}); err != nil {
			b.Fatal(err)
		}
		if err := bb.Append(bot.Entry{Time: time.Now(), Message: provider.Message{Role: provider.RoleAssistant, Content: "hello"}}); err != nil {
			b.Fatal(err)
		}
	}
	ui := httptest.NewServer(muxOf(s))
	defer ui.Close()
	cookie := loginCookieB(b, ui.URL, tok)
	req, _ := http.NewRequest(http.MethodGet, ui.URL+"/api/bots/status", nil)
	req.AddCookie(cookie)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := ui.Client().Do(req)
		if err != nil {
			b.Fatal(err)
		}
		res.Body.Close()
	}
}

func loginCookieB(b *testing.B, base, tok string) *http.Cookie {
	b.Helper()
	res, err := http.Post(base+"/api/login", "application/json", strings.NewReader(`{"token":"`+tok+`"}`))
	if err != nil {
		b.Fatal(err)
	}
	res.Body.Close()
	for _, c := range res.Cookies() {
		if c.Name == cookieName {
			return c
		}
	}
	b.Fatal("no cookie")
	return nil
}
