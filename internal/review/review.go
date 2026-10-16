// Package review classifies a concrete tool call before it runs. The kernel
// asks the owner about the action itself: the tool name and the arguments.
// It never trusts how the model describes the call.
package review

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

// Kind is one category of action that needs the owner before it runs.
type Kind string

const (
	KindHTTP   Kind = "http"
	KindDelete Kind = "delete"
	KindSend   Kind = "send"
	KindPlan   Kind = "plan"
)

// Check reports whether the concrete call must be confirmed by the owner and
// which danger kinds it matched. It reads the tool name and arguments only.
func Check(tool string, args json.RawMessage) (bool, []Kind) {
	name := strings.ToLower(strings.TrimSpace(tool))
	var kinds []Kind
	add := func(k Kind) {
		for _, seen := range kinds {
			if seen == k {
				return
			}
		}
		kinds = append(kinds, k)
	}
	switch {
	case name == "http_request":
		add(KindHTTP)
	case name == "shell":
		deletes, sends := ShellFlags(shellCommand(args))
		if deletes {
			add(KindDelete)
		}
		if sends {
			add(KindSend)
		}
	case name == "links":
		if strings.ToLower(linksAction(args)) == "remove" {
			add(KindDelete)
		}
	default:
		if isDeleteTool(name) {
			add(KindDelete)
		}
		if isSendTool(name) {
			add(KindSend)
		}
	}
	if isPlanTool(name) || isPlanPathTool(name, args) {
		add(KindPlan)
	}
	return len(kinds) > 0, kinds
}

// ShellFlags reports whether a shell command deletes files or content, or
// sends data out. It splits on shell operators so a delete or send buried in
// `sh -c`, a pipe, or a quoted string is still visible.
func ShellFlags(cmd string) (deletes, sends bool) {
	words := shellWords(cmd)
	for _, w := range words {
		switch filepath.Base(w) {
		case "rm", "unlink", "rmdir", "shred":
			deletes = true
		case "curl", "wget", "nc", "ncat", "netcat", "socat", "sendmail", "mail", "mailx":
			sends = true
		}
	}
	if strings.Contains(cmd, "-delete") {
		for _, w := range words {
			if filepath.Base(w) == "find" {
				deletes = true
			}
		}
	}
	for _, part := range []string{"os.remove", "os.unlink", "shutil.rmtree", "fs.rmSync", "fs.unlink", "fs.rm("} {
		if strings.Contains(cmd, part) {
			deletes = true
		}
	}
	low := strings.ToLower(cmd)
	for _, part := range []string{"requests", "urllib", "http.client", "httplib"} {
		if strings.Contains(low, part) {
			sends = true
		}
	}
	return deletes, sends
}

// IsPlanPath reports a file that records an agreed plan, so editing it is a
// plan change rather than an ordinary write.
func IsPlanPath(path string) bool {
	p := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(path), "\\", "/"))
	p = strings.TrimPrefix(p, "./")
	base := filepath.Base(p)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	if base == "plan" {
		return true
	}
	for _, token := range []string{"plan/", "plans/"} {
		if strings.HasPrefix(p, token) || strings.Contains(p, "/"+token) {
			return true
		}
	}
	return false
}

var deleteTools = map[string]bool{
	"delete_file": true, "delete_files": true, "remove_file": true,
	"file_delete": true, "unlink_file": true, "trash_file": true,
	"delete_bot": true, "remove_bot": true,
	"delete_secret": true, "secret_delete": true, "secret_rm": true, "remove_secret": true,
	"delete_link": true, "remove_link": true,
	"remove_mcp": true, "mcp_remove": true, "delete_mcp": true,
}

var sendTools = map[string]bool{
	"message_bot":  true,
	"post_group":   true,
	"send_message": true,
	"send_email":   true,
	"post_message": true,
	"send_webhook": true,
	"webhook_send": true,
}

var planTools = map[string]bool{
	"routine_create":  true,
	"routine_update":  true,
	"routine_delete":  true,
	"propose_persona": true,
	"propose_profile": true,
}

func isDeleteTool(name string) bool {
	if deleteTools[name] {
		return true
	}
	return strings.HasSuffix(name, "_delete") || strings.HasSuffix(name, "_remove")
}

func isSendTool(name string) bool {
	if sendTools[name] {
		return true
	}
	return strings.HasPrefix(name, "send_") || strings.HasSuffix(name, "_send")
}

func isPlanTool(name string) bool { return planTools[name] }

func isPlanPathTool(name string, args json.RawMessage) bool {
	if name != "write_file" && name != "edit_file" {
		return false
	}
	var in struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return false
	}
	return IsPlanPath(in.Path)
}

func shellCommand(args json.RawMessage) string {
	var in struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(args, &in)
	return in.Command
}

func linksAction(args json.RawMessage) string {
	var in struct {
		Action string `json:"action"`
	}
	_ = json.Unmarshal(args, &in)
	return in.Action
}

// shellWords splits a shell command on operators and quotes so a dangerous
// word inside `sh -c "..."`, a pipe, or a chain is still seen as a word.
func shellWords(cmd string) []string {
	cleaned := strings.Map(func(r rune) rune {
		switch r {
		case ';', '&', '|', '<', '>', '`', '(', ')', '\n', '\r', '$':
			return ' '
		default:
			return r
		}
	}, cmd)
	var out []string
	for _, f := range strings.Fields(cleaned) {
		f = strings.Trim(f, `"'`)
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}
