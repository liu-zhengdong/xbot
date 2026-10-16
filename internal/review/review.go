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
)

// Check reports whether the concrete call must be confirmed by the owner and
// which danger kinds it matched. It reads the tool name and arguments only.
//
// It asks for a send only when data leaves XBot: shell tools that write to the
// network, or an external channel send tool. A message between the owner's own
// bots is not an egress and does not ask here. Deletions of files or data ask.
// http_request asks only when it injects a secret or uses a method other than
// GET/HEAD. Editing a planned file, routine or persona is not this classifier's
// business; those tools keep whatever confirmation they had before. A routine
// is the bot's own schedule, so routine_delete is not a data deletion here.
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
		if httpNeedsOwner(args) {
			add(KindHTTP)
		}
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
	case strings.HasPrefix(name, "routine_"):
		// Creating, updating, pausing or deleting a routine is the bot
		// managing its own schedule, not owner data. It never matches here.
	default:
		if isDeleteTool(name) {
			add(KindDelete)
		}
		if isSendTool(name) {
			add(KindSend)
		}
	}
	return len(kinds) > 0, kinds
}

// httpNeedsOwner reports whether an http_request must be confirmed: it injects
// a {{secret:NAME}} placeholder anywhere, or its method is not GET or HEAD.
func httpNeedsOwner(args json.RawMessage) bool {
	if strings.Contains(string(args), "{{secret:") {
		return true
	}
	var in struct {
		Method string `json:"method"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return false
	}
	method := strings.ToUpper(strings.TrimSpace(in.Method))
	return method != "" && method != "GET" && method != "HEAD"
}

// ShellFlags reports whether a shell command deletes files or content, or
// sends data out of XBot. It splits on shell operators so a delete or send
// buried in `sh -c`, a pipe, or a quoted string is still visible.
func ShellFlags(cmd string) (deletes, sends bool) {
	words := shellWords(cmd)
	for _, w := range words {
		switch filepath.Base(w) {
		case "rm", "unlink", "rmdir", "shred":
			deletes = true
		case "sendmail", "mail", "mailx", "nc", "ncat", "netcat":
			sends = true
		}
	}
	if strings.Contains(cmd, "-delete") && containsBase(words, "find") {
		deletes = true
	}
	if gitClean(words) {
		deletes = true
	}
	for _, part := range []string{"os.remove", "os.unlink", "shutil.rmtree", "fs.rmSync", "fs.unlink", "fs.rm("} {
		if strings.Contains(cmd, part) {
			deletes = true
		}
	}
	if curlWrites(words) || wgetWrites(words) || pythonWrites(cmd) {
		sends = true
	}
	if (containsBase(words, "scp") || containsBase(words, "rsync")) && remoteTarget(words) {
		sends = true
	}
	return deletes, sends
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
	"send_message": true,
	"send_email":   true,
	"post_message": true,
	"send_webhook": true,
	"webhook_send": true,
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

func containsBase(words []string, want string) bool {
	for _, w := range words {
		if filepath.Base(w) == want {
			return true
		}
	}
	return false
}

func isWriteMethod(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case "POST", "PUT", "PATCH":
		return true
	default:
		return false
	}
}

// curlWrites reports a curl invocation that sends a body or uploads a file:
// -d/--data*, -F/--form, -T/--upload-file, --json, or -X/--request with a
// write method. A plain GET/HEAD fetch is not a send.
func curlWrites(words []string) bool {
	if !containsBase(words, "curl") {
		return false
	}
	for i, w := range words {
		switch {
		case w == "-d" || strings.HasPrefix(w, "--data"):
			return true
		case w == "-F" || strings.HasPrefix(w, "--form"):
			return true
		case w == "-T" || strings.HasPrefix(w, "--upload-file"):
			return true
		case w == "--json":
			return true
		case (w == "-X" || w == "--request") && i+1 < len(words):
			if isWriteMethod(words[i+1]) {
				return true
			}
		case strings.HasPrefix(w, "-X"):
			if isWriteMethod(strings.TrimPrefix(w, "-X")) {
				return true
			}
		case strings.HasPrefix(w, "--request="):
			if isWriteMethod(strings.TrimPrefix(w, "--request=")) {
				return true
			}
		}
	}
	return false
}

// wgetWrites reports a wget invocation that posts a body.
func wgetWrites(words []string) bool {
	if !containsBase(words, "wget") {
		return false
	}
	for i, w := range words {
		switch {
		case strings.HasPrefix(w, "--post-data"), strings.HasPrefix(w, "--post-file"),
			strings.HasPrefix(w, "--body-data"), strings.HasPrefix(w, "--body-file"):
			return true
		case w == "--method" && i+1 < len(words):
			if isWriteMethod(words[i+1]) {
				return true
			}
		case strings.HasPrefix(w, "--method="):
			if isWriteMethod(strings.TrimPrefix(w, "--method=")) {
				return true
			}
		}
	}
	return false
}

// pythonWrites reports a Python one-liner or script that posts data out:
// requests.post/put/patch, an HTTP client request with a write method, or
// urllib/http.client with a data= body.
func pythonWrites(cmd string) bool {
	low := strings.ToLower(cmd)
	for _, part := range []string{"requests.post", "requests.put", "requests.patch"} {
		if strings.Contains(low, part) {
			return true
		}
	}
	if strings.Contains(low, ".request(") {
		for _, method := range []string{`"post"`, `'post'`, `"put"`, `'put'`, `"patch"`, `'patch'`} {
			if strings.Contains(low, method) {
				return true
			}
		}
	}
	if strings.Contains(low, "urllib") || strings.Contains(low, "http.client") || strings.Contains(low, "httplib") {
		if strings.Contains(low, "data=") {
			return true
		}
	}
	return false
}

// remoteTarget reports whether any argument names a remote host: a URL scheme,
// or a host:path (with or without user@) rather than a local path.
func remoteTarget(words []string) bool {
	for _, w := range words {
		if strings.Contains(w, "://") {
			return true
		}
		if strings.HasPrefix(w, "-") {
			continue
		}
		if strings.Contains(w, ":") && !strings.Contains(w, "::") {
			return true
		}
	}
	return false
}

// gitClean reports a `git clean` subcommand, allowing global options such as
// -C <path> before it. A different subcommand (git log --grep clean) is not a
// clean.
func gitClean(words []string) bool {
	for i, w := range words {
		if filepath.Base(w) != "git" {
			continue
		}
		for j := i + 1; j < len(words); {
			arg := words[j]
			switch {
			case arg == "-C" || arg == "-c":
				j += 2
			case strings.HasPrefix(arg, "-"):
				j++
			default:
				return arg == "clean"
			}
		}
	}
	return false
}
