package tools

import (
	"context"
	"testing"
)

func TestCallContextPreparedLifetime(t *testing.T) {
	type markerKey struct{}
	for _, cancelCall := range []bool{false, true} {
		name := "completed"
		if cancelCall {
			name = "cancelled"
		}
		t.Run(name, func(t *testing.T) {
			j := NewJobs(t.TempDir())
			defer j.Shutdown()
			entered := make(chan context.Context, 1)
			release := make(chan struct{})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			res := j.RunCall(ctx, CallRequest{
				Name:       "context-test",
				Background: true,
				Prepare: func(ctx context.Context, id string) (context.Context, func()) {
					if CallID(ctx) != id {
						t.Errorf("Prepare CallID=%q want=%q", CallID(ctx), id)
					}
					return context.WithValue(ctx, markerKey{}, "final prepared context"), func() {}
				},
				Run: func(ctx context.Context) CallResult {
					entered <- ctx
					select {
					case <-release:
						return CallResult{Text: "completed"}
					case <-ctx.Done():
						return CallResult{Err: ctx.Err()}
					}
				},
			})
			if res.Err != nil || res.Job == nil {
				t.Fatalf("RunCall: %+v", res)
			}
			id := res.Job.ID
			// Keep the existing live completion channel, including final cleanup.
			lv := j.get(id)
			defer lv.cancel()
			runCtx := <-entered
			got, ok := j.CallContext(id)
			if !ok || got != runCtx || got.Value(markerKey{}) != "final prepared context" || CallID(got) != id {
				t.Fatalf("CallContext did not return final prepared execution context: ctx=%v ok=%v", got, ok)
			}
			// The consumer can call back into Jobs after lookup: neither table nor
			// live locks may remain held across use of the returned context.
			status, err := j.Status(CallID(got))
			if err != nil || status.Status != "running" || !status.Local {
				t.Fatalf("Status after lookup: %+v err=%v", status, err)
			}
			// A separate table sees the real running metadata, but cannot borrow
			// this process's original call context, before or after Load.
			history := NewJobs(j.Directory())
			if ctx, ok := history.CallContext(id); ok || ctx != nil || len(history.jobs) != 0 {
				t.Fatal("CallContext loaded metadata on demand")
			}
			if err := history.Load(); err != nil {
				t.Fatal(err)
			}
			foreign, err := history.Status(id)
			if err != nil || foreign.Local {
				t.Fatalf("loaded metadata: %+v err=%v", foreign, err)
			}
			if ctx, ok := history.CallContext(id); ok || ctx != nil {
				t.Fatal("loaded metadata exposed an original call context")
			}
			wantStatus := "done"
			if cancelCall {
				wantStatus = "cancelled"
				if _, err := j.Cancel(id); err != nil {
					t.Fatal(err)
				}
			} else {
				close(release)
			}
			<-lv.done
			if ctx, ok := j.CallContext(id); ok || ctx != nil {
				t.Fatal("settled call still exposes its context")
			}
			lv.mu.Lock()
			retained := lv.callContext
			lv.mu.Unlock()
			if retained != nil {
				t.Fatal("settled live record retained its context")
			}
			status, err = j.Status(id)
			if err != nil || status.Status != wantStatus {
				t.Fatalf("settled status: %+v err=%v want=%s", status, err, wantStatus)
			}
		})
	}
}
