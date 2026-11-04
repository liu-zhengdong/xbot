package tools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestDetachedShellCwdRequiresOptIn(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX cwd fixture")
	}
	for _, persist := range []bool{false, true} {
		t.Run(map[bool]string{false: "default", true: "foreign_context"}[persist], func(t *testing.T) {
			dir := t.TempDir()
			jobs := NewJobs(filepath.Join(dir, "jobs"))
			if err := jobs.Load(); err != nil {
				t.Fatal(err)
			}
			defer jobs.Close()
			shell := NewShell()
			shell.Dir = dir
			shell.BindJobs(jobs)
			shell.PersistBackgroundCwd = persist
			cwd := dir
			updates := 0
			shell.BindSession(func(context.Context) (string, string) { return dir, "" }, func(_ context.Context, path string) (string, string) { cwd = path; updates++; return path, "" })
			done := make(chan Job, 1)
			args := json.RawMessage(`{"command":"mkdir nested; cd nested","description":"test detached cwd"}`)
			result := jobs.RunCall(context.Background(), CallRequest{Name: shell.Name(), Dir: dir, Background: true, Run: func(ctx context.Context) CallResult {
				out, err := shell.Run(ctx, args)
				return CallResult{Text: out, Err: err}
			}, OnDone: func(job Job) { done <- job }})
			if result.Err != nil {
				t.Fatal(result.Err)
			}
			select {
			case job := <-done:
				if job.Status != "done" {
					t.Fatalf("job: %+v", job)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("detached shell did not finish")
			}
			if persist {
				want, err := filepath.EvalSymlinks(filepath.Join(dir, "nested"))
				if err != nil {
					t.Fatal(err)
				}
				if updates != 1 || cwd != want {
					t.Fatalf("cwd=%q updates=%d want=%q", cwd, updates, want)
				}
			} else if updates != 0 || cwd != dir {
				t.Fatalf("default detached call changed cwd: %q updates=%d", cwd, updates)
			}
		})
	}
}
