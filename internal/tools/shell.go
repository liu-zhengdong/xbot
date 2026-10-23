package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"xbot/internal/clip"
	"xbot/internal/notable"
)

const (
	defaultShellTimeout = 120 * time.Second
	maxShellTimeout     = 600 * time.Second
	maxShellOutput      = 32 << 10
	maxCommandBytes     = 16 << 10
)

// Shell runs a command through sh -c.
// Dir is the working directory. Empty means the process working directory.
// Shared, when set with Dir, is exported as XBOT_SHARED.
// KeyEnv names extra provider API key variables to omit unless the call sets keys.
// Jobs, when set, keeps a command that outlives the timeout. Without it the
// command is killed at the timeout, which is what node and eval shells do.
type Shell struct {
	DefaultTimeout time.Duration
	MaxTimeout     time.Duration
	MaxOutput      int
	Dir            string
	Shared         string
	KeyEnv         []string
	Jobs           *Jobs
	jobsOnce       sync.Once

	// startDir and afterDir persist the working directory for one execution
	// context. Nil leaves every call in Dir. The mutex covers the func fields
	// and markerDir; Run copies them before it starts the process.
	sessMu    sync.Mutex
	startDir  func(context.Context) (dir, notice string)
	afterDir  func(context.Context, string) (kept, notice string)
	markerDir string
}

// BindSession installs the working directory for this shell. start reports
// where this call begins and an optional notice. after receives the directory
// a foreground command finished in and returns the directory to show, plus a
// notice when the candidate was rejected. Both receive the call context so one
// shell can keep a different directory per turn. Either function may be nil.
func (s *Shell) BindSession(start func(context.Context) (string, string), after func(context.Context, string) (string, string)) {
	if s == nil {
		return
	}
	s.sessMu.Lock()
	s.startDir = start
	s.afterDir = after
	s.sessMu.Unlock()
}

// SetMarkerDir is the directory for the cwd marker file. Empty stays empty
// until a later call. The marker is never created in the process temp dir.
func (s *Shell) SetMarkerDir(dir string) {
	if s == nil || dir == "" {
		return
	}
	s.sessMu.Lock()
	if s.markerDir == "" {
		s.markerDir = dir
	}
	s.sessMu.Unlock()
}

func (s *Shell) session() (func(context.Context) (string, string), func(context.Context, string) (string, string), string) {
	if s == nil {
		return nil, nil, ""
	}
	s.sessMu.Lock()
	defer s.sessMu.Unlock()
	return s.startDir, s.afterDir, s.markerDir
}

// BindJobs sets the job table once. Later calls do not write Jobs, so a shell
// that is already running can be attached without a data race.
func (s *Shell) BindJobs(j *Jobs) {
	if s == nil || j == nil {
		return
	}
	s.jobsOnce.Do(func() { s.Jobs = j })
}

var (
	keyEnvMu    sync.Mutex
	keyEnvNames = []string{"XBOT_API_KEY"}
	keyLookup   func(string) (string, bool)
	// machineGate reports this machine's switch: allow, ask, or off.
	// Nil means allow, so tests and older processes keep the previous rules.
	machineGate func() (string, error)
)

// SetMachineGate installs the local machine switch. off refuses shell.
// ask makes every command need the owner, including read-only ones.
func SetMachineGate(fn func() (string, error)) {
	keyEnvMu.Lock()
	machineGate = fn
	keyEnvMu.Unlock()
}

func localMachine() (string, error) {
	keyEnvMu.Lock()
	fn := machineGate
	keyEnvMu.Unlock()
	if fn == nil {
		return "allow", nil
	}
	return fn()
}

// machineOff reports the local machine switch is off, which refuses both
// shell and the file tools. The kernel review path already refuses an off
// machine; this is the last line of defense for a tool call that reaches Run
// directly.
func machineOff() error {
	mode, err := localMachine()
	if err != nil {
		return err
	}
	if mode == "off" {
		return errors.New("machine local is off")
	}
	return nil
}

// SetKeyLookup supplies provider key values kept in memory after they are
// removed from the process environment. keys:true puts them back for that call.
func SetKeyLookup(fn func(string) (string, bool)) {
	keyEnvMu.Lock()
	keyLookup = fn
	keyEnvMu.Unlock()
}

// UseKeyEnv sets provider API key variable names that shell commands omit
// unless the call sets keys. XBOT_API_KEY is always included.
func UseKeyEnv(names []string) {
	seen := map[string]bool{"XBOT_API_KEY": true}
	out := []string{"XBOT_API_KEY"}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] || strings.ContainsAny(name, "=\x00") {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	keyEnvMu.Lock()
	keyEnvNames = out
	keyEnvMu.Unlock()
}

