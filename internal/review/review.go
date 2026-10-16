// Package review holds the single policy table for tool permissions. The
// kernel asks it what class a tool belongs to, then decides whether the owner
// must confirm the concrete call. Tools never decide for themselves.
package review

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

// Class is the one category a tool belongs to. The kernel review path reads
// the class and the call arguments; it never trusts how the model describes
// the call or what a tool's own Confirm says.
type Class string

const (
	// Readonly tools only observe; they never ask.
	Readonly Class = "readonly"
	// Normal tools do routine work that does not leave XBot or destroy data.
	Normal Class = "normal"
	// Delete tools destroy files or records; they always ask.
	Delete Class = "delete"
	// ExternalSend tools send data out of XBot or install an external link;
	// they always ask.
	ExternalSend Class = "external_send"
	// HTTP tools ask only when the call injects a secret or uses a method
	// other than GET/HEAD.
	HTTP Class = "http"
	// Shell tools ask only when the command deletes or sends data out.
	Shell Class = "shell"
	// SelfEdit tools change the bot's own persona or profile; they ask.
	SelfEdit Class = "self_edit"
)

// Table is the one explicit policy table. Every tool any link or the kernel
// can register has an entry here. MCP tools cannot be listed by name, so
// mcp_* names resolve through the rule in ClassOf.
//
// Keep this table in sync with the tools that are actually registered: the
// guard test fails when a registered tool has no entry.
var Table = map[string]Class{
	// Built-in tools link: shell and files.
	"shell":        Shell,
	"read_file":    Readonly,
	"write_file":   SelfEdit,
	"edit_file":    SelfEdit,
	"http_request": HTTP,
	"list_secrets": Readonly,

	// Memory.
	"remember": Normal,
	"forget":   Normal,
	"recall":   Readonly,

	// Persona and profile.
	"propose_persona": SelfEdit,
	"propose_profile": SelfEdit,

	// Routines and the owner notification. A routine is the bot's own
	// schedule, so deleting one is not a data deletion.
	"routine_create": Normal,
	"routine_list":   Readonly,
	"routine_update": Normal,
	"routine_delete": Normal,
	"routine_pause":  Normal,
	"notify_owner":   Normal,

	// Context.
	"compact":        Normal,
	"recall_history": Readonly,
	"expand":         Readonly,

	// Subagents.
	"spawn":           Normal,
	"steer":           Normal,
	"queue":           Normal,
	"interrupt":       Normal,
	"cancel":          Normal,
	"read_subagent":   Readonly,
	"expand_subagent": Readonly,

	// Skills.
	"read_skill":  Readonly,
	"write_skill": Normal,

	// The owner question tool.
	"ask_user": Normal,

	// Bot management.
	"create_bot":  Normal,
	"delete_bot":  Delete,
	"pin_bot":     Normal,
	"set_primary": Normal,

	// Peers and groups. A message between the owner's own bots is not egress.
	"list_bots":    Readonly,
	"message_bot":  Normal,
	"list_groups":  Readonly,
	"create_group": Normal,
	"post_group":   Normal,
	"fork_bot":     Normal,

	// Link catalog. install and remove ask; search and load do not.
	"links": ExternalSend,

	// Connectors.
	"telegram_send": ExternalSend,
	"lark_send":     ExternalSend,

	// Coding-agent tasks.
	"code_start":     Normal,
	"code_steer":     Normal,
	"code_queue":     Normal,
	"code_interrupt": Normal,
	"code_cancel":    Normal,
	"code_show":      Readonly,

	// Node calls. A shell command runs on the other machine.
	"node_list": Readonly,
	"node_call": Shell,

	// Desktop.
	"desktop_screenshot": Readonly,
	"desktop_click":      Normal,
	"desktop_type":       Normal,
	"desktop_scroll":     Normal,
	"desktop_key":        Normal,

	// Browser. browser_stop stays available to close the process.
	"browser_open":     Normal,
	"browser_snapshot": Readonly,
	"browser_click":    Normal,
	"browser_stop":     Normal,

	// Delete tools that other links may register. Kept explicit so the review
	// never has to guess from a name suffix.
	"delete_file":   Delete,
	"delete_files":  Delete,
	"remove_file":   Delete,
	"file_delete":   Delete,
	"unlink_file":   Delete,
	"trash_file":    Delete,
	"delete_secret": Delete,
	"secret_delete": Delete,
	"secret_rm":     Delete,
	"remove_secret": Delete,
	"delete_link":   Delete,
	"remove_link":   Delete,
	"remove_mcp":    Delete,
	"mcp_remove":    Delete,
	"delete_mcp":    Delete,

	// External send tools that other links may register.
	"send_message": ExternalSend,
	"send_email":   ExternalSend,
	"post_message": ExternalSend,
	"send_webhook": ExternalSend,
	"webhook_send": ExternalSend,
}

