package notable

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func displayPath(path, home string) string {
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	if home != "" {
		if rel, err := filepath.Rel(home, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			if rel == "." {
				return "~"
			}
			return "~/" + filepath.ToSlash(rel)
		}
	}
	return path
}
func itemText(item Item) string {
	if len(item.Objects) == 0 {
		for _, c := range catalog {
			if c.Kind == item.Kind {
				return c.Text
			}
		}
		return ""
	}
	objects := item.Objects
	if len(objects) > 2 {
		objects = objects[:2]
	}
	shown := make([]string, len(objects))
	for i, o := range objects {
		shown[i] = shortObject(o)
	}
	names := strings.Join(shown, "、")
	names = strings.NewReplacer("\n", " ", "\r", " ", "\t", " ").Replace(names)
	if len(item.Objects) > 2 {
		unit := item.Unit
		if unit == "" {
			unit = "项"
		}
		names += " 等 " + strconv.Itoa(len(item.Objects)) + " " + unit
	}
	if item.Incomplete {
		names += "等（部分对象未提供或未记录）"
	}
	switch item.Kind {
	case "delete":
		return "删除了 " + names
	case "write_outside":
		return "写入了 " + names
	case "global_install":
		return "全局安装了 " + names
	case "external_message":
		if item.Incomplete {
			return "向 " + names + "发送了消息"
		}
		return "向 " + names + " 发了 " + strconv.Itoa(item.Calls) + " 条消息"
	}
	return ""
}
func recordDelete(ctx context.Context, objects []string, unit string) {
	if s := from(ctx); s.log != nil {
		s.log.merge(Item{Kind: "delete", Objects: objects, Calls: 1, Unit: unit, Incomplete: len(objects) == 0})
	}
}
func deletedObjects(name string, args json.RawMessage) ([]string, string) {
	var in struct {
		Path  string   `json:"path"`
		Paths []string `json:"paths"`
		Name  string   `json:"name"`
	}
	if json.Unmarshal(args, &in) != nil {
		return nil, ""
	}
	switch name {
	case "delete_file", "delete_files", "remove_file", "file_delete", "unlink_file", "trash_file":
		paths := in.Paths
		if in.Path != "" {
			paths = append(paths, in.Path)
		}
		for i, p := range paths {
			paths[i] = displayPath(p, "")
		}
		return paths, "个文件"
	case "delete_bot":
		if in.Name != "" {
			return []string{in.Name}, "个 Bot"
		}
	case "delete_secret", "secret_delete", "secret_rm", "remove_secret", "delete_link", "remove_link", "remove_mcp", "mcp_remove", "delete_mcp":
		if in.Name != "" {
			return []string{in.Name}, "条记录"
		}
	}
	return nil, ""
}
func endpoint(raw string) []string {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return nil
	}
	return []string{u.Hostname()}
}
func sendObjects(name string, args json.RawMessage) []string {
	var in struct {
		To      string `json:"to"`
		Service string `json:"service"`
		URL     string `json:"url"`
	}
	_ = json.Unmarshal(args, &in)
	switch name {
	case "telegram_send":
		if in.To != "" {
			return []string{"Telegram · " + in.To}
		}
		return []string{"Telegram"}
	case "lark_send":
		if in.To != "" {
			return []string{"飞书 · " + in.To}
		}
		return []string{"飞书"}
	case "send_webhook", "webhook_send":
		return endpoint(in.URL)
	case "send_email":
		if in.To != "" {
			return []string{in.To}
		}
	case "send_message", "post_message":
		if in.Service != "" {
			return []string{in.Service}
		}
	}
	// Dynamic MCP tools have no common object schema. Never record their body,
	// credential fields or arbitrary argument values as a supposed destination.
	return nil
}

// The full objects stay in metadata; summaries must fit the existing 4 KiB
// completion report even when a path/URL is unusually long.
func shortObject(s string) string {
	r := []rune(s)
	if len(r) <= 96 {
		return s
	}
	if strings.ContainsAny(s, "/\\") {
		// Transport summaries may elide directories, never pieces of a filename.
		sep := "/"
		if strings.Contains(s, "\\") && !strings.Contains(s, "/") {
			sep = "\\"
		}
		parts := strings.FieldsFunc(s, func(r rune) bool { return r == '/' || r == '\\' })
		if len(parts) <= 4 {
			return s
		}
		root := ""
		if strings.HasPrefix(s, sep) {
			root = sep
		}
		if parts[0] == "~" || strings.HasSuffix(parts[0], ":") {
			root += parts[0] + sep
			parts = parts[1:]
		}
		if len(parts) <= 4 {
			return s
		}
		return root + strings.Join(parts[:2], sep) + sep + "…" + sep + strings.Join(parts[len(parts)-2:], sep)
	}
	return string(r[:95]) + "…"
}