// AddKeyEnv adds names to the set shell commands omit unless keys is set.
// configureWeb uses it when web.json gains a search key after startup.
func AddKeyEnv(names []string) {
	keyEnvMu.Lock()
	defer keyEnvMu.Unlock()
	seen := map[string]bool{}
	for _, name := range keyEnvNames {
		seen[name] = true
	}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] || strings.ContainsAny(name, "=\x00") {
			continue
		}
		seen[name] = true
		keyEnvNames = append(keyEnvNames, name)
	}
}

// NewShell returns a shell tool with the default limits.
func NewShell() *Shell {
	return &Shell{
		DefaultTimeout: defaultShellTimeout,
		MaxTimeout:     maxShellTimeout,
		MaxOutput:      maxShellOutput,
	}
}

func (s *Shell) Name() string { return "shell" }
func (s *Shell) WaitPolicy(args json.RawMessage) (time.Duration, bool) {
	in, err := parseShellArgs(args)
	if err != nil {
		return DefaultToolWait, false
	}
	return s.duration(in.TimeoutSeconds), in.RunInBackground
}

func (s *Shell) timeoutLabel() (def, max int) {
	d := defaultShellTimeout
	m := maxShellTimeout
	if s != nil && s.DefaultTimeout > 0 {
		d = s.DefaultTimeout
	}
	if s != nil && s.MaxTimeout > 0 {
		m = s.MaxTimeout
	}
	return int(d / time.Second), int(m / time.Second)
}

func (s *Shell) Description() string {
	def, max := s.timeoutLabel()
	_, after, _ := s.session()
	persist := ""
	if after != nil {
		persist = " The working directory persists across calls for this bot. Each result includes a cwd line. A directory that is missing or outside the workspace falls back to the workspace."
	}
	if s != nil && s.Jobs != nil {
		return "Run a shell command; stdout and stderr are combined. The foreground waits at most 2 seconds for any tool. timeout_seconds can shorten this wait, never extend it. A running call moves to the background and is not killed. run_in_background returns immediately. shell_output reads output (offset or tail), shell_status checks status, shell_cancel requests cancellation. A normal completion message arrives; do not poll in a tight loop. Provider keys stay out unless keys is true and approved." + persist
	}
	return fmt.Sprintf("Run a shell command. stdout and stderr are combined and truncated. The default timeout is %d seconds and the maximum is %d; pass timeout_seconds to change it. The command stops at the timeout. Provider API keys are left out of the environment unless keys is true, which the owner must approve. The local machine switch can refuse the command, require approval for every command, or in allow mode ask the owner only for deletions and external sends.%s", def, max, persist)
}

func (s *Shell) Parameters() any {
	def, max := s.timeoutLabel()
	if s.Jobs != nil {
		def, max = 2, 2
	}
	props := map[string]any{
		"command":         stringProp("Shell command to run"),
		"timeout_seconds": intProp(fmt.Sprintf("Timeout in seconds (default %d, max %d)", def, max)),
		"keys":            boolProp("Pass provider API keys through. The owner must approve."),
	}
	if s != nil && s.Jobs != nil {
		props["run_in_background"] = boolProp("Start in the background immediately and return a job id. Do not use this to sleep or to poll.")
	}
	return objectSchema(props, []string{"command"})
}

type shellArgs struct {
	Command         string `json:"command"`
	TimeoutSeconds  int    `json:"timeout_seconds"`
	Keys            bool   `json:"keys"`
	RunInBackground bool   `json:"run_in_background"`
}

// Confirm only builds the card summary. The kernel review path decides
// whether the call needs the owner: a delete, an external send, or keys:true.
func (s *Shell) Confirm(args json.RawMessage) (bool, string, error) {
	in, err := parseShellArgs(args)
	if err != nil {
		return false, "", err
	}
	if in.Keys {
		return false, in.Command + "\nwith provider keys", nil
	}
	return false, in.Command, nil
}

