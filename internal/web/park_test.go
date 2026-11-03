package web

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/compact"
	"xbot/internal/inbox"
	"xbot/internal/kernel"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
)

// parkModel exercises the review seam in an explicitly strict test kernel,
// not the production always-allow assembly.
type parkModel struct{}

const parkCall = `{"command":"mkdir parked-dir","description":"Create the parked test directory"}`

func (parkModel) Chat(_ context.Context, req provider.Request, onDelta func(string)) (provider.Message, error) {
	// Environment deltas are context, not a new owner/card decision.
	if msg, ok := spoke(req); ok {
		return msg, nil
	}
	last := lastConversationInput(req.Messages)
	say := func(text string) (provider.Message, error) {
		return sayTo(text), nil
	}
	call := func() (provider.Message, error) {
		return provider.Message{Role: provider.RoleAssistant, Content: provider.ToolContent([]provider.ToolCall{{
			ID: "c" + time.Now().Format("150405.000000"), Type: "function",
			Function: provider.FunctionCall{Name: "sh", Arguments: parkCall},
		}})}, nil
	}
	switch {
	case strings.Contains(last.Text(), "<owner_interjected>"):
		return say("答插话")
	case strings.Contains(last.Text(), "<approval_decided>") && strings.Contains(last.Text(), "approved"):
		return call()
	case strings.Contains(last.Text(), "<approval_decided>"):
		return say("好的不做了")
	case last.Role == provider.RoleTool && strings.Contains(last.Text(), "exit 0"):
		return say("做完了")
	case last.Role == provider.RoleTool:
		return say("等你确认")
	default:
		return call()
	}
}

func parkServer(t *testing.T, permissive bool, wait ...time.Duration) (*httptest.Server, *http.Cookie, *bot.Bot, string) {
	t.Helper()
	root := t.TempDir()
	work := t.TempDir()
	s := &Server{Root: root, Owner: "u1", PollInterval: 20 * time.Millisecond}
	if len(wait) > 0 {
		s.ApprovalWait = wait[0]
	}
	tok, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := bot.Create(root, "ada", "persona", []string{link.NameWeb})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	s.NewKernel = func(b *bot.Bot, _ *kernel.BotResources) (*kernel.Kernel, error) {
		box, err := inbox.Open(b.InboxPath())
		if err != nil {
			return nil, err
		}
		return &kernel.Kernel{
			Bot: b, Provider: parkModel{}, Inbox: box, Memory: memory.NopStore{}, MaxSteps: 8,
			Budget:        compact.DefaultBudget(),
			Links:         []link.Link{link.Web{}},
			WorkDir:       work,
			Permissive:    permissive,
			PermissionsOn: func() bool { return true },
			// Strict mechanism tests ask; production-style assembly must
			// bypass this gate even with the legacy switch on.
			Machine: func() (string, error) { return "ask", nil },
		}, nil
	}
	ui := httptest.NewServer(muxOf(s))
	t.Cleanup(func() {
		kernel.SetWaker(root, nil)
		ui.Close()
	})
	return ui, loginCookie(t, ui.URL, tok), b, work
}

// waitTranscript waits until some line of the transcript contains text, and
// then until no turn holds the bot, so the test ends after the last write.
func waitTranscript(t *testing.T, b *bot.Bot, text string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		entries, err := b.Entries()
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, e := range entries {
			if saidIn(e.Message, text) {
				found = true
			}
		}
		if found {
			if lock, ok, _ := b.TryLockHandle(); ok {
				_ = lock.Release()
				return
			}
		}
		if time.Now().After(deadline) {
			for _, e := range entries {
				t.Logf("%s %q %v", e.Message.Role, clip(e.Message.Text(), 160), e.Message.Calls())
			}
			t.Fatalf("transcript never had %q", text)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func cardCount(t *testing.T, b *bot.Bot) int {
	t.Helper()
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, e := range entries {
		if card, ok := bot.ParseApprovalCard(e.Message.Text()); ok {
			ids[card.ID] = true
		}
	}
	return len(ids)
}

// parkAndInterject starts a call that waits on a card, then speaks over it.
// The interjection is answered at once while the card stays open.
func parkAndInterject(t *testing.T, ui *httptest.Server, cookie *http.Cookie, b *bot.Bot, work string) string {
	t.Helper()
	first := postSSE(t, ui.URL, cookie, "做一件需要确认的事")
	ev := waitEvent(t, first, "approval")
	var card struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(ev.data), &card); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	res := postJSON(t, ui.URL+"/api/bots/ada/messages", cookie, `{"text":"插一句：现在几点"}`)
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("interjection waited %v behind the card", took)
	}
	if got := parseSSE(t, res.Body); res.StatusCode != http.StatusOK || !strings.Contains(got["done"], "答插话") {
		t.Fatalf("interjection %d %v", res.StatusCode, got)
	}
	done := waitEvent(t, first, "done")
	if !strings.Contains(done.data, `"handed_off":true`) {
		t.Fatalf("first request done %s", done.data)
	}
	if _, err := os.Stat(filepath.Join(work, "parked-dir")); err == nil {
		t.Fatal("parked call ran before the owner decided")
	}
	waitTranscript(t, b, "答插话")
	if states := approvalStates(t, ui.URL, cookie, card.ID); len(states) != 1 || states[0] != bot.ApprovalPending {
		t.Fatalf("card states %v, want still pending", states)
	}
	return card.ID
}

