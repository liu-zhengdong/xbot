package eval

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestMemoryRecallRuntimeEvalBridgeQualifier(t *testing.T) {
	const binary = "/source/bin/xbot"
	const root = "/source/root"
	env := []string{"XBOT_BIN=" + binary, "XBOT_ROOT=" + root}
	for _, tc := range []struct {
		name, command string
		want          bool
	}{
		{"literal_bin", `"$XBOT_BIN" memory-recall --self --query 'launch code' --scope bot`, true},
		{"source_bin", `'/source/bin/xbot' memory-recall --self --json`, true},
		{"same_root", `"$XBOT_BIN" --root /source/root memory-recall --self`, true},
		{"same_root_equals", `"$XBOT_BIN" --root=/source/root memory-recall --self`, true},
		{"other_root", `"$XBOT_BIN" --root /other memory-recall --self`, false},
		{"unrelated_shell", `printf 'memory-recall --self'`, false},
		{"other_bin", `/other/xbot memory-recall --self`, false},
		{"other_noun", `"$XBOT_BIN" profile show --self`, false},
		{"missing_self", `"$XBOT_BIN" memory-recall --query tea`, false},
		{"help", `"$XBOT_BIN" memory-recall --self --help`, false},
		{"query_file", `"$XBOT_BIN" memory-recall --self --query-file /host/input`, false},
		{"query_stdin", `"$XBOT_BIN" memory-recall --self --query-file -`, false},
		{"bad_parser", `"$XBOT_BIN" memory-recall --self --unknown`, false},
		{"compose", `"$XBOT_BIN" memory-recall --self; printf fake`, false},
		{"pipe", `"$XBOT_BIN" memory-recall --self | head`, false},
		{"substitution", `"$XBOT_BIN" memory-recall --self --query "$(id)"`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gotBinary, argv, ok := evalMemoryRecallCommand(tc.command, env)
			if ok != tc.want {
				t.Fatalf("qualified = %v, want %v", ok, tc.want)
			}
			if ok && (gotBinary != binary || len(argv) == 0) {
				t.Fatalf("wrong executable/argv: %q %q", gotBinary, argv)
			}
		})
	}
	_, argv, ok := evalMemoryRecallCommand(`"$XBOT_BIN" memory-recall --self --query 'a; $(literal)'`, env)
	if !ok || !reflect.DeepEqual(argv, []string{"memory-recall", "--self", "--query", "a; $(literal)"}) {
		t.Fatalf("literal argv changed: %q, qualified %v", argv, ok)
	}
	if newEvalShell(t.TempDir(), false).evalEnv != nil {
		t.Fatal("ordinary eval shell unexpectedly has a source runtime")
	}
}

func TestMemoryRecallRuntimeEvalBridgeActualBinary(t *testing.T) {
	// The source must be the actual CLI, not this package's test executable.
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	temp := t.TempDir()
	binary := filepath.Join(temp, "xbot")
	buildCtx, cancelBuild := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancelBuild()
	build := exec.CommandContext(buildCtx, "go", "build", "-o", binary, "./cmd/xbot")
	build.Dir = repo
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build actual CLI: %v\n%s", err, out)
	}
	fixture, err := os.ReadFile(filepath.Join(repo, "evals/proactivity/memory-recall.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	suite := filepath.Join(temp, "suite")
	if err := os.Mkdir(suite, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(suite, "memory-recall.yaml"), fixture, 0600); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(temp, "report")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "--root", filepath.Join(temp, "root"), "eval", "fake/scripted", "--suite", suite, "--runs", "1", "--out", outDir)
	// This existing eval-only switch permits the unit test on macOS. It does
	// not establish Linux jail execution; the qualified CLI executes directly.
	cmd.Env = append(os.Environ(), "XBOT_EVAL_NO_JAIL=1", "XBOT_CALL_ID=stale-process-id")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("actual CLI closed-case eval: %v\n%s", err, out)
	}
	raw, err := os.ReadFile(filepath.Join(outDir, "fake__scripted.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report Report
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatal(err)
	}
	if !report.Passed || len(report.Cases) != 1 || !report.Cases[0].Pass || len(report.Cases[0].Runs) != 1 {
		t.Fatalf("closed-case report did not pass: %s", raw)
	}
	run := report.Cases[0].Runs[0]
	if len(run.Tools) != 1 || run.Tools[0].Name != "sh" || !strings.Contains(run.Tools[0].Args, "memory-recall") {
		t.Fatalf("unexpected actual tool calls: %+v", run.Tools)
	}
	transcript, err := os.ReadFile(run.TranscriptFile)
	if err != nil {
		t.Fatal(err)
	}
	body := string(transcript)
	toolStart := strings.Index(body, " tool\n")
	if toolStart < 0 {
		t.Fatalf("missing actual paired tool result:\n%s", body)
	}
	toolBody := body[toolStart:]
	if end := strings.Index(toolBody, "\n#"); end >= 0 {
		toolBody = toolBody[:end]
	}
	for _, fact := range []string{"exit 0", "violet-orbit-73", "bot:projects/x.md:1"} {
		if !strings.Contains(toolBody, fact) {
			t.Fatalf("actual source tool result missing %q:\n%s", fact, toolBody)
		}
	}
	// Use the same actual binary to verify the original process result adapter
	// preserves CLI failure as a nonzero exit, without inventing a source call.
	failure := exec.CommandContext(ctx, binary, "memory-recall", "--self")
	failure.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + temp, "TMPDIR=" + temp, "LANG=C.UTF-8"}
	out, err := evalCommandResult(ctx, ctx, failure, 30*time.Second)
	if err != nil || !strings.HasPrefix(out, "exit 1\n") || !strings.Contains(out, "XBOT_ROOT") {
		t.Fatalf("CLI failure became success: %q, %v", out, err)
	}
}
