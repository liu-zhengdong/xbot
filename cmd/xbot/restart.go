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

	"xbot/internal/upgrade"
	"xbot/internal/version"
	"xbot/internal/web"
)

// drainCap bounds how long a graceful restart waits for in-flight turns.
const drainCap = 2 * time.Minute

// upgradeFirstDelay lets the service settle before its first release check.
const upgradeFirstDelay = 3 * time.Minute

// upgradeEvery is the periodic release check interval.
const upgradeEvery = 6 * time.Hour

// autoUpgradeLoop checks for a newer release now and then. When one applies it
// asks restart to re-exec the process; it never returns after that.
func autoUpgradeLoop(ctx context.Context, restart func(string)) {
	timer := time.NewTimer(upgradeFirstDelay)
	defer timer.Stop()
	ticker := time.NewTicker(upgradeEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		case <-ticker.C:
		}
		info, err := upgrade.Check(ctx, version.Version)
		if err != nil {
			fmt.Fprintln(os.Stderr, "xbot: upgrade check:", err)
			continue
		}
		if !info.Available {
			continue
		}
		if _, err := upgrade.Apply(ctx, info); err != nil {
			fmt.Fprintln(os.Stderr, "xbot: upgrade apply:", err)
			continue
		}
		restart("applied " + info.Latest)
		return
	}
}

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
