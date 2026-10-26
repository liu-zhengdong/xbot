package eval

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

	"xbot/internal/review"
	"xbot/internal/tools"
)

// jailScript is the setup shell. The first format arg is the absolute chroot binary.
// PATH is only /usr/bin:/bin, matching a normal user who cannot see /usr/sbin.
const jailScript = `
set -u
PATH=/usr/bin:/bin
sandbox=$1
command=$2
root=$3
mkdir -p "$root/sandbox" "$root/usr" "$root/tmp" "$root/dev" "$root/etc" || exit 1
mount --bind "$sandbox" "$root/sandbox" || { echo "jail: sandbox mount failed" >&2; exit 1; }
mount --bind -o ro /usr "$root/usr" || { echo "jail: usr mount failed" >&2; exit 1; }
mkdir -p "$root/bin" "$root/lib" "$root/lib64"
if [ -e /bin ]; then mount --bind -o ro /bin "$root/bin" || true; fi
if [ -e /lib ]; then mount --bind -o ro /lib "$root/lib" || true; fi
if [ -e /lib64 ]; then mount --bind -o ro /lib64 "$root/lib64" || true; fi
for n in null zero urandom tty random; do
  if [ -e "/dev/$n" ]; then
    touch "$root/dev/$n"
    mount --bind "/dev/$n" "$root/dev/$n" || true
  fi
done
if [ -f /etc/ld.so.cache ]; then cp /etc/ld.so.cache "$root/etc/ld.so.cache" || true; fi
printf 'root:x:0:0:root:/sandbox:/bin/sh\n' > "$root/etc/passwd"
printf 'root:x:0:\n' > "$root/etc/group"
mkdir -p "$root/sandbox/tmp"
exec %s "$root" /usr/bin/env -i HOME=/sandbox TMPDIR=/sandbox/tmp PATH=/usr/bin:/bin LANG=C.UTF-8 \
  /bin/sh -c 'cd /sandbox && exec /bin/sh -c "$1"' _ "$2"
`

var (
	jailProbe sync.Once
	jailErr   error
	// chrootPath overrides the chroot lookup. Tests point it at a missing file.
	chrootPath string
)

func resetJail() {
	jailProbe = sync.Once{}
	jailErr = nil
}

// XBOT_EVAL_NO_JAIL=1 explicitly runs eval shell commands on the host.
// This switch is read only by the eval harness; normal tools are unchanged.
func evalNoJail() bool { return os.Getenv("XBOT_EVAL_NO_JAIL") == "1" }

// requireJail probes the sandbox once, unless the eval-only opt-out is set.
func requireJail() error {
	if evalNoJail() {
		return nil
	}
	jailProbe.Do(func() {
		sandbox, err := os.MkdirTemp("", "xbot-jail-probe-")
		if err != nil {
			jailErr = fmt.Errorf("eval: jail did not start: %w", err)
			return
		}
		defer os.RemoveAll(sandbox)
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		out, err := runJailed(ctx, sandbox, "true", 8*time.Second, false)
		if err != nil {
			jailErr = fmt.Errorf("eval: jail did not start: %w", err)
			return
		}
		if !strings.HasPrefix(out, "exit 0") {
			if strings.TrimSpace(out) == "" {
				out = "probe failed"
			}
			jailErr = fmt.Errorf("eval: jail did not start: %s", out)
		}
	})
	return jailErr
}

func findChroot() (string, error) {
	if chrootPath != "" {
		if _, err := os.Stat(chrootPath); err != nil {
			return "", fmt.Errorf("chroot %s: %w", chrootPath, err)
		}
		return chrootPath, nil
	}
	for _, p := range []string{"/usr/sbin/chroot", "/sbin/chroot"} {
		st, err := os.Stat(p)
		if err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return p, nil
		}
	}
	p, err := exec.LookPath("chroot")
	if err != nil {
		return "", errors.New("chroot not found")
	}
	return p, nil
}

func findUnshare() (string, error) {
	for _, p := range []string{"/usr/bin/unshare", "/bin/unshare"} {
		st, err := os.Stat(p)
		if err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return p, nil
		}
	}
	p, err := exec.LookPath("unshare")
	if err != nil {
		return "", errors.New("unshare is not available")
	}
	return p, nil
}

