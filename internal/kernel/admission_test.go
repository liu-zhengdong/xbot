package kernel

import (
	"context"
	"strings"
	"testing"
	"time"

	"xbot/internal/exec"
	"xbot/internal/link"
)

func TestEnsureExecQueuesAndReportsPosition(t *testing.T) {
	hold := make(chan struct{})
	p := &roleModel{hold: hold}
	k, b, _ := dispatchKernel(t, p)
	defer finishAdmissionJobs(t, k, hold)
	for i := 0; i < 11; i++ {
		result, err := k.dispatch(context.Background(), "research", "task "+string(rune('a'+i)))
		if err != nil {
			t.Fatalf("dispatch %d: %v", i, err)
		}
		want := "started "
		if i >= 3 {
			want = "queued "
		}
		if !strings.HasPrefix(result, want) {
			t.Fatalf("dispatch %d = %s", i, result)
		}
		if i >= 3 && !strings.Contains(result, "position "+string(rune('1'+i-3))) {
			t.Fatalf("position %d: %s", i, result)
		}
	}
	if _, err := k.dispatch(context.Background(), "research", "overflow"); err == nil {
		t.Fatal("queue over 8 accepted")
	}
	tasks, _ := k.ensureExec().Tasks()
	if len(tasks) != 11 {
		t.Fatalf("tasks %d", len(tasks))
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "status", time.Now())); err != nil {
		t.Fatal(err)
	}
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	var note string
	for _, e := range entries {
		if strings.Contains(e.Message.Content, "<background_tasks>") {
			note = e.Message.Content
		}
	}
	for _, task := range tasks {
		if !strings.Contains(note, task.ID+" ["+task.Status+"]") {
			t.Fatalf("missing %s %s in %s", task.ID, task.Status, note)
		}
	}
	if !strings.Contains(note, "排在第 8 个") {
		t.Fatal(note)
	}
}

func TestSameResourceDispatchSaysQueued(t *testing.T) {
	hold := make(chan struct{})
	k, _, _ := dispatchKernel(t, &roleModel{hold: hold})
	defer finishAdmissionJobs(t, k, hold)
	for i := 0; i < 2; i++ {
		result, err := k.dispatch(context.Background(), "dev", "same repo task")
		if err != nil {
			t.Fatal(err)
		}
		if i == 1 && (!strings.HasPrefix(result, "queued ") || !strings.Contains(result, "position 1")) {
			t.Fatal(result)
		}
	}
}

func TestInterjectedTaskGetsLiveBackgroundSnapshot(t *testing.T) {
	k, _, _ := dispatchKernel(t, &roleModel{})
	t0 := &turn{}
	k.openInterject(t0, &Sink{})
	defer k.closeInterject(t0, true)
	hold := make(chan struct{})
	defer finishAdmissionJobs(t, k, hold)
	// Local execution is held, while the owner's message enters the live turn.
	loc := exec.NewLocal(exec.Limits{MaxConcurrent: 1})
	loc.Prov = &roleModel{hold: hold}
	k.Exec = loc
	go k.forwardExec(loc.Events())
	for _, id := range []string{"sa_aaaaaaaa", "sa_bbbbbbbb"} {
		if _, err := loc.Start(context.Background(), exec.StartRequest{ID: id, Task: id, Mode: exec.ModeBrief, Dir: k.Bot.AgentDir(id)}); err != nil {
			t.Fatal(err)
		}
	}
	if !Interject(k.Bot, link.TerminalEvent("u1", "another task", time.Now()), &Sink{}) {
		t.Fatal("not interjected")
	}
	q := t0.takeInterjected()
	if len(q) != 3 || !q[0].Internal || !strings.Contains(q[0].Content, "sa_aaaaaaaa [running]") || !strings.Contains(q[0].Content, "sa_bbbbbbbb [queued]") || q[1].Content != "another task" {
		t.Fatalf("messages %+v", q)
	}
}

func finishAdmissionJobs(t *testing.T, k *Kernel, hold chan struct{}) {
	t.Helper()
	tasks, _ := k.execSnapshot().Tasks()
	want := map[string]bool{}
	for _, task := range tasks {
		want[task.ID] = true
	}
	close(hold)
	until := time.Now().Add(5 * time.Second)
	for time.Now().Before(until) {
		// A worker leaves Tasks before forwardExec finishes writing its event.
		// Wait for both owners of writes before TempDir cleanup, without sleeps
		// masquerading as a completion barrier.
		_ = k.Inbox.Reload()
		for _, ev := range k.Inbox.Pending() {
			if ev.Source == link.SourceSubagent && (ev.Type == exec.EvDone || ev.Type == exec.EvFailed) {
				delete(want, ev.Sender)
			}
		}
		if admissionCount(k) == 0 && len(want) == 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Errorf("subagent cleanup incomplete: active=%d pending reports=%d", admissionCount(k), len(want))
}

func admissionCount(k *Kernel) int { list, _ := k.execSnapshot().Tasks(); return len(list) }
