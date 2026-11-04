//go:build smoke

package smoke

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
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
	srv := Start(t, StartStub(t))
	srv.CreateBot("Workspace", "聊天页面改版验收")
	srv.CreateBot("Other", "切换隔离验收")
	if status, body := srv.do("POST", "/api/skills/import", map[string]any{
		"folder":  "layout-evidence",
		"content": "---\nname: layout-evidence\ntitle: 布局验收技能\ndescription: 用独立测试数据检查技能列表与控件布局。\n---\n\n只用于界面取证。\n",
	}); status != 200 {
		t.Fatalf("seed skill: %d %s", status, body)
	}
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
		mem.Bot.Path:    "# memory\n\n## 索引\n工作约定\n\n## 条目\n- (2026-11-01) Bot 记忆验收标记\n- (2026-10-31) 日常说话简短、口语，先给结论\n- (2026-10-30) 先确认声音参考，再保持稳定音色\n",
	} {
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	store := experience.At(bot.Dir(srv.Root, "Workspace"))
	base := time.Now().Add(-24 * time.Hour)
	if _, err := store.Add(experience.Entry{CreatedAt: base, Summary: "真实经历验收标记", Segment: "来自真实存储的可展开提纲"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 45; i++ {
		if _, err := store.Add(experience.Entry{CreatedAt: base.Add(-time.Duration(i+1) * time.Minute), Summary: fmt.Sprintf("TTS 声音经历 %02d", i), Focus: "稳定的声音", Segment: "用户确认声音参考"}); err != nil {
			t.Fatal(err)
		}
	}
	// None of the searchable old entries is present on the initial 40-row page.
	for i := 0; i < 42; i++ {
		if _, err := store.Add(experience.Entry{CreatedAt: base.Add(time.Duration(i+1) * time.Minute), Summary: fmt.Sprintf("最近工作进展 %02d", i)}); err != nil {
			t.Fatal(err)
		}
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
