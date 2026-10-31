//go:build smoke

package smoke

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/provider"
)

// The actual serve binary, real HTTP/FTS and shipped browser UI. No model is
// called: the stub is configured only to keep this journey isolated.
func TestTranscriptWindowJourney(t *testing.T) {
	if _, err := os.Stat("/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"); err != nil {
		t.Skip("Chrome unavailable")
	}
	srv := Start(t, StartStub(t))
	srv.CreateBot("History", "History window smoke")
	b, err := bot.Load(srv.Root, "History")
	if err != nil {
		t.Fatal(err)
	}
	defer bot.CloseStore(b)
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
	u, _ := url.Parse(srv.URL)
	var cookieName, token string
	for _, cookie := range srv.client.Jar.Cookies(u) {
		if cookie.Name == "xbot_session" {
			cookieName, token = cookie.Name, cookie.Value
		}
	}
	if token == "" {
		t.Fatal("serve login did not establish browser session")
	}
	input, _ := json.Marshal(map[string]string{"url": srv.URL, "cookie": cookieName, "token": token, "evidence": os.Getenv("XBOT_TRANSCRIPT_EVIDENCE")})
	cmd := exec.Command("node", "../../internal/web/testdata/transcript-window.cjs")
	cmd.Stdin = bytes.NewReader(input)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("serve/browser journey: %v\n%s", err, out)
	}
	t.Log(string(out))
}
