package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestMCP2aCallCallbacksAreSourceScoped(t *testing.T) {
	j := NewJobs(t.TempDir())
	var defaults atomic.Int32
	j.OnDone = func(Job) { defaults.Add(1) }
	j.Redact = func(s string) string { return strings.ReplaceAll(s, "global-value", "[global]") }
	j.Forms = func() []string { return []string{"global-value"} }
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer j.Shutdown()
	defer releaseOnce.Do(func() { close(release) })
	type running struct {
		id   string
		done chan Job
	}
	var calls []running
	for _, secret := range []string{"source-one-value", "source-two-value"} {
		entered := make(chan struct{})
		done := make(chan Job, 1)
		learned := new(atomic.Bool)
		redact := func(s string) string {
			if learned.Load() {
				s = strings.ReplaceAll(s, secret, "[source]")
			}
			return s
		}
		res := j.RunCall(context.Background(), CallRequest{Name: "sh", Command: secret, Background: true,
			Redact: redact, Forms: func() []string {
				if learned.Load() {
					return []string{secret}
				}
				return nil
			}, OnDone: func(job Job) { done <- job },
			Run: func(ctx context.Context) CallResult {
				lv := ctx.Value(callKey{}).(*live)
				learned.Store(true)
				_, err := lv.w.Write([]byte("prefix " + secret[:8]))
				close(entered)
				<-release
				if err == nil {
					_, err = lv.w.Write([]byte(secret[8:] + "\n"))
				}
				return CallResult{Text: secret, Err: err}
			}})
		if res.Err != nil || res.Job == nil {
			t.Fatalf("start: %+v", res)
		}
		<-entered
		lv := j.get(res.Job.ID)
		if lv.w.held() != 8 {
			t.Fatalf("source forms not captured: held=%d", lv.w.held())
		}
		body, err := os.ReadFile(res.Job.Output)
		if err != nil || string(body) != "prefix " {
			t.Fatalf("partial secret on disk: %q, %v", body, err)
		}
		calls = append(calls, running{res.Job.ID, done})
	}
	releaseOnce.Do(func() { close(release) })
	for _, call := range calls {
		job := <-call.done
		<-j.get(call.id).done
		if job.Result != "[source]" {
			t.Fatalf("source result: %+v", job)
		}
		body, err := os.ReadFile(job.Output)
		if err != nil || string(body) != "prefix [source]\n" {
			t.Fatalf("source log: %q, %v", body, err)
		}
		// The initial identity was captured before the dynamic callback learned it.
		if job.Command != "[source]" {
			t.Fatalf("terminal command not masked: %+v", job)
		}
	}
	if defaults.Load() != 0 {
		t.Fatalf("default OnDone also ran: %d", defaults.Load())
	}
	fallback := j.RunCall(context.Background(), CallRequest{Name: "lookup", Background: true, Command: "global-value", Run: func(context.Context) CallResult { return CallResult{Text: "global-value"} }})
	if fallback.Err != nil || fallback.Job == nil {
		t.Fatalf("fallback: %+v", fallback)
	}
	<-j.get(fallback.Job.ID).done
	if defaults.Load() != 1 {
		t.Fatalf("default OnDone count=%d", defaults.Load())
	}
	st, _ := j.Status(fallback.Job.ID)
	if st.Command != "[global]" || st.Result != "[global]" {
		t.Fatalf("missing defaults: %+v", st)
	}
}

func TestMCP2aCallTextAndErrors(t *testing.T) {
	const secret = "source-diagnostic-value"
	redact := func(s string) string { return strings.ReplaceAll(s, secret, "[source]") }
	original := &os.PathError{Op: "read", Path: secret, Err: os.ErrPermission}
	unchanged := errors.New("ordinary failure")
	for _, tc := range []struct {
		name string
		text string
		err  error
	}{
		{"text", secret, nil}, {"changed-error", "ignored", original}, {"unchanged-error", "ignored", unchanged},
	} {
		t.Run(tc.name, func(t *testing.T) {
			j := NewJobs(t.TempDir())
			res := j.RunCall(context.Background(), CallRequest{Name: "lookup", Command: secret, Redact: redact, Run: func(context.Context) CallResult { return CallResult{Text: tc.text, Err: tc.err} }})
			if strings.Contains(res.Text, secret) || (res.Err != nil && strings.Contains(res.Err.Error(), secret)) {
				t.Fatalf("raw diagnostic: %+v", res)
			}
			want := redact(tc.text)
			if tc.err != nil {
				want = "error: " + redact(tc.err.Error())
			}
			if res.Text != want {
				t.Fatalf("result text=%q want=%q", res.Text, want)
			}
			if tc.err == original {
				var pathErr *os.PathError
				if !errors.Is(res.Err, os.ErrPermission) || !errors.As(res.Err, &pathErr) || pathErr != original {
					t.Fatalf("lost error chain: %v", res.Err)
				}
			}
			if tc.err == unchanged && res.Err != unchanged {
				t.Fatalf("unchanged error wrapped: %v", res.Err)
			}
		})
	}
	// Registration failures also leave through the selected source callback.
	bad := filepath.Join(t.TempDir(), secret)
	if err := os.WriteFile(bad, nil, 0600); err != nil {
		t.Fatal(err)
	}
	res := NewJobs(bad).RunCall(context.Background(), CallRequest{Redact: redact, Run: func(context.Context) CallResult { t.Fatal("unregistered run"); return CallResult{} }})
	var pathErr *os.PathError
	if res.Err == nil || strings.Contains(res.Err.Error(), secret) || !errors.As(res.Err, &pathErr) {
		t.Fatalf("registration diagnostic: %v", res.Err)
	}
}