func (s *Shell) Run(ctx context.Context, args json.RawMessage) (string, error) {
	in, err := parseShellArgs(args)
	if err != nil {
		return "", err
	}
	if err := machineOff(); err != nil {
		return "", err
	}
	timeout := s.duration(in.TimeoutSeconds)
	startDir, afterDir, markerDir := s.session()
	dir := s.Dir
	notice := ""
	if startDir != nil {
		dir, notice = startDir(ctx)
	}
	env := s.commandEnv(in.Keys)
	command := in.Command
	var marker string
	var ended string
	// cwdCh is written by the job's finish goroutine and read only on the
	// path that actually waited. A late send after a background return does
	// not touch ended.
	var cwdCh chan string
	if startDir != nil {
		markDir := markerDir
		if markDir == "" {
			markDir = s.Dir
		}
		if markDir == "" {
			return "", errors.New("shell: no directory for the cwd marker")
		}
		f, err := os.CreateTemp(markDir, "xbot-cwd-*.txt")
		if err != nil {
			return "", err
		}
		marker = f.Name()
		f.Close()
		command = wrapShellCwd(dir, in.Command, marker)
		cwdCh = make(chan string, 1)
	}
	// The shared Jobs runner owns the wait for every command.
	var out string
	if s.Jobs != nil {
		out, err = s.Jobs.Run(ctx, RunRequest{
			Command:    in.Command,
			Exec:       command,
			Timeout:    timeout,
			MaxOutput:  s.maxOutput(),
			Dir:        dir,
			Env:        env,
			Background: in.RunInBackground,
			CwdFile:    marker,
			OnCwd: func(got string) {
				if cwdCh == nil || got == "" {
					return
				}
				select {
				case cwdCh <- got:
				default:
				}
			},
		})
	} else {
		out, err = runCommand(ctx, command, timeout, s.maxOutput(), dir, env)
		if marker != "" {
			ended = readCwdFile(marker)
			_ = os.Remove(marker)
		}
	}
	if err != nil {
		if marker != "" && s.Jobs != nil {
			// A foreground failure returns before finish; a background failure
			// already removed the marker. Remove is idempotent.
			_ = os.Remove(marker)
		}
		return "", err
	}
	if cwdCh != nil && !strings.HasPrefix(out, "background ") {
		select {
		case ended = <-cwdCh:
		default:
		}
	}
	if strings.HasPrefix(out, "exit 0\n") || out == "exit 0" {
		notable.Shell(ctx, in.Command, dir, env)
	}
	cwd := dir
	if ended != "" && !strings.HasPrefix(out, "background ") {
		cwd = ended
		if afterDir != nil {
			kept, extra := afterDir(ctx, ended)
			if kept != "" {
				cwd = kept
			}
			notice = joinNotice(notice, extra)
		}
	}
	if startDir != nil {
		// A background result already carries the start directory. Adding it
		// again would put two cwd lines on the same result.
		show := cwd
		if strings.HasPrefix(out, "background ") {
			show = ""
		}
		out = withCwd(out, show, notice)
	}
	return out, nil
}

func (s *Shell) commandEnv(passKeys bool) []string {
	blocked := s.blockedKeys()
	extra := map[string]string{}
	if passKeys {
		extra = heldFor(blocked)
	}
	if s.Dir == "" && s.Shared == "" && len(extra) == 0 && (passKeys || len(blocked) == 0) {
		return nil
	}
	env := append([]string{}, os.Environ()...)
	if passKeys {
		env = mergeHeld(env, extra)
	} else {
		env = stripEnv(env, blocked)
	}
	if s.Dir != "" {
		env = append(env, "XBOT_WORK="+s.Dir)
	}
	if s.Shared != "" {
		env = append(env, "XBOT_SHARED="+s.Shared)
	}
	return env
}

func heldFor(names []string) map[string]string {
	keyEnvMu.Lock()
	fn := keyLookup
	keyEnvMu.Unlock()
	if fn == nil {
		return nil
	}
	out := map[string]string{}
	for _, name := range names {
		if value, ok := fn(name); ok {
			out[name] = value
		}
	}
	return out
}

func mergeHeld(env []string, extra map[string]string) []string {
	if len(extra) == 0 {
		return env
	}
	names := make([]string, 0, len(extra))
	for name := range extra {
		names = append(names, name)
	}
	env = stripEnv(env, names)
	for _, name := range names {
		env = append(env, name+"="+extra[name])
	}
	return env
}

