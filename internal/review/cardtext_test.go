package review

import (
	"encoding/json"
	"testing"
)

func TestCardText(t *testing.T) {
	tests := []struct {
		name    string
		tool    string
		args    string
		summary string
		machine string
		wantT   string
		wantSub string
	}{
		{
			name: "view image", tool: "view_image",
			args: `{"path":"shots/a.png"}`, summary: "view shots/a.png", machine: "local",
			wantT: "查看图片", wantSub: "a.png",
		},
		{
			name: "web search", tool: "web_search",
			args: `{"query":"xbot"}`, summary: "", machine: "",
			wantT: "搜索网页", wantSub: "xbot",
		},
		{
			name: "shell deletes", tool: "shell",
			args: `{"command":"rm -rf /tmp/x"}`, summary: "rm -rf /tmp/x", machine: "local",
			wantT: "删除文件", wantSub: "在本机上执行",
		},
		{
			name: "shell sends", tool: "shell",
			args: `{"command":"curl -X POST https://example.com"}`, summary: "curl -X POST https://example.com", machine: "local",
			wantT: "对外发送", wantSub: "在本机上执行",
		},
		{
			name: "shell runs", tool: "shell",
			args: `{"command":"mkdir -p approved-dir"}`, summary: "mkdir -p approved-dir", machine: "local",
			wantT: "运行命令", wantSub: "在本机上执行",
		},
		{
			name: "shell other machine", tool: "shell",
			args: `{"command":"ls"}`, summary: "ls", machine: "vps-tokyo",
			wantT: "运行命令", wantSub: "在 vps-tokyo 上执行",
		},
		{
			name: "shell falls back to summary", tool: "shell",
			args: ``, summary: "rm /tmp/x", machine: "local",
			wantT: "删除文件", wantSub: "在本机上执行",
		},
		{
			name: "http get", tool: "http_request",
			args: `{"method":"GET","url":"https://api.example.com/v1/x"}`, summary: "GET https://api.example.com/v1/x",
			wantT: "发起 HTTP 请求", wantSub: "GET api.example.com",
		},
		{
			name: "http secret", tool: "http_request",
			args: `{"method":"POST","url":"https://api.example.com/x","headers":{"Authorization":"Bearer {{secret:TOKEN}}"}}`, summary: "POST https://api.example.com/x",
			wantT: "带密钥的 HTTP 请求", wantSub: "POST api.example.com",
		},
		{
			name: "http host from summary", tool: "http_request",
			args: ``, summary: "DELETE example.com:8443/x",
			wantT: "发起 HTTP 请求", wantSub: "DELETE example.com:8443",
		},
		{
			name: "write plain", tool: "write_file",
			args: `{"path":"/tmp/a/b.txt","content":"x"}`, summary: "write /tmp/a/b.txt (1 bytes)",
			wantT: "写入文件", wantSub: "b.txt",
		},
		{
			name: "write persona", tool: "write_file",
			args: `{"path":"/home/u/.xbot/bots/ada/persona.md","content":"x"}`, summary: "write persona.md (1 bytes)",
			wantT: "修改 persona", wantSub: "persona.md",
		},
		{
			name: "edit profile", tool: "edit_file",
			args: `{"path":"/home/u/.xbot/bots/ada/config.json","old_string":"a","new_string":"b"}`, summary: "edit config.json (1 -> 1 bytes)",
			wantT: "修改档案", wantSub: "config.json",
		},
		{
			name: "propose persona", tool: "bot_propose_persona",
			args: `{"persona":"x"}`, summary: "update persona",
			wantT: "修改职责", wantSub: "",
		},
		{
			name: "update profile", tool: "bot_update_profile",
			args: `{"name":"Ada"}`, summary: "update profile: name",
			wantT: "修改档案", wantSub: "",
		},
		{
			name: "delete file", tool: "delete_file",
			args: `{"path":"/tmp/x"}`, summary: "delete /tmp/x",
			wantT: "删除文件", wantSub: "",
		},
		{
			name: "delete bot", tool: "delete_bot",
			args: `{"name":"bob"}`, summary: "delete bot bob",
			wantT: "删除 Bot", wantSub: "",
		},
		{
			name: "telegram send", tool: "telegram_send",
			args: `{"to":"1","body":"hi"}`, summary: "telegram 1\nhi",
			wantT: "发送消息到 Telegram", wantSub: "",
		},
		{
			name: "lark send", tool: "lark_send",
			args: `{"to":"1","body":"hi"}`, summary: "lark 1\nhi",
			wantT: "发送消息到 Lark", wantSub: "",
		},
		{
			name: "connector messenger name", tool: "telegram",
			args: ``, summary: "telegram 1\nhi",
			wantT: "发送消息到 Telegram", wantSub: "",
		},
		{
			name: "mcp tool", tool: "mcp_github_create_issue",
			args: `{"title":"x"}`, summary: "create issue",
			wantT: "调用 MCP 工具 mcp_github_create_issue", wantSub: "",
		},
		{
			name: "links install", tool: "links",
			args: `{"action":"install","id":"telegram"}`, summary: "install telegram",
			wantT: "安装 Link", wantSub: "",
		},
		{
			name: "default is tool name", tool: "ask_user",
			args: `{"question":"?"}`, summary: "?",
			wantT: "ask_user", wantSub: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var args json.RawMessage
			if tt.args != "" {
				args = json.RawMessage(tt.args)
			}
			title, subtitle := CardText(tt.tool, args, tt.summary, tt.machine)
			if title != tt.wantT || subtitle != tt.wantSub {
				t.Fatalf("CardText(%q) = (%q, %q), want (%q, %q)", tt.tool, title, subtitle, tt.wantT, tt.wantSub)
			}
		})
	}
}