func TestMCP2aFinalizeAndWriteDiagnostics(t *testing.T) {
	const secret = "source-finalize-value"
	for _, failure := range []string{"", "finalize", "metadata", "output"} {
		t.Run(failure, func(t *testing.T) {
			j := NewJobs(filepath.Join(t.TempDir(), secret))
			defer j.Shutdown()
			done := make(chan Job, 1)
			var prepared, finalized, cleaned atomic.Int32
			res := j.RunCall(context.Background(), CallRequest{Name: "lookup", Command: secret, Background: true,
				Redact: func(s string) string { return strings.ReplaceAll(s, secret, "[source]") },
				Prepare: func(ctx context.Context, id string) (context.Context, func()) {
					prepared.Add(1)
					if CallID(ctx) != id {
						t.Error("Prepare lost call identity")
					}
					if job, err := j.Status(id); err != nil || job.Command != "[source]" {
						t.Errorf("initial command=%q err=%v", job.Command, err)
					}
					return ctx, func() { cleaned.Add(1) }
				},
				Run: func(ctx context.Context) CallResult {
					if failure == "output" {
						path := j.get(CallID(ctx)).snapshot().Output
						if err := os.Remove(path); err != nil {
							return CallResult{Err: err}
						}
						if err := os.Mkdir(path, 0700); err != nil {
							return CallResult{Err: err}
						}
					}
					return CallResult{Text: secret}
				},
				Finalize: func(job Job, result CallResult) (Job, error) {
					finalized.Add(1)
					if strings.Contains(result.Text, secret) || strings.Contains(job.Command, secret) {
						t.Error("raw input to Finalize")
					}
					if failure == "metadata" {
						// Replace this known metadata path with a directory to fail its atomic write.
						if err := os.Remove(j.metaPath(job.ID)); err != nil {
							return job, err
						}
						if err := os.Mkdir(j.metaPath(job.ID), 0700); err != nil {
							return job, err
						}
					}
					if failure == "finalize" {
						return job, errors.New(secret)
					}
					if failure == "" {
						job.Result, job.Command, job.Description = secret, secret, secret
					}
					return job, nil
				}, OnDone: func(job Job) { done <- job }})
			if res.Err != nil || res.Job == nil {
				t.Fatalf("start: %+v", res)
			}
			job := <-done
			lv := j.get(job.ID)
			<-lv.done
			if prepared.Load() != 1 || finalized.Load() != 1 || cleaned.Load() != 1 {
				t.Fatalf("lifetime prepare/finalize/cleanup=%d/%d/%d", prepared.Load(), finalized.Load(), cleaned.Load())
			}
			for _, text := range []string{job.Result, job.Command, job.Description, lv.result.Text} {
				if strings.Contains(text, secret) {
					t.Fatalf("raw metadata/diagnostic: %q", text)
				}
			}
			if lv.result.Err != nil && strings.Contains(lv.result.Err.Error(), secret) {
				t.Fatalf("raw result error: %v", lv.result.Err)
			}
			if failure != "" && job.Status != "failed" {
				t.Fatalf("silent %s failure: %+v", failure, job)
			}
			if failure != "metadata" {
				raw, err := os.ReadFile(j.metaPath(job.ID))
				// Output/Cwd are structural paths; inspect only the free-text fields.
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(raw), `"command":"`+secret) || strings.Contains(string(raw), `"result":"`+secret) || strings.Contains(string(raw), `"description":"`+secret) {
					t.Fatalf("raw serialized fields: %s", raw)
				}
			}
			if failure != "output" {
				body, err := os.ReadFile(job.Output)
				if err != nil || string(body) != "[source]" {
					t.Fatalf("processed output: %q, %v", body, err)
				}
			}
		})
	}
}

