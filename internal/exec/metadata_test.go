package exec

import (
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
