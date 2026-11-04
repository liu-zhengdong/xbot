//go:build smoke

package smoke

import (
	"bytes"
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"

	"xbot/internal/browsertest"
)

// Use real serve, skill files, Bot switches and shipped UI. No model call is needed.
func TestBotSkillsJourney(t *testing.T) {
	t.Setenv("XBOT_TEST_CHROME", browsertest.Chrome(t))
	srv := Start(t, StartStub(t))
	srv.CreateBot("SkillBot", "技能浏览真实入口验收")
	srv.CreateBot("OtherBot", "技能按 Bot 隔离验收")

	const guideBody = "# 技能正文验收\n\n**真实技能粗体** 与中文说明。\n\n- 阅读正文\n- 保留聊天草稿\n\n<img src=\"data:,\" onerror=\"window.skillJourneyUnsafe=1\">\n\n[危险链接](javascript:window.skillJourneyUnsafe=2)"
	for _, skill := range []struct {
		slug, title, description, body string
	}{
		{"journey-guide", "技能旅程手册", "SearchNeedle 中文整理。" + strings.Repeat("这是较长的说明，用于检查真实技能列表在窄屏中保持可读。", 5), guideBody},
		{"plain-slug", "", "没有 title 时使用技能名称。", "# 无标题技能\n\nPLAIN_BODY_ONLY"},
		{"disabled-skill", "停用验收技能", "按每个 Bot 独立启用。", "# 开关隔离\n\nDISABLED_BODY_ONLY"},
	} {
		content := "---\nname: " + skill.slug + "\n"
		if skill.title != "" {
			content += "title: " + skill.title + "\n"
		}
		content += "description: " + skill.description + "\n---\n\n" + skill.body + "\n"
		if status, body := srv.do("POST", "/api/skills/import", map[string]string{"folder": skill.slug, "content": content}); status != 200 {
			t.Fatalf("import %s: %d %s", skill.slug, status, body)
		}
	}
	for _, route := range []string{
		"/api/bots/SkillBot/skills/disabled-skill/disable",
		"/api/bots/OtherBot/skills/plain-slug/disable",
	} {
		if status, body := srv.do("POST", route, nil); status != 200 {
			t.Fatalf("seed Bot skill switch: %d %s", status, body)
		}
	}

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	var cookieName, token string
	for _, cookie := range srv.client.Jar.Cookies(u) {
		if cookie.Name == "xbot_session" {
			cookieName, token = cookie.Name, cookie.Value
		}
	}
	if token == "" {
		t.Fatal("browser session missing")
	}
	input, err := json.Marshal(map[string]string{
		"url": srv.URL, "cookie": cookieName, "token": token,
		"body": guideBody, "evidence": os.Getenv("XBOT_BOT_SKILLS_EVIDENCE"),
	})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("node", "../../internal/web/testdata/bot-skills.cjs")
	cmd.Stdin = bytes.NewReader(input)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bot skills real journey: %v\n%s", err, out)
	}
	t.Log(string(out))
}
