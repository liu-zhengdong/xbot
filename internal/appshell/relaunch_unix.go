//go:build unix

package appshell

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/egoist/mygo"
)

// Relaunch quits the app and starts the binary at exe once this process has
// exited, so an update installed in its place runs.
//
// It does not use MyGo's App.Relaunch: that starts the new process before the
// old one is gone, and the new one then loses the race for the data root's
// app.lock and exits with "xbot app is already running". The launch waits for
// this pid to disappear first, in a session of its own so ending this process
// does not take it with it.
func Relaunch(exe string, args, env []string) error {
	if exe == "" {
		return errors.New("appshell: no executable to relaunch")
	}
	cmd := exec.Command("/bin/sh", "-c", waitThenLaunch(os.Getpid(), exe, args))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	// env carries what the running process holds rather than what it started
	// with: provider keys are taken out of the environment at startup
	// (config.HoldKeys), so a child given os.Environ() has none of them.
	cmd.Env = env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	if err := cmd.Start(); err != nil {
		return err
	}
	_ = cmd.Process.Release()
	go mygo.App.Quit()
	return nil
}

// waitThenLaunch is the shell script Relaunch runs: wait for pid to go, then
// start exe with its arguments.
func waitThenLaunch(pid int, exe string, args []string) string {
	quoted := []string{shellQuote(exe)}
	for _, a := range args {
		quoted = append(quoted, shellQuote(a))
	}
	return fmt.Sprintf("while kill -0 %d 2>/dev/null; do sleep 0.2; done; exec %s", pid, strings.Join(quoted, " "))
}

// shellQuote quotes one argument for /bin/sh.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