// mcpPrefix marks a tool discovered from an MCP server. The server chooses the
// remote name, so the local tool name is dynamic and cannot be listed.
const mcpPrefix = "mcp_"

// ClassOf returns the class for a tool name. MCP tools are an external send:
// the call leaves XBot and the remote name is dynamic. A name with no entry
// falls back to Normal so a custom or not-yet-listed tool does not change
// behavior at runtime; Known reports it and the guard test rejects it.
// ReadOnlyHinter is implemented by tools that carry an MCP readOnlyHint.
type ReadOnlyHinter interface{ ReadOnlyHint() bool }

// ClassOfMCP returns Readonly for an mcp_* tool whose server marked it
// readOnlyHint, and ClassOf(tool) otherwise.
func ClassOfMCP(tool string, readOnlyHint bool) Class {
	if readOnlyHint && strings.HasPrefix(normalize(tool), mcpPrefix) {
		return Readonly
	}
	return ClassOf(tool)
}

func ClassOf(tool string) Class {
	name := normalize(tool)
	if c, ok := Table[name]; ok {
		return c
	}
	if strings.HasPrefix(name, mcpPrefix) {
		return ExternalSend
	}
	return Normal
}

// Known reports whether the tool has an explicit table entry, or is an MCP
// tool covered by the mcp_* rule.
func Known(tool string) bool {
	name := normalize(tool)
	if _, ok := Table[name]; ok {
		return true
	}
	return strings.HasPrefix(name, mcpPrefix)
}

// MachineTouching reports whether the tool acts on the local machine. The
// kernel review path applies the machine switch to these tools: off refuses
// them, ask confirms every call, and allow follows the table. browser_stop is
// deliberately excluded so the owner can always close a Chrome process.
func MachineTouching(tool string) bool {
	switch normalize(tool) {
	case "shell", "read_file", "write_file", "edit_file",
		"desktop_screenshot", "desktop_click", "desktop_type", "desktop_scroll", "desktop_key",
		"browser_open", "browser_snapshot", "browser_click":
		return true
	default:
		return false
	}
}

// NeedsOwner reports whether the call must be confirmed by the owner, based on
// the class and the arguments alone. It does not consult the machine switch or
// the bot's own persona/profile paths; the kernel review path adds both.
func NeedsOwner(tool string, args json.RawMessage) bool {
	name := normalize(tool)
	switch ClassOf(name) {
	case Readonly, Normal:
		return false
	case Delete:
		return true
	case ExternalSend:
		// links is one tool with four actions. Only install and remove change
		// the bot's grants or secrets.
		if name == "links" {
			switch strings.ToLower(linksAction(args)) {
			case "install", "remove":
				return true
			default:
				return false
			}
		}
		return true
	case HTTP:
		return httpNeedsOwner(args)
	case Shell:
		if name == "shell" && shellKeys(args) {
			return true
		}
		deletes, sends := ShellFlags(shellCommand(args))
		return deletes || sends
	case SelfEdit:
		// write_file and edit_file are only a self-edit when the path is this
		// bot's own persona or profile; the kernel checks the resolved path.
		if name == "write_file" || name == "edit_file" {
			return false
		}
		return true
	default:
		return false
	}
}

