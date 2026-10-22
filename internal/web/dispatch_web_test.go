package web

import (
	"context"
	"fmt"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/exec"
	"xbot/internal/inbox"
	"xbot/internal/kernel"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
)

// dispatchModel is the foreground and the subagent. The foreground hands a
// long task to a dev subagent, answers anything else at once, and reports
// the subagent's result. The subagent works until hold is closed.
type dispatchModel struct {
	hold      chan struct{}
	mu        sync.Mutex
	foreCalls int
}

func (m *dispatchModel) Chat(ctx context.Context, req provider.Request, onDelta func(string)) (provider.Message, error) {
	say := func(text string) (provider.Message, error) {
		if onDelta != nil {
			onDelta(text)
		}
		return provider.Message{Role: provider.RoleAssistant, Content: text}, nil
	}
	if strings.HasPrefix(req.Messages[0].Content, exec.SubPersona) {
		select {
		case <-m.hold:
		case <-ctx.Done():
			return provider.Message{}, ctx.Err()
		}
		return provider.Message{Role: provider.RoleAssistant, Content: "**Changed**\n- a.go\n\n**Tests**\nok\n\n**Not done**\n- none"}, nil
	}
	m.mu.Lock()
	m.foreCalls++
	m.mu.Unlock()
	last := lastConversationInput(req.Messages)
	switch {
	case last.Role == provider.RoleTool:
		return say("已派给 dev 子代理，做完告诉你。")
	case strings.Contains(last.Content, `source="subagent"`) && strings.Contains(last.Content, `type="done"`):
		return say("开发任务做完了：改了 a.go，测试通过。")
	case strings.Contains(last.Content, "十分钟"):
		return provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "d1", Type: "function",
			Function: provider.FunctionCall{Name: "dispatch", Arguments: `{"type":"dev","task":"在工作目录里重构 a.go，改完跑 go test，交回改了什么和测试结果。"}`},
		}}}, nil
	default:
		return say("答：" + last.Content)
	}
}

// ADR 0016 acceptance: a ten-minute dev task is handed out within three
// foreground steps; five messages sent while it runs are each answered in
// under three seconds; the result is reported once, unasked.
func TestDispatchKeepsTheForegroundFree(t *testing.T) {
	root := t.TempDir()
	work := t.TempDir()
	s := &Server{Root: root, Owner: "u1", PollInterval: 20 * time.Millisecond}
	tok, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := bot.Create(root, "ada", "persona", []string{link.NameWeb, link.NameTools, link.NameAgents})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	m := &dispatchModel{hold: make(chan struct{})}
	s.NewKernel = func(b *bot.Bot) (*kernel.Kernel, error) {
		box, err := inbox.Open(b.InboxPath())
		if err != nil {
			return nil, err
		}
		return &kernel.Kernel{
			Bot: b, Provider: m, Inbox: box, Memory: memory.NopStore{}, MaxSteps: 8,
			Dispatch: true, Permissive: true, WorkDir: work,
			Links: []link.Link{link.Web{}, link.NewToolsLinkAt(work, ""), link.Agents{}},
		}, nil
	}
	ui := httptest.NewServer(muxOf(s))
	t.Cleanup(func() {
		kernel.SetWaker(root, nil)
		ui.Close()
	})
	cookie := loginCookie(t, ui.URL, tok)

	res := postJSON(t, ui.URL+"/api/bots/ada/messages", cookie, `{"text":"做个十分钟的开发任务：重构 a.go"}`)
	if ev := parseSSE(t, res.Body); !strings.Contains(ev["done"], "已派给 dev") {
		t.Fatalf("first reply %v", ev)
	}
	m.mu.Lock()
	steps := m.foreCalls
	m.mu.Unlock()
	if steps > 3 {
		t.Fatalf("handing out took %d foreground steps", steps)
	}

	var took []time.Duration
	for i := 1; i <= 5; i++ {
		start := time.Now()
		res := postJSON(t, ui.URL+"/api/bots/ada/messages", cookie, fmt.Sprintf(`{"text":"第 %d 句"}`, i))
		took = append(took, time.Since(start))
		if ev := parseSSE(t, res.Body); !strings.Contains(ev["done"], fmt.Sprintf("第 %d 句", i)) {
			t.Fatalf("message %d got %v", i, ev)
		}
	}
	sort.Slice(took, func(i, j int) bool { return took[i] < took[j] })
	// p95 of five is the slowest.
	if p95 := took[len(took)-1]; p95 > 3*time.Second {
		t.Fatalf("p95 %v while the subagent runs", p95)
	}
	t.Logf("five replies while a subagent runs: p50 %v, p95 %v", took[2], took[4])

	close(m.hold)
	report := "开发任务做完了"
	count := func() int {
		entries, err := b.Entries()
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, e := range entries {
			if e.Message.Role == provider.RoleAssistant && strings.Contains(e.Message.Content, report) {
				n++
			}
		}
		return n
	}
	deadline := time.Now().Add(5 * time.Second)
	for count() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	// Let any second delivery land before counting, and let the turn end.
	time.Sleep(300 * time.Millisecond)
	waitTranscript(t, b, report)
	if n := count(); n != 1 {
		t.Fatalf("result reported %d times", n)
	}
	if n := s.botStatuses()[0].CompletedUnread; n != 1 {
		t.Fatalf("delivered subagent result has %d unread completions, want 1", n)
	}
}
