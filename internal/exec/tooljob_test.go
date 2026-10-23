package exec

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"xbot/internal/provider"
	"xbot/internal/tools"
)

type runnerFetch struct {
	release chan struct{}
	calls   atomic.Int32
}

func (f *runnerFetch) Fetch(ctx context.Context, _ string) (tools.Page, error) {
	f.calls.Add(1)
	select {
	case <-f.release:
		return tools.Page{Text: "RUNNER_FINAL_831"}, nil
	case <-ctx.Done():
		return tools.Page{}, ctx.Err()
	}
}

func TestRunnerToolJobsOutliveParentAndUseExistingControl(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(map[bool]string{false: "result", true: "cancel"}[cancel], func(t *testing.T) {
			fetch := &runnerFetch{release: make(chan struct{})}
			jobDir := filepath.Join(t.TempDir(), "shell-jobs")
			jobs := tools.NewJobs(jobDir)
			if err := jobs.Load(); err != nil {
				t.Fatal(err)
			}
			defer jobs.Shutdown()
			loc := NewLocal(Limits{MaxConcurrent: 3, MaxSteps: 3, Timeout: time.Second * 10})
			loc.Prov = &effectsModel{calls: []provider.ToolCall{{ID: "lookup", Type: "function", Function: provider.FunctionCall{Name: "web_fetch", Arguments: `{"url":"https://example.org/runner"}`}}}}
			loc.ToolSet = func(string, string) []tools.Tool { return []tools.Tool{&tools.WebFetch{Fetcher: fetch}} }
			loc.JobsFor = func(req StartRequest) (*tools.Jobs, error) { return jobs, nil }
			srv := httptest.NewServer((&Server{Local: loc, Token: "test-runner"}).Handler())
			defer srv.Close()
			remote := Dial(srv.URL, "test-runner")
			defer remote.Close()
			_, err := remote.Start(context.Background(), StartRequest{ID: "sa_0123456789abcdef", Task: "one fetch", Mode: ModeBrief, Dir: t.TempDir(), Workspace: t.TempDir(), JobDir: jobDir})
			if err != nil {
				t.Fatal(err)
			}
			end := time.NewTimer(4 * time.Second)
			defer end.Stop()
			for {
				select {
				case ev := <-remote.Events():
					if ev.Kind == EvDone {
						goto parentDone
					}
				case <-end.C:
					t.Fatal("parent stayed blocked on slow tool")
				}
			}
		parentDone:
			list := jobs.List()
			if len(list) != 1 || list[0].Status != "running" {
				t.Fatalf("parent killed detached call: %+v", list)
			}
			job := list[0]
			if cancel {
				if err := remote.CancelToolJob(context.Background(), jobDir, job.ID); err != nil {
					t.Fatal(err)
				}
			} else {
				close(fetch.release)
			}
			for {
				select {
				case ev := <-remote.Events():
					if ev.Kind != tools.JobEventType {
						continue
					}
					if ev.ID != "shelljob_"+job.ID {
						t.Fatalf("unstable event id: %+v", ev)
					}
					if !cancel && !strings.Contains(ev.Text, "RUNNER_FINAL_831") {
						t.Fatalf("missing completion: %+v", ev)
					}
					if fetch.calls.Load() != 1 {
						t.Fatalf("ran %d times", fetch.calls.Load())
					}
					if err := jobs.Deliver(job.ID, func(tools.Job, tools.CallResult) error { return nil }); err != nil {
						t.Fatal(err)
					}
					st, _ := jobs.Status(job.ID)
					if cancel && st.Status != "cancelled" {
						t.Fatalf("cancellation not real: %+v", st)
					}
					return
				case <-end.C:
					t.Fatal("no ordinary tool completion over runner transport")
				}
			}
		})
	}
}
