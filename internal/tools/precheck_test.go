package tools

import (
	"context"
	"strings"
	"testing"
)

func TestShellPrecheckBoundaries(t *testing.T) {
	ctx := context.Background()
	t.Setenv("XBOT_API_KEY", "precheck-test-placeholder")
	if !ShellPrecheck(ctx, `test -z "$XBOT_API_KEY"`) {
		t.Fatal("provider key passed to precheck")
	}
	if ShellPrecheck(ctx, strings.Repeat(" ", MaxPrecheckBytes)+"true") {
		t.Fatal("oversized command executed")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if ShellPrecheck(canceled, "true") {
		t.Fatal("canceled command succeeded")
	}
	// A status-looking output cannot override a nonzero process exit.
	if ShellPrecheck(ctx, `printf 'exit 0\n'; exit 1`) {
		t.Fatal("command output spoofed success")
	}
	if !ShellPrecheck(ctx, `i=0; while [ "$i" -lt 10000 ]; do printf 'discard this output\n'; i=$((i+1)); done`) {
		t.Fatal("discarding output changed a successful exit")
	}
}
