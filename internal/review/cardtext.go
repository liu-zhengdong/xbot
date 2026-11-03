package review

import (
	"encoding/json"
	"net/url"
	"path"
	"strings"
)

// CardText returns the Chinese title and subtitle shown on one approval card.
//
// tool is the tool name, args its raw JSON arguments, summary the tool's own
// short detail that the caller keeps as the command chip, and machine the
// machine the action runs on ("local" renders as 本机, any other name is shown
// as is). The function is pure: the same inputs always give the same labels.
func CardText(tool string, args json.RawMessage, summary, machine string) (title, subtitle string) {
	name := normalize(tool)
	switch {
	case IsShellTool(name):
		return shellCardText(args, summary, machine)
	case name == "write_file" || name == "edit_file":
		return fileCardText(args, summary)
	case name == "view_image":
		return viewImageCardText(args, summary)
	case name == "web_fetch":
		return "抓取网页", hostOf(jsonString(args, "url"))
	case name == "web_search":
		return "搜索网页", jsonString(args, "query")
	case name == "bot_propose_persona":
		return "修改职责", ""
	case isConnectorSend(name):
		return "发送消息到 " + connectorName(name), ""
	case strings.HasPrefix(name, mcpPrefix):
		return "调用 MCP 工具 " + strings.TrimSpace(tool), ""
	case ClassOf(name) == Delete:
		return deleteTitle(name), ""
	default:
		return strings.TrimSpace(tool), ""
	}
}

// shellCardText labels a shell command by what it does. The machine name comes
// from the machine argument.
func shellCardText(args json.RawMessage, summary, machine string) (string, string) {
	cmd := jsonString(args, "command")
	if strings.TrimSpace(cmd) == "" {
		cmd = firstLine(summary)
	}
	target := strings.TrimSpace(machine)
	if target == "" || target == "local" {
		target = "本机"
	}
	deletes, sends := ShellFlags(cmd)
	switch {
	case deletes:
		return "删除文件", whereRun(target)
	case sends:
		return "对外发送", whereRun(target)
	default:
		return "运行命令", whereRun(target)
	}
}

// fileCardText labels a file write or edit. A persona or profile file is a
// self-edit; the subtitle is always the path's base name.
func fileCardText(args json.RawMessage, summary string) (string, string) {
	p := strings.TrimSpace(jsonString(args, "path"))
	if p == "" {
		p = pathFromSummary(summary)
	}
	base := path.Base(strings.ReplaceAll(p, `\`, "/"))
	if base == "." || base == "/" || base == `\` {
		base = ""
	}
	switch strings.ToLower(base) {
	case "persona.md":
		return "修改 persona", base
	case "config.json":
		return "修改档案", base
	default:
		return "写入文件", base
	}
}

func isConnectorSend(name string) bool {
	switch name {
	case "telegram", "telegram_send", "lark", "lark_send":
		return true
	default:
		return strings.HasSuffix(name, "_send")
	}
}

func connectorName(name string) string {
	name = strings.TrimSuffix(name, "_send")
	switch name {
	case "telegram":
		return "Telegram"
	case "lark":
		return "Lark"
	case "":
		return ""
	default:
		return strings.ToUpper(name[:1]) + name[1:]
	}
}

func deleteTitle(name string) string {
	switch name {
	case "delete_file", "delete_files", "remove_file", "file_delete", "unlink_file", "trash_file":
		return "删除文件"
	case "delete_secret", "secret_delete", "secret_rm", "remove_secret":
		return "删除密钥"
	case "delete_bot":
		return "删除 Bot"
	case "delete_link", "remove_link":
		return "删除 Link"
	case "remove_mcp", "mcp_remove", "delete_mcp":
		return "删除 MCP 服务器"
	default:
		return "删除"
	}
}

func viewImageCardText(args json.RawMessage, summary string) (string, string) {
	if raw := strings.TrimSpace(jsonString(args, "url")); raw != "" {
		return "查看图片", hostOf(raw)
	}
	p := strings.TrimSpace(jsonString(args, "path"))
	if p == "" {
		p = pathFromSummary(summary)
	}
	base := path.Base(strings.ReplaceAll(p, `\`, "/"))
	if base == "." || base == "/" || base == `\` {
		base = ""
	}
	return "查看图片", base
}

func jsonString(args json.RawMessage, key string) string {
	if len(args) == 0 {
		return ""
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(args, &fields); err != nil {
		return ""
	}
	raw, ok := fields[key]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(line)
}

// hostOf returns the host of an absolute or scheme-less URL.
func hostOf(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	u, err := url.Parse(rawURL)
	if err == nil && u.Host != "" {
		return u.Host
	}
	if !strings.Contains(rawURL, "://") {
		if u, err := url.Parse("http://" + rawURL); err == nil && u.Host != "" {
			return u.Host
		}
	}
	return rawURL
}

// pathFromSummary recovers the path shown by write_file or edit_file.
func pathFromSummary(summary string) string {
	line := firstLine(summary)
	line = strings.TrimPrefix(line, "write ")
	line = strings.TrimPrefix(line, "edit ")
	if i := strings.LastIndex(line, " ("); i >= 0 {
		line = line[:i]
	}
	return strings.TrimSpace(line)
}

// whereRun says where a command runs. Chinese needs no spaces around 本机;
// a machine name keeps them so it reads apart from the text.
func whereRun(target string) string {
	if target == "本机" {
		return "在本机上执行"
	}
	return "在 " + target + " 上执行"
}
