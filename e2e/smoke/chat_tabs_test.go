//go:build smoke

package smoke

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/browsertest"
	"xbot/internal/experience"
	"xbot/internal/provider"
)

// Real serve, disk-backed memory/chat and shipped UI. The model alone is stubbed.
func TestChatTabsJourney(t *testing.T) {
	t.Setenv("XBOT_TEST_CHROME", browsertest.Chrome(t))
	srv := Start(t, chatTabsVisibleStub(t))
	srv.CreateBot("Workspace", "聊天页面改版验收")
	srv.CreateBot("Other", "切换隔离验收")
	srv.Send("Workspace", "SMOKE_HELLO_1")
	b, err := bot.Load(srv.Root, "Workspace")
	if err != nil {
		t.Fatal(err)
	}
	defer bot.CloseStore(b)
	for i := 1; i <= 100; i++ {
		text := fmt.Sprintf("聊天历史 %03d，顶部标签切换保持这份对话。", i)
		if err := b.Append(bot.Entry{Time: time.Now(), Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent(text)}}); err != nil {
			t.Fatal(err)
		}
	}
	mem := srv.Memory("Workspace")
	for p, text := range map[string]string{
		mem.Person.Path: "# 关于你\n\n原始文件中的 **用户记忆验收标记**。\n",
		mem.Bot.Path:    "# 关于这个 Bot\n\n原始文件中的 Bot 记忆验收标记。\n",
	} {
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	store := experience.At(bot.Dir(srv.Root, "Workspace"))
	if _, err := store.Add(experience.Entry{CreatedAt: time.Now(), Summary: "真实经历验收标记", Segment: "来自真实存储的可展开提纲"}); err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(srv.URL)
	var cookieName, token string
	for _, c := range srv.client.Jar.Cookies(u) {
		if c.Name == "xbot_session" {
			cookieName, token = c.Name, c.Value
		}
	}
	if token == "" {
		t.Fatal("browser session missing")
	}
	input, _ := json.Marshal(map[string]string{"url": srv.URL, "cookie": cookieName, "token": token, "evidence": os.Getenv("XBOT_CHAT_TABS_EVIDENCE")})
	cmd := exec.Command("node", "../../internal/web/testdata/chat-tabs.cjs")
	cmd.Stdin = bytes.NewReader(input)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("chat tabs real journey: %v\n%s", err, out)
	}
	t.Log(string(out))
}

// The legacy smoke hello returns model prose (an aside in the current UI).
// This journey scripts an actual send_message_to_user tool delivery instead.
func chatTabsVisibleStub(t *testing.T) *Stub {
	s := StartStub(t)
	fallback := s.srv
	t.Cleanup(fallback.Close)
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			fallback.Config.Handler.ServeHTTP(w, r)
			return
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read", http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		var req stubRequest
		if json.Unmarshal(raw, &req) != nil {
			fallback.Config.Handler.ServeHTTP(w, r)
			return
		}
		text := lastUser(req)
		if !strings.HasPrefix(text, "TOP_TABS_") {
			fallback.Config.Handler.ServeHTTP(w, r)
			return
		}
		label := strings.TrimPrefix(text, "TOP_TABS_")
		id := "tabs_" + label
		for _, m := range req.Messages {
			for _, tc := range m.ToolCalls {
				var call struct {
					ID string `json:"id"`
				}
				if json.Unmarshal(tc, &call) == nil && call.ID == id {
					streamText(w, []string{"SILENT"})
					return
				}
			}
		}
		time.Sleep(150 * time.Millisecond)
		streamTool(w, id, "send_message_to_user", argsJSON(map[string]string{"text": "切页期间的实际回复 " + label}))
	}))
	return s
}
