package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCallWaitCanOnlyShortenCommonSlice(t *testing.T) {
	s := NewShell()
	for _, tc := range []struct {
		seconds int
		want    time.Duration
	}{{0, DefaultToolWait}, {1, time.Second}, {10, DefaultToolWait}, {600, DefaultToolWait}} {
		args, _ := json.Marshal(map[string]any{"description": "Verify the scenario behavior", "command": "echo ok", "timeout_seconds": tc.seconds})
		got, bg := CallWait(s, args)
		if got != tc.want || bg {
			t.Fatalf("seconds=%d: wait=%s background=%v", tc.seconds, got, bg)
		}
	}
}

func TestCallJobShellReturnsRedactionHeldSuffix(t *testing.T) {
	j := NewJobs(t.TempDir())
	j.Forms = func() []string { return []string{"secret-unit-test-only"} }
	j.Redact = func(s string) string { return strings.ReplaceAll(s, "secret-unit-test-only", "[redacted]") }
	defer j.Shutdown()
	out, err := j.Run(context.Background(), RunRequest{Command: "printf yes"})
	if err != nil || !strings.HasSuffix(out, "yes") {
		t.Fatalf("final output=%q err=%v", out, err)
	}
}

func TestCallJobOutputOffsetsMatchProcessedFile(t *testing.T) {
	j := NewJobs(t.TempDir())
	defer j.Shutdown()
	r := j.RunCall(context.Background(), CallRequest{Name: "lookup", Background: true, Run: func(context.Context) CallResult { return CallResult{Text: "first\nsecond"} }})
	id := r.Job.ID
	end := time.Now().Add(time.Second)
	for {
		st, _ := j.Status(id)
		if st.Status != "running" {
			if st.Bytes != 12 {
				t.Fatalf("bytes=%d", st.Bytes)
			}
			break
		}
		if time.Now().After(end) {
			t.Fatal("not completed")
		}
		time.Sleep(time.Millisecond)
	}
	out, err := j.Output(id, 6, 0)
	if err != nil || !strings.Contains(out, "next_offset: 12") || !strings.HasSuffix(out, "second") {
		t.Fatalf("output=%q err=%v", out, err)
	}
}

func TestCallJobSingleWinner(t *testing.T) {
	for i := 0; i < 24; i++ {
		j := NewJobs(t.TempDir())
		release := make(chan struct{})
		signal := make(chan struct{})
		var run, reported atomic.Int32
		j.OnDone = func(Job) { reported.Add(1) }
		ctx, cancel := context.WithCancel(WithWaitSignal(context.Background(), signal))
		result := make(chan CallResult, 1)
		go func() {
			result <- j.RunCall(ctx, CallRequest{Name: "any-tool", Wait: time.Millisecond, Run: func(context.Context) CallResult { run.Add(1); <-release; return CallResult{Text: "ONE_RESULT"} }})
		}()
		switch i % 3 {
		case 0:
			close(release)
			close(signal)
		case 1:
			close(signal)
			close(release)
		case 2:
			cancel()
			close(release)
		}
		res := <-result
		j.Shutdown()
		cancel()
		if run.Load() > 1 {
			t.Fatalf("ran %d times", run.Load())
		}
		want := int32(0)
		if res.Job != nil {
			want = 1
		}
		if reported.Load() != want {
			t.Fatalf("receipt=%v err=%v reports=%d want=%d", res.Job != nil, res.Err, reported.Load(), want)
		}
		if res.Job == nil && res.Err == nil && res.Text != "ONE_RESULT" {
			t.Fatalf("lost result: %+v", res)
		}
	}
}

func TestCallJobNonCooperativeCancelDoesNotClaimStopped(t *testing.T) {
	j := NewJobs(t.TempDir())
	release := make(chan struct{})
	entered := make(chan struct{})
	defer j.Shutdown()
	res := j.RunCall(context.Background(), CallRequest{Name: "ignores-context", Background: true, Run: func(context.Context) CallResult { close(entered); <-release; return CallResult{Text: "real end"} }})
	<-entered
	id := res.Job.ID
	text, err := j.Cancel(id)
	if err != nil || text != "cancellation requested "+id {
		close(release)
		t.Fatalf("cancel: %s %v", text, err)
	}
	st, _ := j.Status(id)
	if st.Status != "running" {
		close(release)
		t.Fatalf("claimed stopped before execution ended: %+v", st)
	}
	closed := make(chan struct{})
	go func() { j.Shutdown(); close(closed) }()
	select {
	case <-closed:
		close(release)
		t.Fatal("shutdown claimed non-cooperative call had ended")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	<-closed
	st, _ = j.Status(id)
	if st.Status != "cancelled" {
		t.Fatalf("status=%s", st.Status)
	}
}

func TestCallJobRegistrationAndOutputFailures(t *testing.T) {
	base := t.TempDir()
	bad := filepath.Join(base, "regular")
	if err := os.WriteFile(bad, nil, 0600); err != nil {
		t.Fatal(err)
	}
	j := NewJobs(bad)
	ran := false
	res := j.RunCall(context.Background(), CallRequest{Name: "no-registration", Run: func(context.Context) CallResult { ran = true; return CallResult{} }})
	if res.Err == nil || ran {
		t.Fatal("unregistered call ran")
	}
	j = NewJobs(filepath.Join(base, "jobs"))
	report := make(chan Job, 1)
	j.OnDone = func(job Job) { report <- job }
	res = j.RunCall(context.Background(), CallRequest{Name: "output-failure", Background: true, Run: func(ctx context.Context) CallResult {
		id := CallID(ctx)
		path := filepath.Join(j.Dir, id+".log")
		_ = os.Remove(path)
		_ = os.Mkdir(path, 0700)
		return CallResult{Text: "actual result"}
	}})
	job := <-report
	j.Shutdown()
	if job.Status != "failed" || !strings.Contains(job.Result, "保存工具结果失败") {
		t.Fatalf("silent output failure: %+v receipt=%+v", job, res)
	}
}

func TestCallJobDetachmentOwnsItsContext(t *testing.T) {
	j := NewJobs(t.TempDir())
	defer j.Shutdown()
	ctx, cancel := context.WithCancel(context.Background())
	release := make(chan struct{})
	ran := make(chan bool, 1)
	res := j.RunCall(ctx, CallRequest{Name: "context-probe", Wait: time.Millisecond, Run: func(ctx context.Context) CallResult {
		<-release
		ran <- ctx.Err() == nil
		return CallResult{Text: "completed"}
	}})
	if res.Job == nil {
		close(release)
		cancel()
		t.Fatal("no background receipt")
	}
	cancel()
	close(release)
	if !<-ran {
		t.Fatal("foreground cancellation killed detached call")
	}
}

func BenchmarkCallJobQuick(b *testing.B) {
	j := NewJobs(b.TempDir())
	defer j.Shutdown()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res := j.RunCall(context.Background(), CallRequest{Name: "fast", Run: func(context.Context) CallResult { return CallResult{Text: "ok"} }})
		if res.Err != nil || res.Job != nil {
			b.Fatal(res)
		}
	}
}
