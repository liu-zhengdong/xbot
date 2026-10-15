package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"xbot/internal/fsutil"
	"xbot/internal/machine"
)

type state struct {
	PID     int    `json:"pid"`
	WS      string `json:"ws"`
	Profile string `json:"profile"`
	Bin     string `json:"bin,omitempty"`
}

func statePath(root string) string { return filepath.Join(root, "browser", "session.json") }

// Launch starts Chrome with its own profile and returns a page session.
// headless adds --headless=new. An existing session for this data root is stopped first.
func Launch(ctx context.Context, root, page string, headless bool) (*Session, error) {
	if err := allow(root, true); err != nil {
		return nil, err
	}
	if err := checkURL(page); err != nil {
		return nil, err
	}
	_ = Stop(root)
	bin, err := findChrome()
	if err != nil {
		return nil, err
	}
	profile := filepath.Join(root, "browser", "profile")
	if err := os.MkdirAll(profile, 0700); err != nil {
		return nil, err
	}
	_ = os.Remove(filepath.Join(profile, "DevToolsActivePort"))
	args := []string{
		"--remote-debugging-port=0",
		"--user-data-dir=" + profile,
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-extensions",
	}
	if headless {
		args = append(args, "--headless=new")
	}
	args = append(args, "about:blank")
	cmd, err := startBrowser(bin, args)
	if err != nil {
		return nil, err
	}
	port, err := waitPort(ctx, profile)
	if err != nil {
		_ = cmd.Process.Kill()
		return nil, err
	}
	ws, err := newPage(ctx, port)
	if err != nil {
		_ = cmd.Process.Kill()
		return nil, err
	}
	sess, err := Attach(ctx, ws)
	if err != nil {
		_ = cmd.Process.Kill()
		return nil, err
	}
	sess.stop = func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }
	raw, err := fsutil.Marshal(state{PID: cmd.Process.Pid, WS: ws, Profile: profile, Bin: bin})
	if err != nil {
		sess.Close()
		return nil, err
	}
	if err := fsutil.WriteAtomic(statePath(root), raw, 0600); err != nil {
		sess.Close()
		return nil, err
	}
	if err := sess.Navigate(ctx, page); err != nil {
		sess.Close()
		return nil, err
	}
	return sess, nil
}

// AttachSaved dials the Chrome left by the last Launch in this data root.
func AttachSaved(ctx context.Context, root string) (*Session, error) {
	if err := allow(root, true); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(statePath(root))
	if err != nil {
		return nil, errors.New("browser: not open")
	}
	var st state
	if err := json.Unmarshal(raw, &st); err != nil || st.WS == "" || !alive(st.PID) || !chromeOwned(st.PID, st.Profile) {
		return nil, errors.New("browser: not open")
	}
	return Attach(ctx, st.WS)
}

// Stop kills the saved Chrome when the pid still belongs to this profile.
// A live pid with a different command line is left alone, and the state file stays.
func Stop(root string) error {
	raw, err := os.ReadFile(statePath(root))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var st state
	if err := json.Unmarshal(raw, &st); err != nil {
		return os.Remove(statePath(root))
	}
	if st.PID > 0 && alive(st.PID) {
		if !chromeOwned(st.PID, st.Profile) {
			return fmt.Errorf("browser: pid %d is not the chrome for this profile", st.PID)
		}
		signalChrome(st.PID)
	}
	return os.Remove(statePath(root))
}

func startBrowser(bin string, args []string) (*exec.Cmd, error) {
	cmd := exec.Command(bin, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	cmd.SysProcAttr = browserProcAttr()
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

func browserProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
}

func signalChrome(pid int) {
	if pid <= 0 {
		return
	}
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil {
		p, findErr := os.FindProcess(pid)
		if findErr == nil {
			_ = p.Signal(syscall.SIGTERM)
		}
	}
}

// chromeOwned reports whether pid's command line contains this profile directory.
func chromeOwned(pid int, profile string) bool {
	profile = strings.TrimSpace(profile)
	if pid <= 0 || profile == "" {
		return false
	}
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil || len(raw) == 0 {
		return false
	}
	return strings.Contains(string(raw), profile)
}

func allow(root string, approved bool) error {
	mode, err := machine.Get(root, machine.Local)
	if err != nil {
		return err
	}
	if mode == machine.Off {
		return errors.New("machine local is off")
	}
	if mode == machine.Ask && !approved {
		return errors.New("machine local is ask")
	}
	return nil
}

func findChrome() (string, error) {
	if p := strings.TrimSpace(os.Getenv("XBOT_CHROME")); p != "" {
		return p, nil
	}
	for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "chrome"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", errors.New("browser: chrome not found; set XBOT_CHROME")
}

func waitPort(ctx context.Context, profile string) (int, error) {
	path := filepath.Join(profile, "DevToolsActivePort")
	deadline := time.Now().Add(8 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(path)
		if err == nil {
			line, _, _ := strings.Cut(string(raw), "\n")
			port, err := strconv.Atoi(strings.TrimSpace(line))
			if err == nil && port > 0 {
				return port, nil
			}
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	return 0, errors.New("browser: chrome did not open a debugging port")
}

func newPage(ctx context.Context, port int) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, fmt.Sprintf("http://127.0.0.1:%d/json/new?about:blank", port), nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var info struct {
		WS string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return "", err
	}
	if info.WS == "" {
		return "", errors.New("browser: chrome did not return a debugger url")
	}
	parsed, err := url.Parse(strings.Replace(info.WS, "ws://", "http://", 1))
	if err != nil || !loopbackHost(parsed.Hostname()) {
		return "", errors.New("browser: debugger url is not loopback")
	}
	return info.WS, nil
}

func alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}
