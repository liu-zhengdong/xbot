package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
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
type Shell struct {
	DefaultTimeout time.Duration
	MaxTimeout     time.Duration
	MaxOutput      int
	Dir            string
	Shared         string
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
	return "Run a shell command. stdout and stderr are combined and truncated. The command stops at the timeout."
}

func (s *Shell) Parameters() any {
	return objectSchema(map[string]any{
		"command":         stringProp("Shell command to run"),
		"timeout_seconds": intProp("Timeout in seconds (default 30, max 120)"),
	}, []string{"command"})
}

type shellArgs struct {
	Command        string `json:"command"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

func (s *Shell) Confirm(args json.RawMessage) (bool, string, error) {
	in, err := parseShellArgs(args)
	if err != nil {
		return false, "", err
	}
	return true, in.Command, nil
}

func (s *Shell) Run(ctx context.Context, args json.RawMessage) (string, error) {
	in, err := parseShellArgs(args)
	if err != nil {
		return "", err
	}
	timeout := s.duration(in.TimeoutSeconds)
	return runCommand(ctx, in.Command, timeout, s.maxOutput(), s.Dir, s.commandEnv())
}

func (s *Shell) commandEnv() []string {
	if s.Dir == "" && s.Shared == "" {
		return nil
	}
	env := os.Environ()
	if s.Dir != "" {
		env = append(env, "XBOT_WORK="+s.Dir)
	}
	if s.Shared != "" {
		env = append(env, "XBOT_SHARED="+s.Shared)
	}
	return env
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
