//go:build smoke

package smoke

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	runner "xbot/internal/exec"
)

func TestBusyFindsLongTaskIdentifier(t *testing.T) {
	stub := StartStub(t)
	srv := Start(t, stub)
	srv.CreateBot("longtask", "long task observation")
	id := "sa_0123456789abcdef"
	task := strings.Repeat("前缀说明", 60) + " BUSY_s3_A"
	dir := filepath.Join(srv.Root, "bots", "longtask", "agents", id)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(runner.Meta{ID: id, Task: task, Status: "running"})
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(srv.Agents("longtask")[0].Task, "BUSY_s3_A") {
		t.Fatal("fixture identifier was not beyond the UI preview")
	}
	b := &busyRun{t: t, srv: srv}
	agent, ok := b.agent("longtask", "BUSY_s3_A")
	if !ok || agent.ID != id || agent.Task != task {
		t.Fatalf("full accepted task was not found: %+v, %v", agent, ok)
	}
}

func TestBusyBatchLatencyStartsAtLastArrival(t *testing.T) {
	sent := time.Unix(100, 0)
	raw := 12619.0
	if got := busyBatchDelay(&raw, sent, sent.Add(8*time.Second)); got == nil || *got != 4619 {
		t.Fatalf("batch delay = %v", got)
	}
	raw = 300
	if got := busyBatchDelay(&raw, sent, sent.Add(8*time.Second)); got == nil || *got != 0 {
		t.Fatalf("already dispatched task has a batch wait: %v", got)
	}
	if busyBatchDelay(nil, sent, sent) != nil {
		t.Fatal("missing task became a zero-latency success")
	}
}

func TestBusyBatchConfirmationCoversEveryRequestAndCount(t *testing.T) {
	tasks := []string{"BUSY_s4_B1", "BUSY_s4_B2", "BUSY_s4_B3", "BUSY_s4_B4", "BUSY_s4_B5"}
	for _, text := range []string{
		"五项调研 B1到B5 都已受理，三项排队、两项在跑。",
		"B1、B2、B3、B4、B5 都已派出，原 A 照常进行。",
	} {
		if !busyBatchConfirmation(text, tasks) {
			t.Fatalf("complete confirmation rejected: %s", text)
		}
	}
	for _, text := range []string{
		"四项调研已分开按 BUSY_s4_B1 到 B5 派出：B4、B5 已经开始，B1、B2、B3 排在队里。五个任务的验收值到了我逐个转给你。",
		"五项调研 B1、B2、B3、B4 已派出。",
		"已派出，结果回来后转告。",
		"SILENT",
		`{"tool_calls":[{"B1":"B2 B3 B4 B5"}]}`,
	} {
		if busyBatchConfirmation(text, tasks) {
			t.Fatalf("wrong or incomplete confirmation accepted: %s", text)
		}
	}
}