func TestMCP2aProcessUsesCallCallbacks(t *testing.T) {
	const secret = "source-process-value"
	for _, fail := range []bool{false, true} {
		j := NewJobs(t.TempDir())
		var processErr error
		var processText string
		res := j.RunCall(context.Background(), CallRequest{Name: "sh", Command: secret, Background: true,
			Redact: func(s string) string { return strings.ReplaceAll(s, secret, "[source]") }, Forms: func() []string { return []string{secret} },
			Run: func(ctx context.Context) CallResult {
				req := RunRequest{Command: "printf '" + secret + "'", Description: secret}
				if fail {
					req.Interpreter = interpreter{name: "sh", path: filepath.Join(j.dir(), secret)}
				}
				processText, processErr = j.Run(ctx, req)
				if strings.Contains(processText, secret) || (processErr != nil && strings.Contains(processErr.Error(), secret)) {
					t.Error("raw process adapter result")
				}
				return CallResult{Text: processText, Err: processErr}
			}})
		if res.Err != nil || res.Job == nil {
			t.Fatalf("start: %+v", res)
		}
		lv := j.get(res.Job.ID)
		<-lv.done
		j.Shutdown()
		job := lv.snapshot()
		if !fail && (job.Description != "[source]" || strings.Contains(job.Command, secret) || !strings.Contains(processText, "[source]")) {
			t.Fatalf("process fields/result: %+v, %q", job, processText)
		}
		if fail {
			var pathErr *os.PathError
			if processErr == nil || !errors.As(processErr, &pathErr) {
				t.Fatalf("lost process error type: %v", processErr)
			}
		}
	}
}

func TestMCP2aOutputUsesDynamicSourceAndCurrentDefault(t *testing.T) {
	j := NewJobs(t.TempDir())
	var sourceLearned, globalLearned atomic.Bool
	j.Redact = func(s string) string {
		if globalLearned.Load() {
			return strings.ReplaceAll(s, "late-global", "[global]")
		}
		return s
	}
	res := j.RunCall(context.Background(), CallRequest{Name: "lookup", Background: true,
		Redact: func(s string) string {
			if sourceLearned.Load() {
				return strings.ReplaceAll(s, "late-source", "[source]")
			}
			return s
		}, Run: func(context.Context) CallResult { return CallResult{Text: "late-source late-global"} }})
	if res.Err != nil || res.Job == nil {
		t.Fatalf("start: %+v", res)
	}
	<-j.get(res.Job.ID).done
	sourceLearned.Store(true)
	globalLearned.Store(true)
	out, err := j.Output(res.Job.ID, 0, 0)
	if err != nil || !strings.Contains(out, "[source] [global]") || !strings.Contains(out, "next_offset: 23") {
		t.Fatalf("dynamic output or offsets: %q, %v", out, err)
	}
}

func TestMCP2aAdmissionPauseResumeClose(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent", "jobs")
	j := NewJobs(dir)
	if !j.AdmissionOpen() {
		t.Fatal("new table closed")
	}
	if err := j.PauseAdmission(); err != nil {
		t.Fatal(err)
	}
	called := false
	req := CallRequest{Name: "lookup", Background: true, Prepare: func(ctx context.Context, _ string) (context.Context, func()) { called = true; return ctx, nil }, Run: func(context.Context) CallResult { called = true; return CallResult{} }}
	if res := j.RunCall(context.Background(), req); res.Err == nil || called {
		t.Fatalf("paused admitted: %+v", res)
	}
	if _, err := os.Stat(filepath.Dir(dir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pause created directory: %v", err)
	}
	if len(j.List()) != 0 || j.AdmissionOpen() {
		t.Fatal("pause registered a job")
	}
	j.Shutdown()
	if j.AdmissionOpen() {
		t.Fatal("Shutdown reopened pause")
	}
	if err := j.ResumeAdmission(); err != nil {
		t.Fatal(err)
	}

	release := make(chan struct{})
	entered := make(chan struct{})
	var once sync.Once
	defer j.Close()
	defer once.Do(func() { close(release) })
	res := j.RunCall(context.Background(), CallRequest{Name: "noncooperative", Background: true, Run: func(context.Context) CallResult {
		close(entered)
		<-release
		return CallResult{Text: "actually returned"}
	}})
	if res.Err != nil || res.Job == nil {
		t.Fatalf("resume failed: %+v", res)
	}
	<-entered
	if err := j.PauseAdmission(); err != nil {
		t.Fatal(err)
	}
	text, err := j.Cancel(res.Job.ID)
	if err != nil || text != "cancellation requested "+res.Job.ID {
		t.Fatalf("paused cancel: %q, %v", text, err)
	}
	st, err := j.Status(res.Job.ID)
	if err != nil || st.Status != "running" {
		t.Fatalf("premature terminal status: %+v, %v", st, err)
	}
	if _, err := j.Output(res.Job.ID, 0, 0); err != nil {
		t.Fatalf("paused output: %v", err)
	}
	joined := make(chan struct{})
	go func() { j.Shutdown(); close(joined) }()
	select {
	case <-joined:
		t.Fatal("joined noncooperative call before return")
	case <-time.After(20 * time.Millisecond):
	}
	once.Do(func() { close(release) })
	<-joined
	if err := j.ResumeAdmission(); err != nil || !j.AdmissionOpen() {
		t.Fatalf("same table did not resume: %v", err)
	}
	j.Close()
	if j.AdmissionOpen() || j.ResumeAdmission() == nil || j.PauseAdmission() == nil {
		t.Fatal("permanent close reopened")
	}
	before := len(j.List())
	called = false
	if res := j.RunCall(context.Background(), req); res.Err == nil || called || len(j.List()) != before {
		t.Fatalf("closed admitted: %+v", res)
	}
}