func normalize(tool string) string {
	return strings.ToLower(strings.TrimSpace(tool))
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
	if gitForcePush(words) {
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

func shellCommand(args json.RawMessage) string {
	var in struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(args, &in)
	return in.Command
}

func shellKeys(args json.RawMessage) bool {
	var in struct {
		Keys bool `json:"keys"`
	}
	_ = json.Unmarshal(args, &in)
	return in.Keys
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
				if arg == "clean" {
					return true
				}
				j = len(words)
			}
		}
	}
	return false
}

// gitForcePush reports a `git push` that rewrites or deletes remote history:
// --force and its variants, -f, --delete/-d, --mirror, or a refspec beginning
// with '+' (force) or ':' (delete). Like gitClean it allows global options such
// as -C <path> before the subcommand, so a forced push buried in `sh -c` is
// still seen. A different subcommand (git log --force) is not a forced push.
func gitForcePush(words []string) bool {
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
				if arg == "push" && forcePushArgs(words[j+1:]) {
					return true
				}
				j = len(words)
			}
		}
	}
	return false
}

// forcePushArgs reports whether any push argument forces or deletes a ref.
func forcePushArgs(args []string) bool {
	for _, w := range args {
		switch w {
		case "--force", "-f", "--force-with-lease", "--force-if-includes",
			"--mirror", "--delete", "-d":
			return true
		}
		if strings.HasPrefix(w, "--force-with-lease=") {
			return true
		}
		if strings.HasPrefix(w, "+") || strings.HasPrefix(w, ":") {
			return true
		}
	}
	return false
}

// ResourceOf names what a call writes, for the background conflict queue: a file
// path for file edits, the working directory for shell and coding tasks. ok is
// false for calls that write nothing a background could be touching. Two calls
// conflict when they name the same resource.
func ResourceOf(name string, args json.RawMessage, workdir string) (string, bool) {
	var in struct {
		Path    string `json:"path"`
		Cwd     string `json:"cwd"`
		Workdir string `json:"workdir"`
		Repo    string `json:"repo"`
	}
	_ = json.Unmarshal(args, &in)
	switch name {
	case "write_file", "edit_file", "delete_file", "remove_file":
		if in.Path == "" {
			return "", false
		}
		p := in.Path
		if !filepath.IsAbs(p) && workdir != "" {
			p = filepath.Join(workdir, p)
		}
		return "file:" + filepath.Clean(p), true
	case "shell":
		if ShellIsReadOnly(args) {
			return "", false
		}
		dir := in.Cwd
		if dir == "" {
			dir = workdir
		}
		return "dir:" + filepath.Clean(dir), true
	case "code_start":
		if in.Repo != "" {
			return "repo:" + in.Repo, true
		}
		dir := in.Workdir
		if dir == "" {
			dir = workdir
		}
		return "dir:" + filepath.Clean(dir), true
	}
	return "", false
}

var readOnlyShell = map[string]bool{
	"ls": true, "cat": true, "pwd": true, "head": true, "tail": true, "grep": true, "rg": true,
	"wc": true, "date": true, "echo": true, "stat": true, "du": true, "df": true, "which": true, "file": true,
}

// ShellIsReadOnly reports a shell command made only of plain read commands: no
// redirection, no chaining, and a first word from a short list (plus git status, log, diff, show).
func ShellIsReadOnly(args json.RawMessage) bool {
	cmd := strings.TrimSpace(shellCommand(args))
	if cmd == "" || strings.ContainsAny(cmd, ">;&|`$(") {
		return false
	}
	w := shellWords(cmd)
	if len(w) == 0 {
		return false
	}
	if w[0] == "git" {
		return len(w) > 1 && (w[1] == "status" || w[1] == "log" || w[1] == "diff" || w[1] == "show")
	}
	return readOnlyShell[w[0]]
}
