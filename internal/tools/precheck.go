package tools

import (
	"context"
	"strings"
	"time"
)

// MaxPrecheckBytes uses the same command limit as the interactive shell.
const MaxPrecheckBytes = maxCommandBytes

// ShellPrecheck runs a non-interactive sh command through the existing hard
// timeout path. No Jobs, stdin, output retention, or completion notification.
// Relative paths use the process working directory, as with an unbound Shell.
func ShellPrecheck(ctx context.Context, command string) bool {
	mode, err := localMachine()
	// A scheduled check has no approver. Re-read the same live machine gate
	// used by shell; an earlier approval cannot authorize a later ask/off mode.
	if len(command) > MaxPrecheckBytes || err != nil || mode != "allow" {
		return false
	}
	result, err := runCommand(ctx, command, 2*time.Second, 0, "", (&Shell{}).commandEnv(false))
	// runCommand owns this status line; with maxOut=0 no command output can
	// supply or spoof it. Nonzero exit and timeout are formatted, not Go errors.
	status, _, _ := strings.Cut(result, "\n")
	return err == nil && status == "exit 0"
}
