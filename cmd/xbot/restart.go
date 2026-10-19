package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"xbot/internal/config"
	"xbot/internal/web"
)

// drainCap bounds how long a graceful restart waits for in-flight turns.
const drainCap = 2 * time.Minute

// reexecService stops new turns, waits for the running foreground ones, closes
// the listener, then replaces the process image with itself. cleanup closes any
// child process (for example the cloudflare tunnel) that must not outlive us.
// It returns only on error, so a successful restart never comes back.
//
// Background turns are deliberately not waited for: the re-exec leaves them
// behind and the new process repairs them with kernel.RecoverInterrupted.
func reexecService(ln net.Listener, ui *web.Server, exe string, argv, env []string, reason string, cleanup func()) error {
	if reason != "" {
		fmt.Fprintf(os.Stderr, "xbot: restarting: %s\n", reason)
	}
	ctx, cancel := context.WithTimeout(context.Background(), drainCap)
	defer cancel()
	if !ui.Drain(ctx) {
		fmt.Fprintln(os.Stderr, "xbot: restart: in-flight turns did not finish in time")
	}
	if ln != nil {
		_ = ln.Close()
	}
	if cleanup != nil {
		cleanup()
	}
	if exe == "" {
		p, err := os.Executable()
		if err != nil {
			return err
		}
		exe, err = filepath.EvalSymlinks(p)
		if err != nil {
			return err
		}
	}
	return syscall.Exec(exe, argv, config.HeldEnv(env))
}
