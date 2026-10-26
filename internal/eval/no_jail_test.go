package eval

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEvalNoJailRunsWithoutChroot(t *testing.T) {
	t.Setenv("XBOT_EVAL_NO_JAIL", "1")
	t.Setenv("EVAL_TEST_SECRET", "must-not-inherit")
	chrootPath = filepath.Join(t.TempDir(), "missing-chroot")
	resetJail()
	t.Cleanup(func() { chrootPath = ""; resetJail() })
	if err := jailFor(Options{}, []string{"proactivity"}); err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	out, err := newEvalShell(work, false).Run(context.Background(), []byte(`{"description":"Verify the scenario behavior","command":"printf 'done\\n' > /sandbox/result.txt; test -f \"$HOME/result.txt\" && test -z \"$EVAL_TEST_SECRET\" && cat /sandbox/result.txt"}`))
	if err != nil || out != "exit 0\ndone" {
		t.Fatalf("shell: %q, %v", out, err)
	}
	raw, err := os.ReadFile(filepath.Join(work, "result.txt"))
	if err != nil || string(raw) != "done\n" {
		t.Fatalf("file: %q, %v", raw, err)
	}
	// The same opt-out reaches the full case runner, not just direct shell calls.
	res := runCase(mustCase(t, "resume"), "normal", Script{
		{Tool: "sh", Args: `{"description":"Verify the scenario behavior","command":"printf 'DONE\\n' > /sandbox/out.txt"}`},
		{Text: "Done."},
	}, nil)
	if !res.Pass {
		t.Fatal(res.Reason)
	}
	// Default behavior still fails when the jail is unavailable.
	t.Setenv("XBOT_EVAL_NO_JAIL", "")
	if err := requireJail(); err == nil {
		t.Fatal("missing chroot passed without opt-out")
	}
}

func TestUnjailedShellKeepsExitAndTimeout(t *testing.T) {
	out, err := runUnjailed(context.Background(), t.TempDir(), "echo failed; exit 7", time.Second)
	if err != nil || out != "exit 7\nfailed" {
		t.Fatalf("exit: %q, %v", out, err)
	}
	out, err = runUnjailed(context.Background(), t.TempDir(), "exec sleep 1", 10*time.Millisecond)
	if err != nil || !strings.HasPrefix(out, "timed out after ") {
		t.Fatalf("timeout: %q, %v", out, err)
	}
}
