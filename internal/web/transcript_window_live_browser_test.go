package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/provider"
)

// A real HTTP handler, durable segmented records and shipped UI run together.
// There are no model calls in this read-only journey.
func TestTranscriptWindowLiveBrowser(t *testing.T) {
	if _, err := os.Stat("/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"); err != nil {
		t.Skip("Chrome unavailable")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	root := t.TempDir()
	s := &Server{Root: root, Owner: "reader"}
	token, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := bot.Create(root, "History", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer bot.CloseStore(b)
	if err := b.SetOwners([]string{"reader"}); err != nil {
		t.Fatal(err)
	}
	b.SetSegmentMax(8000)
	for i := 1; i <= 180; i++ {
		text := fmt.Sprintf("聊天记录 %03d · 这条消息保留在同一份只追加的对话里。", i)
		if i == 25 {
			text += " 历史线索 needle"
		}
		if err := b.Append(bot.Entry{Time: time.Date(2026, 10, 30, 10, 0, i, 0, time.UTC), Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent(text)}}); err != nil {
			t.Fatal(err)
		}
	}
	srv := httptest.NewServer(muxOf(s))
	defer srv.Close()
	input, _ := json.Marshal(map[string]string{"url": srv.URL, "cookie": cookieName, "token": token, "evidence": os.Getenv("XBOT_TRANSCRIPT_EVIDENCE")})
	cmd := exec.Command(node, "testdata/transcript-window.cjs")
	cmd.Stdin = bytes.NewReader(input)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("real HTTP browser: %v\n%s", err, out)
	}
	t.Log(string(out))
}

func TestTranscriptWindowVisibleTailBrowser(t *testing.T) {
	if _, err := os.Stat("/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"); err != nil {
		t.Skip("Chrome unavailable")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	root := t.TempDir()
	s := &Server{Root: root, Owner: "reader"}
	token, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := bot.Create(root, "History", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer bot.CloseStore(b)
	if err := b.SetOwners([]string{"reader"}); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	appendMessage := func(m provider.Message) {
		t.Helper()
		if err := b.Append(bot.Entry{Time: at, Message: m}); err != nil {
			t.Fatal(err)
		}
	}
	for page := 0; page < 2; page++ {
		for i := 0; i < 25; i++ {
			call := provider.ToolCall{ID: fmt.Sprintf("say-%d-%d", page, i), Type: "function"}
			call.Function.Name, call.Function.Arguments = "send_message_to_user", `{"text":"visible delivered bubble"}`
			appendMessage(provider.Message{Role: provider.RoleAssistant, Content: provider.ToolContent([]provider.ToolCall{call})})
			appendMessage(provider.Message{Role: provider.RoleTool, Content: provider.ToolResult(call.ID, "sent to the owner's chat")})
		}
		for i := 0; i < 250; i++ {
			appendMessage(provider.Message{Role: provider.RoleTool, Content: provider.ToolResult("noise", strings.Repeat("x", 600))})
		}
	}
	srv := httptest.NewServer(muxOf(s))
	defer srv.Close()
	input, _ := json.Marshal(map[string]string{"url": srv.URL, "cookie": cookieName, "token": token, "evidence": os.Getenv("XBOT_TRANSCRIPT_EVIDENCE")})
	cmd := exec.Command(node, "testdata/transcript-visible-tail.cjs")
	cmd.Stdin = bytes.NewReader(input)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("visible tail HTTP/browser: %v\n%s", err, out)
	}
	t.Log(string(out))
}
