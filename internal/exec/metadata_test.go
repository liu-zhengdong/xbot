package exec

import (
	"io"
	"strings"
	"testing"
	"time"
)

func TestTaskMetadataKeepsStartAndCurrentStep(t *testing.T) {
	start := time.Now().UTC().Add(-time.Minute)
	j := &job{id: "sa_clock", task: "整理文件", dir: t.TempDir(), startedAt: start, currentStep: "读取目录", steps: 2}
	j.writeStatus(statusRunning, "")
	m, err := ReadMeta(j.dir)
	if err != nil {
		t.Fatal(err)
	}
	if !m.StartedAt.Equal(start) || m.CurrentStep != "读取目录" || !m.FinishedAt.IsZero() {
		t.Fatalf("running metadata: %+v", m)
	}
	j.writeStatus(statusDone, "整理好了")
	m, err = ReadMeta(j.dir)
	if err != nil {
		t.Fatal(err)
	}
	if !m.StartedAt.Equal(start) || m.FinishedAt.Before(start) || m.Steps != 2 {
		t.Fatalf("finished metadata: %+v", m)
	}
}

func TestRunnerActiveCountExcludesQueueAndDropsFinished(t *testing.T) {
	r := &Remote{events: make(chan Event, 2)}
	a, b := t.TempDir(), t.TempDir()
	if err := writeMeta(a, Meta{ID: "sa_a", Status: statusRunning}); err != nil {
		t.Fatal(err)
	}
	if err := writeMeta(b, Meta{ID: "sa_b", Status: statusQueued}); err != nil {
		t.Fatal(err)
	}
	r.active.Store("sa_a", a)
	r.active.Store("sa_b", b)
	if n := r.ActiveCount(); n != 1 {
		t.Fatalf("running and queued = %d, want 1", n)
	}
	if err := writeMeta(b, Meta{ID: "sa_b", Status: statusRunning}); err != nil {
		t.Fatal(err)
	}
	if n := r.ActiveCount(); n != 2 {
		t.Fatalf("promoted = %d, want 2", n)
	}
	r.read(io.NopCloser(strings.NewReader("data: {\"agent_id\":\"sa_a\",\"kind\":\"done\"}\n\n")))
	if n := r.ActiveCount(); n != 1 {
		t.Fatalf("after completion = %d, want 1", n)
	}
	if err := writeMeta(b, Meta{ID: "sa_b", Status: statusDone}); err != nil {
		t.Fatal(err)
	}
	if n := r.ActiveCount(); n != 0 {
		t.Fatalf("finished metadata = %d, want 0", n)
	}
	if _, ok := r.active.Load("sa_b"); ok {
		t.Fatal("finished history stayed in live requests")
	}

}