func runJailed(ctx context.Context, sandbox, command string, timeout time.Duration, network bool) (string, error) {
	chroot, err := findChroot()
	if err != nil {
		return "", err
	}
	unshare, err := findUnshare()
	if err != nil {
		return "", err
	}
	jailRoot, err := os.MkdirTemp("", "xbot-jail-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(jailRoot)
	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	args := []string{"--user", "--mount", "--map-root-user"}
	if !network {
		args = append(args, "--net")
	}
	args = append(args, "--", "/bin/sh", "-s", "--", sandbox, command, jailRoot)
	cmd := exec.CommandContext(tctx, unshare, args...)
	cmd.Env = []string{"PATH=/usr/bin:/bin"}
	cmd.Stdin = strings.NewReader(fmt.Sprintf(jailScript, chroot))
	return evalCommandResult(ctx, tctx, cmd, timeout)
}

// runUnjailed keeps the case directory, environment and output format while
// executing directly on the host. /sandbox remains the model-facing alias.
func runUnjailed(ctx context.Context, work, command string, timeout time.Duration) (string, error) {
	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	command = strings.ReplaceAll(command, caseWorkdir, work)
	cmd := exec.CommandContext(tctx, "/bin/sh", "-c", command)
	cmd.Dir = work
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + work, "TMPDIR=" + work + "/tmp", "LANG=C.UTF-8"}
	if err := os.MkdirAll(work+"/tmp", 0700); err != nil {
		return "", err
	}
	out, err := evalCommandResult(ctx, tctx, cmd, timeout)
	return tools.RewriteHost(work, "", out), err
}

func evalCommandResult(ctx, tctx context.Context, cmd *exec.Cmd, timeout time.Duration) (string, error) {
	var buf bytes.Buffer
	cmd.Stdout = &limitWriter{w: &buf, max: 32 << 10}
	cmd.Stderr = cmd.Stdout
	err := cmd.Run()
	text := strings.TrimRight(buf.String(), "\n")
	if errors.Is(tctx.Err(), context.DeadlineExceeded) {
		return "timed out after " + timeout.String() + suffix(text), nil
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return "", ctx.Err()
	}
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return fmt.Sprintf("exit %d%s", exit.ExitCode(), suffix(text)), nil
		}
		return "", fmt.Errorf("shell: %w", err)
	}
	return "exit 0" + suffix(text), nil
}

func suffix(text string) string {
	if text == "" {
		return ""
	}
	return "\n" + text
}

type limitWriter struct {
	w   *bytes.Buffer
	max int
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if l.w.Len() >= l.max {
		return len(p), nil
	}
	remain := l.max - l.w.Len()
	if len(p) > remain {
		_, _ = l.w.Write(p[:remain])
		return len(p), nil
	}
	_, _ = l.w.Write(p)
	return len(p), nil
}

// evalShell is the shell tool offered inside an eval case.
// It never inherits the process environment; the eval-only opt-out skips the jail.
// network is false unless the case has to attempt a connection.
type evalShell struct {
	root    string
	network bool
	inner   *tools.Shell
}

func newEvalShell(root string, network bool) *evalShell {
	inner := tools.NewShell()
	inner.Dir = root
	// The jail stops the command. These limits are the ones Run enforces;
	// the model-facing description is read from the inner shell.
	inner.DefaultTimeout = 30 * time.Second
	inner.MaxTimeout = 120 * time.Second
	return &evalShell{root: root, network: network, inner: inner}
}

func (e *evalShell) Name() string        { return e.inner.Name() }
func (e *evalShell) Description() string { return e.inner.Description() }
func (e *evalShell) Parameters() any     { return e.inner.Parameters() }

func (e *evalShell) Confirm(args json.RawMessage) (bool, string, error) {
	return e.inner.Confirm(args)
}

func (e *evalShell) Run(ctx context.Context, args json.RawMessage) (string, error) {
	if _, _, err := e.inner.Confirm(args); err != nil {
		return "", err
	}
	var in struct {
		Command        string `json:"command"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("shell: %w", err)
	}
	in.Command = strings.TrimSpace(in.Command)
	if in.Command == "" {
		return "", errors.New("shell: command is empty")
	}
	if len(in.Command) > 16<<10 {
		return "", errors.New("shell: command is too long")
	}
	if err := requireJail(); err != nil {
		return "", err
	}
	timeout := 30 * time.Second
	if in.TimeoutSeconds > 0 {
		timeout = time.Duration(in.TimeoutSeconds) * time.Second
	}
	if timeout > 120*time.Second {
		timeout = 120 * time.Second
	}
	if evalNoJail() {
		return runUnjailed(ctx, e.root, in.Command, timeout)
	}
	return runJailed(ctx, e.root, in.Command, timeout, e.network)
}

type sandboxLink struct {
	list []tools.Tool
}

func (sandboxLink) Name() string { return "tools" }

func (s sandboxLink) Tools() []tools.Tool { return s.list }

func caseTools(work string, network bool) []tools.Tool {
	return []tools.Tool{
		newEvalShell(work, network),
		&tools.ReadFile{Root: work, Sandbox: work},
		&tools.WriteFile{Root: work, Sandbox: work},
		&tools.EditFile{Root: work, Sandbox: work},
	}
}

// fileOnlyTools removes the shell by identity, independent of catalog order.
func fileOnlyTools(list []tools.Tool) []tools.Tool {
	out := make([]tools.Tool, 0, len(list))
	for _, t := range list {
		if !review.IsShellTool(t.Name()) {
			out = append(out, t)
		}
	}
	return out
}
