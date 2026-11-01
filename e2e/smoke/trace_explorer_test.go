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
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/browsertest"
	"xbot/internal/provider"
)

// Real binary + serve + durable transcript + browser. Read-only records are
// seeded on disk, and the later owner message uses the scripted model through
// real HTTP/SSE. No browser routes or clipboard APIs are replaced.
func TestTraceExplorerJourney(t *testing.T) {
	t.Setenv("XBOT_TEST_CHROME", browsertest.Chrome(t))
	srv := Start(t, StartStub(t))
	srv.CreateBot("Trace", "轨迹真实入口验收")
	srv.CreateBot("Other", "轨迹归属隔离")
	b, err := bot.Load(srv.Root, "Trace")
	if err != nil {
		t.Fatal(err)
	}
	defer bot.CloseStore(b)
	appendMessage := func(role string, parts []provider.ContentPart) {
		t.Helper()
		if err := b.Append(bot.Entry{Time: time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC), Message: provider.Message{Role: role, Content: parts}}); err != nil {
			t.Fatal(err)
		}
	}
	call := func(id, name, args string) provider.ToolCall {
		c := provider.ToolCall{ID: id, Type: "function"}
		c.Function.Name, c.Function.Arguments = name, args
		return c
	}
	for i := 1; i <= 100; i++ {
		if i == 20 {
			appendMessage(provider.RoleAssistant, provider.ToolContent([]provider.ToolCall{call("outside-window", "read_file", `{"path":"outside.txt"}`)}))
			continue
		}
		appendMessage(provider.RoleUser, provider.TextContent(fmt.Sprintf("已存输入 %03d", i)))
	}
	appendMessage(provider.RoleUser, provider.TextContent("真实输入 ORCHID"))
	appendMessage(provider.RoleUser, provider.TextContent("<environment_update>真实工作区上下文</environment_update>"))
	parts := []provider.ContentPart{{Type: provider.PartReasoning, Vendor: provider.ChatCompletions, VendorRaw: json.RawMessage(`{"reasoning_content":"真实思考默认折叠"}`)}}
	parts = append(parts, provider.TextContent("模型正文 ORCHID")...)
	parts = append(parts, provider.ToolContent([]provider.ToolCall{call("read-one", "read_file", `{"path":"one.txt"}`), call("fail-two", "shell", `{"command":"false"}`)})...)
	appendMessage(provider.RoleAssistant, parts)
	appendMessage(provider.RoleTool, provider.ToolResult("read-one", "真实文件结果 LANTERN"))
	appendMessage(provider.RoleTool, provider.ToolResult("fail-two", "error: 真实失败 COBALT"))
	appendMessage(provider.RoleAssistant, []provider.ContentPart{{Type: provider.PartHosted, Text: "[厂商生图: 已保存到工作区 generated/trace.png]", Hosted: &provider.HostedStep{ID: "hosted-one", Name: "image_generation", Status: "completed", Query: "安静海边"}}})
	appendMessage(provider.RoleAssistant, provider.TextContent("真实阅读 SELECTABLE\n"+strings.Repeat("长正文，列表和详情各自滚动。\n", 100)))
	appendMessage(provider.RoleTool, provider.ToolResult("outside-window", "真实窗口孤立结果 AMBER"))
	u, _ := url.Parse(srv.URL)
	var cookieName, token string
	for _, c := range srv.client.Jar.Cookies(u) {
		if c.Name == "xbot_session" {
			cookieName, token = c.Name, c.Value
		}
	}
	if token == "" {
		t.Fatal("serve login did not establish browser session")
	}
	t.Logf("trace explorer serve URL: %s (private data root: %s)", srv.URL, srv.Root)
	// Optional metadata helps the supervisor's existing screenshot tooling locate
	// this process while the journey is running. Authentication is never written.
	if dir := os.Getenv("XBOT_SMOKE_ARTIFACT_DIR"); dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		metadata, _ := json.Marshal(map[string]string{"url": srv.URL, "root": srv.Root, "test": t.Name()})
		if err := os.WriteFile(filepath.Join(dir, "trace-explorer-server.json"), metadata, 0600); err != nil {
			t.Fatal(err)
		}
	}
	input, _ := json.Marshal(map[string]string{"url": srv.URL, "cookie": cookieName, "token": token, "evidence": os.Getenv("XBOT_TRACE_EVIDENCE")})
	cmd := exec.Command("node", "../../internal/web/testdata/trace-explorer.cjs")
	cmd.Stdin = bytes.NewReader(input)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("trace explorer serve/browser journey: %v\n%s", err, out)
	}
	t.Log(string(out))
}