// Production-style permissive assembly still runs the real interpreter tool
// through review, but never parks for a tool permission card.
func TestPermissiveToolReviewRunsWithoutParking(t *testing.T) {
	ui, cookie, b, work := parkServer(t, true)
	for ev := range postSSE(t, ui.URL, cookie, "创建目录") {
		if ev.name == "approval" || ev.name == "error" {
			t.Fatalf("unexpected %s: %s", ev.name, ev.data)
		}
	}
	waitTranscript(t, b, "做完了")
	if _, err := os.Stat(filepath.Join(work, "parked-dir")); err != nil {
		t.Fatalf("allowed tool did not run: %v", err)
	}
	if n := cardCount(t, b); n != 0 {
		t.Fatalf("permissive review opened %d cards", n)
	}
}

// A card being waited on does not hold up the owner, and approving it later
// runs the call once, with no second card, and the bot carries on.
func TestParkedCardApprovedLater(t *testing.T) {
	ui, cookie, b, work := parkServer(t, false)
	id := parkAndInterject(t, ui, cookie, b, work)
	decide(t, ui.URL, cookie, id, true)
	waitTranscript(t, b, "做完了")
	if _, err := os.Stat(filepath.Join(work, "parked-dir")); err != nil {
		t.Fatal("approved call never ran")
	}
	if n := cardCount(t, b); n != 1 {
		t.Fatalf("%d cards; the approved call must not ask again", n)
	}
}

// Declining it later tells the bot, and the call never runs.
func TestParkedCardDeclinedLater(t *testing.T) {
	ui, cookie, b, work := parkServer(t, false)
	id := parkAndInterject(t, ui, cookie, b, work)
	decide(t, ui.URL, cookie, id, false)
	waitTranscript(t, b, "好的不做了")
	if _, err := os.Stat(filepath.Join(work, "parked-dir")); err == nil {
		t.Fatal("declined call ran")
	}
}

func TestParkedApprovalTimeoutAllowsAndWakes(t *testing.T) {
	ui, cookie, b, work := parkServer(t, false, 300*time.Millisecond)
	stream := postSSE(t, ui.URL, cookie, "long task")
	id := approvalCardID(t, waitEvent(t, stream, "approval"))
	for ev := range postSSE(t, ui.URL, cookie, "顺便问一句") {
		if ev.name == "error" {
			t.Fatal(ev.data)
		}
	}
	for range stream {
	}
	waitTranscript(t, b, "做完了")
	if _, err := os.Stat(filepath.Join(work, "parked-dir")); err != nil {
		t.Fatal(err)
	}
	if got := latestState(t, b, id); got != bot.ApprovalAutoApproved {
		t.Fatalf("state %q", got)
	}
	if n := cardCount(t, b); n != 1 {
		t.Fatalf("opened %d cards, want one", n)
	}
}

func TestDisconnectedApprovalTimeoutResumes(t *testing.T) {
	ui, cookie, b, work := parkServer(t, false, 200*time.Millisecond)
	req, _ := http.NewRequest("POST", ui.URL+"/api/bots/ada/messages", strings.NewReader(`{"text":"long task"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	scan := bufio.NewScanner(res.Body)
	seen := false
	for scan.Scan() {
		if scan.Text() == "event: approval" {
			seen = true
			break
		}
	}
	res.Body.Close()
	if !seen {
		t.Fatal("no card before disconnect")
	}
	waitTranscript(t, b, "做完了")
	if _, err := os.Stat(filepath.Join(work, "parked-dir")); err != nil {
		t.Fatal(err)
	}
	if n := cardCount(t, b); n != 1 {
		t.Fatalf("opened %d cards", n)
	}
}
