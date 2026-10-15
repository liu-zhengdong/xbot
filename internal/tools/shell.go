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
)

const (
	defaultShellTimeout = 30 * time.Second
	maxShellTimeout     = 120 * time.Second
	maxShellOutput      = 32 << 10
	maxCommandBytes     = 16 << 10
)

// Shell runs a command through sh -c.
// Dir is the working directory. Empty means the process working directory.
// Shared, when set with Dir, is exported as XBOT_SHARED.
// KeyEnv names extra provider API key variables to omit unless the call sets keys.
type Shell struct {
	DefaultTimeout time.Duration
	MaxTimeout     time.Duration
	MaxOutput      int
	Dir            string
	Shared         string
	KeyEnv         []string
}

var (
	keyEnvMu    sync.Mutex
	keyEnvNames = []string{"XBOT_API_KEY"}
	keyLookup   func(string) (string, bool)
)

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

// NewShell returns a shell tool with the default limits.
func NewShell() *Shell {
	return &Shell{
		DefaultTimeout: defaultShellTimeout,
		MaxTimeout:     maxShellTimeout,
		MaxOutput:      maxShellOutput,
	}
}

func (s *Shell) Name() string { return "shell" }

func (s *Shell) Description() string {
	return "Run a shell command. stdout and stderr are combined and truncated. The command stops at the timeout. Provider API keys are left out of the environment unless keys is true, which the owner must approve."
}

func (s *Shell) Parameters() any {
	return objectSchema(map[string]any{
		"command":         stringProp("Shell command to run"),
		"timeout_seconds": intProp("Timeout in seconds (default 30, max 120)"),
		"keys":            boolProp("Pass provider API keys through. The owner must approve."),
	}, []string{"command"})
}

type shellArgs struct {
	Command        string `json:"command"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	Keys           bool   `json:"keys"`
}

// safeCommands are read-only lookups. A command is safe only when it is a
// single simple invocation of one of these names, with no shell operators.
var safeCommands = map[string]bool{
	"date": true, "pwd": true, "whoami": true, "uname": true, "hostname": true,
	"id": true, "echo": true, "printf": true, "true": true, "false": true,
	"uptime": true, "ls": true, "which": true, "env": true, "printenv": true, "cal": true,
}

func (s *Shell) Confirm(args json.RawMessage) (bool, string, error) {
	in, err := parseShellArgs(args)
	if err != nil {
		return false, "", err
	}
	if in.Keys {
		return true, in.Command + "\nwith provider keys", nil
	}
	if safeShell(in.Command) {
		return false, in.Command, nil
	}
	return true, in.Command, nil
}

func safeShell(command string) bool {
	if strings.ContainsAny(command, "\n;&|<>`$\\") || strings.Contains(command, "(") || strings.Contains(command, ")") {
		return false
	}
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return false
	}
	return safeCommands[filepath.Base(fields[0])]
}

func (s *Shell) Run(ctx context.Context, args json.RawMessage) (string, error) {
	in, err := parseShellArgs(args)
	if err != nil {
		return "", err
	}
	timeout := s.duration(in.TimeoutSeconds)
	return runCommand(ctx, in.Command, timeout, s.maxOutput(), s.Dir, s.commandEnv(in.Keys))
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