func (s *Shell) blockedKeys() []string {
	keyEnvMu.Lock()
	out := append([]string{}, keyEnvNames...)
	keyEnvMu.Unlock()
	seen := map[string]bool{}
	for _, name := range out {
		seen[name] = true
	}
	if s != nil {
		for _, name := range s.KeyEnv {
			name = strings.TrimSpace(name)
			if name == "" || seen[name] || strings.ContainsAny(name, "=\x00") {
				continue
			}
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

func stripEnv(env, names []string) []string {
	drop := map[string]bool{}
	for _, name := range names {
		drop[name] = true
	}
	out := make([]string, 0, len(env))
	for _, entry := range env {
		key, _, ok := strings.Cut(entry, "=")
		if ok && drop[key] {
			continue
		}
		out = append(out, entry)
	}
	return out
}

func (s *Shell) duration(seconds int) time.Duration {
	def := s.DefaultTimeout
	if def <= 0 {
		def = defaultShellTimeout
	}
	max := s.MaxTimeout
	if max <= 0 {
		max = maxShellTimeout
	}
	d := def
	if seconds > 0 {
		d = time.Duration(seconds) * time.Second
	}
	if d > max {
		d = max
	}
	if d <= 0 {
		d = time.Second
	}
	return d
}

func (s *Shell) maxOutput() int {
	if s.MaxOutput <= 0 {
		return maxShellOutput
	}
	return s.MaxOutput
}

func parseShellArgs(args json.RawMessage) (shellArgs, error) {
	var in shellArgs
	if err := json.Unmarshal(args, &in); err != nil {
		return shellArgs{}, fmt.Errorf("shell: %w", err)
	}
	in.Command = strings.TrimSpace(in.Command)
	if in.Command == "" {
		return shellArgs{}, errors.New("shell: command is empty")
	}
	if len(in.Command) > maxCommandBytes {
		return shellArgs{}, errors.New("shell: command is too long")
	}
	return in, nil
}

type capWriter struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	max       int
	truncated bool
}

func (w *capWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.buf.Len() >= w.max {
		w.truncated = true
		return len(p), nil
	}
	remain := w.max - w.buf.Len()
	if len(p) > remain {
		_, _ = w.buf.Write(p[:remain])
		w.truncated = true
		return len(p), nil
	}
	_, _ = w.buf.Write(p)
	return len(p), nil
}

func (w *capWriter) String() (string, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String(), w.truncated
}

func runCommand(ctx context.Context, command string, timeout time.Duration, maxOut int, dir string, env []string) (string, error) {
	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(tctx, "sh", "-c", command)
	if dir != "" {
		cmd.Dir = dir
	}
	if len(env) > 0 {
		cmd.Env = env
	}
	configureCmd(cmd)
	out := &capWriter{max: maxOut}
	cmd.Stdout = out
	cmd.Stderr = out
	err := cmd.Run()
	text, truncated := out.String()
	text = clip.Valid(text)
	text = strings.TrimRight(text, "\n")
	if errors.Is(tctx.Err(), context.DeadlineExceeded) {
		return formatShell("timed out after "+timeout.String(), text, truncated), nil
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return "", ctx.Err()
	}
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return formatShell(fmt.Sprintf("exit %d", exit.ExitCode()), text, truncated), nil
		}
		return "", fmt.Errorf("shell: %w", err)
	}
	return formatShell("exit 0", text, truncated), nil
}

func formatShell(status, out string, truncated bool) string {
	var b strings.Builder
	b.WriteString(status)
	if out != "" {
		b.WriteByte('\n')
		b.WriteString(out)
	}
	if truncated {
		b.WriteString("\n[truncated]")
	}
	return b.String()
}

// wrapShellCwd runs command after cd, and records the physical directory on
// the way out, including an explicit exit and a set -e abort. The trap writes
// only the marker file.
func wrapShellCwd(start, command, marker string) string {
	var b strings.Builder
	if start != "" {
		b.WriteString("cd ")
		b.WriteString(shQuote(start))
		b.WriteString(" || exit 97\n")
	}
	b.WriteString("trap 'pwd -P > ")
	b.WriteString(shQuote(marker))
	b.WriteString("' EXIT\n")
	b.WriteString(command)
	if !strings.HasSuffix(command, "\n") {
		b.WriteByte('\n')
	}
	return b.String()
}

func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func readCwdFile(path string) string {
	if path == "" {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if len(b) > 4096 {
		return ""
	}
	s := strings.TrimSpace(string(b))
	if s == "" || strings.ContainsAny(s, "\r\n\x00") || !filepath.IsAbs(s) {
		return ""
	}
	return filepath.Clean(s)
}

func withCwd(result, cwd, notice string) string {
	if cwd == "" && notice == "" {
		return result
	}
	line, rest, found := strings.Cut(result, "\n")
	var b strings.Builder
	b.WriteString(line)
	if cwd != "" {
		b.WriteString("\ncwd: ")
		b.WriteString(cwd)
	}
	if notice != "" {
		b.WriteByte('\n')
		b.WriteString(notice)
	}
	if found {
		b.WriteByte('\n')
		b.WriteString(rest)
	}
	return b.String()
}

func joinNotice(a, b string) string {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" {
		return b
	}
	if b == "" || a == b {
		return a
	}
	return a + "\n" + b
}
