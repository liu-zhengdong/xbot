package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestShellTimeoutBounds(t *testing.T) {
	sh := NewShell()
	if got := sh.duration(0); got != 120*time.Second {
		t.Fatalf("default %s", got)
	}
	if got := sh.duration(10000); got != 600*time.Second {
		t.Fatalf("max %s", got)
	}
	if got := sh.duration(30); got != 30*time.Second {
		t.Fatalf("explicit %s", got)
	}
}

func TestShellBackgroundDescription(t *testing.T) {
	sh := NewShell()
	if strings.Contains(sh.Description(), "run_in_background") {
		t.Fatal("description promises background without a job directory")
	}
	sh.Jobs = NewJobs(t.TempDir())
	d := sh.Description()
	for _, s := range []string{"run_in_background", "shell_output", "shell_status", "shell_cancel", "tight loop", "not killed"} {
		if !strings.Contains(d, s) {
			t.Fatalf("description missing %q", s)
		}
	}
	raw, err := json.Marshal(sh.Parameters())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "run_in_background") || !strings.Contains(string(raw), "120") {
		t.Fatalf("schema %s", raw)
	}
}

func TestSkipAutoBackground(t *testing.T) {
	skip := []string{
		"sleep 30",
		"  sleep 1",
		"/bin/sleep 2",
		"FOO=1 sleep 3",
		"watch -n 1 date",
		"while true; do echo x; sleep 1; done",
		"until false; do sleep 1; done",
	}
	keep := []string{
		"echo hello",
		"echo hello; sleep 30",
		"go test ./...",
		"while true; do echo x; done",
	}
	for _, cmd := range skip {
		if !skipAutoBackground(cmd) {
			t.Fatalf("expected skip: %s", cmd)
		}
	}
	for _, cmd := range keep {
		if skipAutoBackground(cmd) {
			t.Fatalf("expected background: %s", cmd)
		}
	}
}

func TestJobWriterRedactsSplitSecret(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	w := newJobWriter(f, 4096, func(s string) string {
		return strings.ReplaceAll(s, "SUPERSECRET", "[secret]")
	})
	if _, err := w.Write([]byte("pre SUPERSE")); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("CRET post")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "SUPERSECRET") || !strings.Contains(string(body), "[secret]") {
		t.Fatalf("file %q", body)
	}
}

func TestJobWriterKeepsTail(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	w := newJobWriter(f, 48, nil)
	payload := strings.Repeat("A", 200) + "END"
	if _, err := w.Write([]byte(payload)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) > 48 || !strings.Contains(string(body), "END") || strings.HasPrefix(string(body), "AAAA") && strings.Count(string(body), "A") == 200 {
		t.Fatalf("tail %q (%d)", body, len(body))
	}
	if strings.Count(string(body), "A") >= 200 {
		t.Fatalf("kept the head: %q", body)
	}
}

func TestShellTimeoutMovesToBackground(t *testing.T) {
	dir := t.TempDir()
	jobs := NewJobs(dir)
	if err := jobs.Load(); err != nil {
		t.Fatal(err)
	}
	sh := &Shell{DefaultTimeout: 200 * time.Millisecond, MaxTimeout: 200 * time.Millisecond, MaxOutput: 1024, Jobs: jobs}
	marker := "xbotbg" + strings.ReplaceAll(t.Name(), "/", "")
	raw, err := json.Marshal(map[string]string{"command": "echo " + marker + "; sleep 30"})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	out, err := sh.Run(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("waited %s: %s", time.Since(start), out)
	}
	if !strings.Contains(out, "background sh_") || !strings.Contains(out, "not killed") {
		t.Fatalf("%s", out)
	}
	id := jobIDFrom(out)
	st, err := jobs.Status(id)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = jobs.Cancel(id) })
	if !strings.HasPrefix(st.Output, dir+string(os.PathSeparator)) {
		t.Fatalf("output path %s", st.Output)
	}
	if !processAlive(st.PID) {
		t.Fatal("timeout killed the process")
	}
	body, err := os.ReadFile(st.Output)
	if err != nil || !strings.Contains(string(body), marker) {
		t.Fatalf("output %q err %v", body, err)
	}
	if _, err := jobs.Cancel(id); err != nil {
		t.Fatal(err)
	}
	waitDead(t, st.PID)
}

