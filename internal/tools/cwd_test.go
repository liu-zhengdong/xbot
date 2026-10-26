package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func physical(t *testing.T, path string) string {
	t.Helper()
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(resolved)
}

func TestShellCwdIsFreshWithoutSession(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatal(err)
	}
	sh := NewShell()
	sh.Dir = dir
	ctx := context.Background()
	if _, err := sh.Run(ctx, json.RawMessage(`{"description":"Verify the scenario behavior","command":"cd sub"}`)); err != nil {
		t.Fatal(err)
	}
	out, err := sh.Run(ctx, json.RawMessage(`{"description":"Verify the scenario behavior","command":"pwd -P"}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "\ncwd:") {
		t.Fatalf("unbound shell printed a session cwd: %q", out)
	}
	if !strings.HasPrefix(out, "exit 0\n") {
		t.Fatalf("status: %q", out)
	}
	got := strings.TrimSpace(strings.TrimPrefix(out, "exit 0"))
	if got != physical(t, dir) {
		t.Fatalf("cd did not stay in the call: got %q want %q", got, physical(t, dir))
	}
	if strings.Contains(sh.Description(), "persists") {
		t.Fatal("description promises a persistent directory without a session")
	}
}

func TestShellCwdPersistsWhenBound(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatal(err)
	}
	wantSub := physical(t, sub)
	wantDir := physical(t, dir)
	cur := wantDir
	sh := NewShell()
	sh.Dir = dir
	sh.BindSession(
		func(context.Context) (string, string) { return cur, "" },
		func(_ context.Context, got string) (string, string) {
			if got == "" || !strings.HasPrefix(got, wantDir) {
				return cur, "working directory " + got + " is outside the workspace; staying in " + cur
			}
			cur = got
			return got, ""
		},
	)
	if !strings.Contains(sh.Description(), "persists") || !strings.Contains(sh.Description(), "cwd") {
		t.Fatalf("description: %s", sh.Description())
	}
	ctx := context.Background()
	out, err := sh.Run(ctx, json.RawMessage(`{"description":"Verify the scenario behavior","command":"cd sub"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "exit 0\n") || !strings.Contains(out, "\ncwd: "+wantSub) {
		t.Fatalf("cd result: %q", out)
	}
	out, err = sh.Run(ctx, json.RawMessage(`{"description":"Verify the scenario behavior","command":"pwd -P"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, wantSub) || strings.TrimSpace(strings.TrimPrefix(strings.Split(out, "\n")[0], "exit ")) != "0" {
		t.Fatalf("pwd: %q", out)
	}
	out, err = sh.Run(ctx, json.RawMessage(`{"description":"Verify the scenario behavior","command":"false"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "exit 1\n") || !strings.Contains(out, "\ncwd: "+wantSub) {
		t.Fatalf("false: %q", out)
	}
	out, err = sh.Run(ctx, json.RawMessage(`{"description":"Verify the scenario behavior","command":"exit 4"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "exit 4\n") || !strings.Contains(out, "\ncwd: "+wantSub) {
		t.Fatalf("exit 4: %q", out)
	}
	if cur != wantSub {
		t.Fatalf("session dir %q", cur)
	}
	out, err = sh.Run(ctx, json.RawMessage(`{"description":"Verify the scenario behavior","command":"cd /"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "outside the workspace") || !strings.Contains(out, "\ncwd: "+wantSub) {
		t.Fatalf("outside: %q", out)
	}
	if cur != wantSub {
		t.Fatalf("outside cd stuck: %q", cur)
	}
}

func TestShellBackgroundRecordsStartCwd(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatal(err)
	}
	wantSub := physical(t, sub)
	cur := wantSub
	sh := NewShell()
	sh.Dir = dir
	sh.Jobs = NewJobs(t.TempDir())
	sh.BindSession(
		func(context.Context) (string, string) { return cur, "" },
		func(_ context.Context, got string) (string, string) {
			cur = got
			return got, ""
		},
	)
	ctx := context.Background()
	out, err := sh.Run(ctx, json.RawMessage(`{"description":"Verify the scenario behavior","command":"cd /","run_in_background":true}`))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, job := range sh.Jobs.List() {
			_, _ = sh.Jobs.Cancel(job.ID)
		}
	})
	if !strings.HasPrefix(out, "background ") || !strings.Contains(out, "\ncwd: "+wantSub) {
		t.Fatalf("background: %q", out)
	}
	if strings.Count(out, "\ncwd:") != 1 {
		t.Fatalf("cwd lines: %q", out)
	}
	if cur != wantSub {
		t.Fatalf("background command changed the session dir to %q", cur)
	}
	var saw bool
	for _, job := range sh.Jobs.List() {
		if job.Cwd == wantSub {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("jobs %+v", sh.Jobs.List())
	}
}
