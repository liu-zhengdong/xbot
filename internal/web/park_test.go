package web

import (
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

// parkModel runs one shell command that needs a card, answers an
// interjection, and follows the owner's late decision on the card.
type parkModel struct{}

const parkCall = `{"command":"mkdir parked-dir"}`

func (parkModel) Chat(_ context.Context, req provider.Request, onDelta func(string)) (provider.Message, error) {
	last := req.Messages[len(req.Messages)-1]
	say := func(text string) (provider.Message, error) {
		if onDelta != nil {
			onDelta(text)
		}
		return provider.Message{Role: provider.RoleAssistant, Content: text}, nil
	}
	call := func() (provider.Message, error) {
		return provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "c" + time.Now().Format("150405.000000"), Type: "function",
			Function: provider.FunctionCall{Name: "shell", Arguments: parkCall},
		}}}, nil
	}
	switch {
	case strings.Contains(last.Content, "<owner_interjected>"):
		return say("答插话")
	case strings.Contains(last.Content, "<approval_decided>") && strings.Contains(last.Content, "approved"):
		return call()
	case strings.Contains(last.Content, "<approval_decided>"):
		return say("好的不做了")
	case last.Role == provider.RoleTool && strings.Contains(last.Content, "exit 0"):
		return say("做完了")
	case last.Role == provider.RoleTool:
		return say("等你确认")
	default:
		return call()
	}
}

func parkServer(t *testing.T) (*httptest.Server, *http.Cookie, *bot.Bot, string) {
	t.Helper()
	root := t.TempDir()
	work := t.TempDir()
	s := &Server{Root: root, Owner: "u1", PollInterval: 20 * time.Millisecond}
	tok, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := bot.Create(root, "ada", "persona", []string{link.NameWeb, link.NameTools})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	s.NewKernel = func(b *bot.Bot) (*kernel.Kernel, error) {
		box, err := inbox.Open(b.InboxPath())
		if err != nil {
			return nil, err
		}
		return &kernel.Kernel{
			Bot: b, Provider: parkModel{}, Inbox: box, Memory: memory.NopStore{}, MaxSteps: 8,
			Budget:  compact.DefaultBudget(),
			Links:   []link.Link{link.Web{}, link.NewToolsLinkAt(work, "")},
			WorkDir: work,
			// ask: every shell command raises a card.
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
			if strings.Contains(e.Message.Content, text) {
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
				t.Logf("%s %q %v", e.Message.Role, clip(e.Message.Content, 160), e.Message.ToolCalls)
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
		if card, ok := bot.ParseApprovalCard(e.Message.Content); ok {
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

// A card being waited on does not hold up the owner, and approving it later
// runs the call once, with no second card, and the bot carries on.
func TestParkedCardApprovedLater(t *testing.T) {
	ui, cookie, b, work := parkServer(t)
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
	ui, cookie, b, work := parkServer(t)
	id := parkAndInterject(t, ui, cookie, b, work)
	decide(t, ui.URL, cookie, id, false)
	waitTranscript(t, b, "好的不做了")
	if _, err := os.Stat(filepath.Join(work, "parked-dir")); err == nil {
		t.Fatal("declined call ran")
	}
}