func TestRunInBackgroundStartsImmediately(t *testing.T) {
	dir := t.TempDir()
	jobs := NewJobs(dir)
	if err := jobs.Load(); err != nil {
		t.Fatal(err)
	}
	sh := NewShell()
	sh.Jobs = jobs
	raw, err := json.Marshal(map[string]any{"command": "sleep 30", "run_in_background": true})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	out, err := sh.Run(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("blocked %s: %s", time.Since(start), out)
	}
	if !strings.Contains(out, "background sh_") || !strings.Contains(out, "started in background") {
		t.Fatalf("%s", out)
	}
	id := jobIDFrom(out)
	t.Cleanup(func() { _, _ = jobs.Cancel(id) })
	st, err := jobs.Status(id)
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != "running" || !processAlive(st.PID) {
		t.Fatalf("status %+v alive %v", st, processAlive(st.PID))
	}
}

func TestSleepIsNotAutoBackgrounded(t *testing.T) {
	bin := copySleep(t)
	dir := t.TempDir()
	jobs := NewJobs(dir)
	if err := jobs.Load(); err != nil {
		t.Fatal(err)
	}
	sh := &Shell{DefaultTimeout: 200 * time.Millisecond, MaxTimeout: 200 * time.Millisecond, Jobs: jobs}
	raw, err := json.Marshal(map[string]string{"command": bin + " 30"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := sh.Run(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "background") || !strings.Contains(out, "timed out") {
		t.Fatalf("%s", out)
	}
	waitGone(t, bin)
}

func TestPollingLoopIsNotAutoBackgrounded(t *testing.T) {
	bin := copySleep(t)
	dir := t.TempDir()
	jobs := NewJobs(dir)
	if err := jobs.Load(); err != nil {
		t.Fatal(err)
	}
	sh := &Shell{DefaultTimeout: 300 * time.Millisecond, MaxTimeout: 300 * time.Millisecond, Jobs: jobs}
	raw, err := json.Marshal(map[string]string{"command": "while true; do " + bin + " 1; done"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := sh.Run(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "background") || !strings.Contains(out, "timed out") {
		t.Fatalf("%s", out)
	}
	waitGone(t, bin)
}

func TestCancelKillsProcessGroup(t *testing.T) {
	dir := t.TempDir()
	jobs := NewJobs(dir)
	if err := jobs.Load(); err != nil {
		t.Fatal(err)
	}
	childPath := filepath.Join(dir, "childpid")
	cmd := fmt.Sprintf("sleep 30 & echo $! > '%s'; wait", childPath)
	out, err := jobs.Run(context.Background(), RunRequest{Command: cmd, Background: true, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	id := jobIDFrom(out)
	t.Cleanup(func() { _, _ = jobs.Cancel(id) })
	var child int
	deadline := time.Now().Add(2 * time.Second)
	for {
		body, err := os.ReadFile(childPath)
		if err == nil {
			fmt.Sscanf(strings.TrimSpace(string(body)), "%d", &child)
			if child > 0 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("child pid file: %v %q", err, body)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := jobs.Cancel(id); err != nil {
		t.Fatal(err)
	}
	waitDead(t, child)
}

func TestShellOutputOffsetAndTail(t *testing.T) {
	dir := t.TempDir()
	jobs := NewJobs(dir)
	if err := jobs.Load(); err != nil {
		t.Fatal(err)
	}
	out, err := jobs.Run(context.Background(), RunRequest{
		Command: "printf 'one\\ntwo\\nthree\\n'; sleep 30", Background: true, Timeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	id := jobIDFrom(out)
	t.Cleanup(func() { _, _ = jobs.Cancel(id) })
	deadline := time.Now().Add(2 * time.Second)
	var tail string
	for {
		tail, err = jobs.Output(id, 0, 1)
		if err == nil && strings.Contains(tail, "three") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("tail %q err %v", tail, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if strings.Contains(tail, "one") {
		t.Fatalf("tail included the head: %s", tail)
	}
	full, err := jobs.Output(id, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(full, "one") || !strings.Contains(full, "next_offset:") {
		t.Fatalf("%s", full)
	}
	again, err := jobs.Output(id, 4, 0)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(again, "one\n") || !strings.Contains(again, "two") {
		t.Fatalf("offset %s", again)
	}
}

func TestBackgroundFileRedactsSecret(t *testing.T) {
	dir := t.TempDir()
	jobs := NewJobs(dir)
	jobs.Redact = func(s string) string { return strings.ReplaceAll(s, "SUPERSECRET", "[secret]") }
	if err := jobs.Load(); err != nil {
		t.Fatal(err)
	}
	out, err := jobs.Run(context.Background(), RunRequest{
		Command: "printf SUPERSECRET; sleep 30", Background: true, Timeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	id := jobIDFrom(out)
	t.Cleanup(func() { _, _ = jobs.Cancel(id) })
	st, err := jobs.Status(id)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	var body []byte
	for {
		body, err = os.ReadFile(st.Output)
		if err == nil && strings.Contains(string(body), "[secret]") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("file %q err %v", body, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if strings.Contains(string(body), "SUPERSECRET") {
		t.Fatalf("secret landed in %s", st.Output)
	}
	meta, err := os.ReadFile(filepath.Join(dir, id+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(meta), "SUPERSECRET") {
		t.Fatalf("secret in meta %s", meta)
	}
}

func TestRestartMarksJobLostWithoutKilling(t *testing.T) {
	dir := t.TempDir()
	old := NewJobs(dir)
	if err := old.Load(); err != nil {
		t.Fatal(err)
	}
	out, err := old.Run(context.Background(), RunRequest{Command: "sleep 30", Background: true, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	id := jobIDFrom(out)
	st, err := old.Status(id)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = old.Cancel(id) })
	if !processAlive(st.PID) {
		t.Fatal("job was not running")
	}
	next := NewJobs(dir)
	if err := next.Load(); err != nil {
		t.Fatal(err)
	}
	if _, err := next.Status(id); err == nil {
		t.Fatal("reload adopted a still-live job")
	}
	if !processAlive(st.PID) {
		t.Fatal("reload killed the old pid")
	}
	raw, err := os.ReadFile(filepath.Join(dir, id+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var meta Job
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatal(err)
	}
	meta.ProcStart = 1
	rewritten, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".json"), rewritten, 0600); err != nil {
		t.Fatal(err)
	}
	lost := NewJobs(dir)
	if err := lost.Load(); err != nil {
		t.Fatal(err)
	}
	got, err := lost.Status(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "lost" {
		t.Fatalf("status %s", got.Status)
	}
	if !processAlive(st.PID) {
		t.Fatal("marking lost killed the old pid")
	}
	text, err := lost.Cancel(id)
	if err != nil {
		t.Fatal(err)
	}
	if text != "status lost" || !processAlive(st.PID) {
		t.Fatalf("cancel on lost: %s alive %v", text, processAlive(st.PID))
	}
}

func TestForegroundEchoStillReturnsExit(t *testing.T) {
	jobs := NewJobs(t.TempDir())
	if err := jobs.Load(); err != nil {
		t.Fatal(err)
	}
	sh := NewShell()
	sh.Jobs = jobs
	out, err := sh.Run(context.Background(), json.RawMessage(`{"command":"echo hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "exit 0") || !strings.Contains(out, "hello") || strings.Contains(out, "background") {
		t.Fatalf("%s", out)
	}
	if len(jobs.List()) != 0 {
		t.Fatalf("foreground job was kept: %+v", jobs.List())
	}
}

func BenchmarkShellTailRetain(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "out.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		b.Fatal(err)
	}
	w := newJobWriter(f, 4096, func(s string) string {
		return strings.ReplaceAll(s, "SUPERSECRET", "[secret]")
	})
	chunk := []byte("SUPERSECRET line of output\n")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := w.Write(chunk); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	_ = w.Close()
}

func TestShellOutputOffsetFollowsStream(t *testing.T) {
	dir := t.TempDir()
	jobs := NewJobs(dir)
	jobs.MaxBytes = 100
	if err := jobs.Load(); err != nil {
		t.Fatal(err)
	}
	cmd := `i=0; while [ $i -lt 8 ]; do printf 'A%02d-------\n' "$i"; i=$((i+1)); done; i=0; while [ $i -lt 8 ]; do printf 'B%02d-------\n' "$i"; i=$((i+1)); done`
	out, err := jobs.Run(context.Background(), RunRequest{Command: cmd, Background: true, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	id := jobIDFrom(out)
	t.Cleanup(func() { _, _ = jobs.Cancel(id) })
	deadline := time.Now().Add(3 * time.Second)
	for {
		st, err := jobs.Status(id)
		if err == nil && st.Status == "done" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("status %+v err %v", st, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	fromStart, err := jobs.Output(id, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fromStart, "A00") || !strings.Contains(fromStart, "[dropped]") || !strings.Contains(fromStart, "B07") {
		t.Fatalf("start after the tail slid: %s", fromStart)
	}
	got, err := jobs.Output(id, 88, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "B00") || strings.Contains(got, "A00") {
		t.Fatalf("stream offset 88: %s", got)
	}
}

func TestFinishedJobsArePruned(t *testing.T) {
	jobs := NewJobs(t.TempDir())
	if err := jobs.Load(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 40; i++ {
		if _, err := jobs.Run(context.Background(), RunRequest{Command: "echo hi", Timeout: time.Minute, Background: true}); err != nil {
			t.Fatalf("job %d: %v", i, err)
		}
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		running := 0
		for _, job := range jobs.List() {
			if job.Status == "running" {
				running++
			}
		}
		if running == 0 && len(jobs.List()) <= maxKeptJobs {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("tracked %d running %d", len(jobs.List()), running)
		}
		time.Sleep(20 * time.Millisecond)
	}
	ents, err := os.ReadDir(jobs.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) > maxKeptJobs*2 {
		t.Fatalf("files %d", len(ents))
	}
	stale := NewJobs(jobs.Dir)
	stale.Now = func() time.Time { return time.Now().Add(maxJobAge + time.Hour) }
	if err := stale.Load(); err != nil {
		t.Fatal(err)
	}
	// The kept jobs ended moments ago, so a clock eight days ahead drops them.
	if n := len(stale.List()); n != 0 {
		t.Fatalf("aged jobs kept: %d", n)
	}
}

func TestForegroundSecretSplitAcrossWrites(t *testing.T) {
	jobs := NewJobs(t.TempDir())
	jobs.Redact = func(s string) string { return strings.ReplaceAll(s, "sk-ABCDEFGHIJKLMNOPQRST", "[redacted]") }
	if err := jobs.Load(); err != nil {
		t.Fatal(err)
	}
	out, err := jobs.Run(context.Background(), RunRequest{
		Command: `printf 'key=sk-ABCDEFGHIJ'; sleep 0.2; printf 'KLMNOPQRST\n'`,
		Timeout: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "sk-ABCDEFGHIJKLMNOPQRST") || strings.Contains(out, "ABCDEFGHIJ") {
		t.Fatalf("secret leaked: %q", out)
	}
}

func TestSecretCutAtTailIsRedacted(t *testing.T) {
	jobs := NewJobs(t.TempDir())
	jobs.MaxBytes = 64
	jobs.Redact = func(s string) string { return strings.ReplaceAll(s, "sk-ABCDEFGHIJKLMNOPQRST", "[redacted]") }
	if err := jobs.Load(); err != nil {
		t.Fatal(err)
	}
	out, err := jobs.Run(context.Background(), RunRequest{
		Command: `printf 'xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxsk-ABCDEFGHIJKLMNOPQRSTyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyy'`,
		Timeout: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "KLMNOPQRST") || strings.Contains(out, "sk-ABC") {
		t.Fatalf("partial secret survived: %q", out)
	}
}

func TestShutdownKillsOwnedProcess(t *testing.T) {
	jobs := NewJobs(t.TempDir())
	if err := jobs.Load(); err != nil {
		t.Fatal(err)
	}
	out, err := jobs.Run(context.Background(), RunRequest{Command: "sleep 30", Background: true, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	id := jobIDFrom(out)
	st, err := jobs.Status(id)
	if err != nil {
		t.Fatal(err)
	}
	if st.ProcStart == 0 || !processAlive(st.PID) {
		t.Fatalf("proc start %d alive %v", st.ProcStart, processAlive(st.PID))
	}
	jobs.Shutdown()
	waitDead(t, st.PID)
	got, err := jobs.Status(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "cancelled" {
		t.Fatalf("status %s", got.Status)
	}
}

func TestOtherProcessLoadLeavesLiveJob(t *testing.T) {
	dir := t.TempDir()
	jobs := NewJobs(dir)
	if err := jobs.Load(); err != nil {
		t.Fatal(err)
	}
	out, err := jobs.Run(context.Background(), RunRequest{Command: "sleep 30", Background: true, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	id := jobIDFrom(out)
	t.Cleanup(func() { jobs.Shutdown() })
	st, err := jobs.Status(id)
	if err != nil {
		t.Fatal(err)
	}
	other := NewJobs(dir)
	if err := other.Load(); err != nil {
		t.Fatal(err)
	}
	if _, err := other.Status(id); err == nil {
		t.Fatal("second process adopted a live job")
	}
	raw, err := os.ReadFile(filepath.Join(dir, id+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"status":"running"`) || !processAlive(st.PID) {
		t.Fatalf("meta %s alive %v", raw, processAlive(st.PID))
	}
}

func TestJobWriterTrickleStaysSmall(t *testing.T) {
	res := testing.Benchmark(func(b *testing.B) {
		f, err := os.Create(filepath.Join(b.TempDir(), "o.log"))
		if err != nil {
			b.Fatal(err)
		}
		w := newJobWriter(f, defaultJobBytes, func(s string) string {
			return strings.ReplaceAll(s, "sk-ABCDEFGHIJKLMNOPQRST", "[redacted]")
		})
		line := []byte("ok  \txbot/internal/some/package\t0.123s some more output text\n")
		for i := 0; i < defaultJobBytes/len(line)+10; i++ {
			if _, err := w.Write(line); err != nil {
				b.Fatal(err)
			}
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := w.Write(line); err != nil {
				b.Fatal(err)
			}
		}
	})
	if res.AllocedBytesPerOp() > 32<<10 || res.NsPerOp() > 200_000 {
		t.Fatalf("full tail rewrite: %d B/op %d ns/op", res.AllocedBytesPerOp(), res.NsPerOp())
	}
}

func BenchmarkJobWriterTrickle(b *testing.B) {
	f, err := os.Create(filepath.Join(b.TempDir(), "o.log"))
	if err != nil {
		b.Fatal(err)
	}
	w := newJobWriter(f, defaultJobBytes, func(s string) string {
		return strings.ReplaceAll(s, "sk-ABCDEFGHIJKLMNOPQRST", "[redacted]")
	})
	line := []byte("ok  \txbot/internal/some/package\t0.123s some more output text\n")
	for i := 0; i < defaultJobBytes/len(line)+10; i++ {
		if _, err := w.Write(line); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := w.Write(line); err != nil {
			b.Fatal(err)
		}
	}
}

func jobIDFrom(out string) string {
	const key = "background "
	i := strings.Index(out, key)
	if i < 0 {
		return ""
	}
	rest := out[i+len(key):]
	if n := strings.IndexByte(rest, '\n'); n >= 0 {
		rest = rest[:n]
	}
	return strings.TrimSpace(rest)
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil
}

func waitDead(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for processAlive(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("pid %d still alive", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// copySleep installs a private sleep binary so a test can see that exact
// process. The file is named sleep, which is what skipAutoBackground matches.
func copySleep(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("pgrep"); err != nil {
		t.Skip("pgrep not installed")
	}
	bin := filepath.Join(t.TempDir(), "sleep")
	if err := exec.Command("cp", "/bin/sleep", bin).Run(); err != nil {
		t.Skip("cannot copy /bin/sleep")
	}
	return bin
}

func waitGone(t *testing.T, bin string) {
	t.Helper()
	// The bracket keeps pgrep from matching its own command line.
	pat := "[" + bin[:1] + "]" + bin[1:]
	deadline := time.Now().Add(time.Second)
	for {
		err := exec.Command("pgrep", "-f", pat).Run()
		if err != nil {
			if _, ok := err.(*exec.ExitError); ok {
				return
			}
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s still running", bin)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
