package exec

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"xbot/internal/notable"
	"xbot/internal/provider"
)

func TestLongResultStoredWithoutTruncation(t *testing.T) {
	dir := t.TempDir()
	text := strings.Repeat("详细内容\n", 1500) + "TAIL_RESULT_99"
	loc := NewLocal(Limits{})
	j := &job{id: "sa_0123456789abcdef", dir: dir, parent: loc}
	j.finish(statusDone, EvDone, text)
	ev := <-loc.Events()
	raw, err := os.ReadFile(filepath.Join(dir, "result.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != text || len(ev.Text) > reportBytes || !strings.Contains(ev.Text, "result.txt") || !strings.Contains(string(raw), "TAIL_RESULT_99") {
		t.Fatal("long result lost")
	}
	meta, err := ReadMeta(dir)
	if err != nil || meta.Status != statusDone {
		t.Fatalf("meta %+v %v", meta, err)
	}
}

func TestResultWriteFailureDoesNotClaimSuccess(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "result.txt"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	// A directory at the output path makes the atomic rename fail on every platform.
	if err := os.Remove(filepath.Join(dir, "result.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "result.txt"), 0700); err != nil {
		t.Fatal(err)
	}
	loc := NewLocal(Limits{})
	j := &job{id: "sa_0123456789abcdef", dir: dir, parent: loc}
	j.finish(statusDone, EvDone, strings.Repeat("x", reportBytes+1))
	ev := <-loc.Events()
	if ev.Kind != EvFailed || !strings.Contains(ev.Text, "保存任务结果失败") {
		t.Fatalf("false success %+v", ev)
	}
	meta, err := ReadMeta(dir)
	if err != nil || meta.Status != statusFailed {
		t.Fatalf("meta %+v %v", meta, err)
	}
}

type blockedResultProvider struct{ calls int }

func (p *blockedResultProvider) Chat(_ context.Context, _ provider.Request, _ func(string)) (provider.Message, error) {
	p.calls++
	text := "BLOCKED 需要主人决定颜色"
	if p.calls > 1 {
		text = "完成42"
	}
	return provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent(text)}, nil
}
func TestBlockedStatePersistsUntilSteered(t *testing.T) {
	dir := t.TempDir()
	loc := NewLocal(Limits{Timeout: time.Second * 5})
	loc.Prov = &blockedResultProvider{}
	id := "sa_0123456789abcdef"
	if _, err := loc.Start(context.Background(), StartRequest{ID: id, Mode: ModeBrief, Task: "ask", Dir: dir}); err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-loc.Events():
		if ev.Kind != EvBlocked {
			t.Fatalf("event %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("no blocked event")
	}
	tasks, err := loc.Tasks()
	if err != nil || len(tasks) != 1 || !tasks[0].Blocked {
		t.Fatalf("tasks %+v %v", tasks, err)
	}
	m, err := ReadMeta(dir)
	if err != nil || !m.Blocked {
		t.Fatalf("meta %+v %v", m, err)
	}
	if err := loc.Control(context.Background(), id, Control{Kind: KindSteer, Text: "选蓝色"}); err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-loc.Events():
		if ev.Kind != EvDone {
			t.Fatalf("terminal %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("no result after steer")
	}
	m, err = ReadMeta(dir)
	if err != nil || m.Blocked {
		t.Fatalf("blocked after finish %+v %v", m, err)
	}
}

func TestLongNotableReportKeepsFullResultPath(t *testing.T) {
	dir := t.TempDir()
	loc := NewLocal(Limits{})
	j := &job{id: "sa_0123456789abcdef", dir: dir, parent: loc}
	ctx := notable.With(context.Background(), "/work", &j.notable)
	notable.Wrote(ctx, "/outside/"+strings.Repeat("a", 100)+".txt")
	notable.Wrote(ctx, "/outside/"+strings.Repeat("b", 100)+".txt")
	j.finish(statusDone, EvDone, strings.Repeat("body", 2000)+"TAIL_447")
	ev := <-loc.Events()
	if len(ev.Text) > reportBytes || !strings.Contains(ev.Text, filepath.Join(dir, "result.txt")) || !strings.Contains(ev.Text, "值得留意：") {
		t.Fatalf("lost long-result entry: %s", ev.Text)
	}
}
