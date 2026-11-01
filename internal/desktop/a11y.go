package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// nativeDriver keeps refs and delivery semantics in the official CLI, not a
// second in-process ref cache. The embedded idle probe preserves old behavior:
// when activity cannot be determined, actions proceed.
type nativeDriver struct{ commandDriver }

func defaultDriver() Driver {
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		return nativeDriver{}
	}
	return commandDriver{}
}

func (nativeDriver) Screenshot(ctx context.Context, path string) error {
	_, err := agentDesktop(ctx, "screenshot", path)
	return err
}

func (nativeDriver) Execute(ctx context.Context, a Action) (string, error) {
	var args []string
	switch a.Name {
	case "snapshot":
		if strings.TrimSpace(a.App) == "" {
			return "", errors.New("desktop: snapshot needs app")
		}
		args = []string{"snapshot", "--app", a.App, "--compact"}
		if a.Skeleton {
			args = append(args, "--skeleton")
		}
		if a.Ref != "" {
			args = append(args, "--root", a.Ref)
		}
		if a.Surface != "" {
			args = append(args, "--surface", a.Surface)
		}
	case "click", "type", "set_value", "scroll":
		if !qualifiedRef.MatchString(a.Ref) {
			return "", errors.New("desktop: use a qualified ref from desktop_snapshot; Mac/Windows do not guess coordinates")
		}
		switch a.Name {
		case "click":
			args = []string{"click", a.Ref}
		case "type", "set_value":
			if len(a.Text) > 2000 || (a.Name == "type" && strings.TrimSpace(a.Text) == "") {
				return "", errors.New("desktop: text must be nonempty and at most 2000 bytes (set_value may clear)")
			}
			command := "type"
			if a.Name == "set_value" {
				command = "set-value"
			}
			args = []string{command, a.Ref, "--", a.Text}
			// Windows type is SendInput and requires explicit headed permission.
			// This is platform selection, never a fallback after failed delivery.
			if runtime.GOOS == "windows" && a.Name == "type" {
				args = append([]string{"--headed"}, args...)
			}
		case "scroll":
			if a.Dy == 0 || a.Dy > 20 || a.Dy < -20 {
				return "", errors.New("desktop: scroll dy must be -20..-1 or 1..20")
			}
			direction, n := "up", a.Dy
			if n < 0 {
				direction, n = "down", -n
			}
			args = []string{"scroll", a.Ref, "--direction", direction, "--amount", strconv.Itoa(n)}
		}
	case "key":
		if !keyPattern.MatchString(a.Key) {
			return "", errors.New("desktop: bad key")
		}
		if strings.TrimSpace(a.App) == "" {
			return "", errors.New("desktop: key needs app on Mac/Windows")
		}
		args = []string{"press", a.Key, "--app", a.App}
	default:
		return "", fmt.Errorf("desktop: unsupported action %q", a.Name)
	}
	return agentDesktop(ctx, args...)
}

// CLIError retains the upstream error envelope, including code, disposition,
// and post_state. A nonzero exit or ok:false can never become a success.
type CLIError struct{ Envelope string }

func (e *CLIError) Error() string { return e.Envelope }

const maxCLIOutput = 1 << 20

type cappedOutput struct {
	bytes.Buffer
	overflow bool
}

func (b *cappedOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > maxCLIOutput {
		b.overflow = true
		return 0, errors.New("desktop: CLI output exceeds 1 MiB")
	}
	return b.Buffer.Write(p)
}

func agentDesktop(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	bin := os.Getenv("XBOT_AGENT_DESKTOP")
	if bin == "" {
		bin = "agent-desktop"
	}
	if runtime.GOOS == "windows" {
		path, err := exec.LookPath(bin)
		if err != nil {
			return "", errors.New("desktop: agent-desktop is not installed; install it or set XBOT_AGENT_DESKTOP to its native .exe")
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".cmd" || ext == ".bat" {
			path, err = windowsNativeCLI(path, runtime.GOARCH)
			if err != nil {
				return "", err
			}
		}
		bin = path
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	var stdout, stderr cappedOutput
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if stdout.overflow || stderr.overflow {
		return "", errors.New("desktop: CLI output exceeds 1 MiB; delivery unknown, observe before retry; use a skeleton or smaller snapshot region")
	}
	if ctx.Err() != nil {
		return "", fmt.Errorf("desktop: CLI interrupted; delivery unknown, snapshot before retry: %w", ctx.Err())
	}
	var envelope struct {
		OK *bool `json:"ok"`
	}
	raw := strings.TrimSpace(stdout.String())
	if json.Unmarshal([]byte(raw), &envelope) == nil && envelope.OK != nil {
		if !*envelope.OK {
			return "", &CLIError{Envelope: raw}
		}
		if err == nil {
			return raw, nil
		}
	}
	if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
		return "", errors.New("desktop: agent-desktop is not installed; run npm install -g agent-desktop, or set XBOT_AGENT_DESKTOP")
	}
	if err != nil {
		return "", fmt.Errorf("desktop: CLI failed; delivery unknown, snapshot before retry: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}
	return "", errors.New("desktop: invalid CLI envelope; delivery unknown, snapshot before retry")
}

// Status probes the same executable used for actions, without requesting OS
// permission or opening a system dialog. The UI derives human-readable copy.
func Status(ctx context.Context) (string, error) { return agentDesktop(ctx, "status") }
