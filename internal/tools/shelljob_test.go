package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
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
	if !strings.Contains(string(raw), "run_in_background") || !strings.Contains(string(raw), "2") {
		t.Fatalf("schema %s", raw)
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
	w.forms = func() []string { return []string{"SUPERSECRET"} }
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
	raw, err := json.Marshal(map[string]string{"description": "Verify the scenario behavior", "command": "echo " + marker + "; sleep 30"})
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
	raw, err := json.Marshal(map[string]any{"description": "Verify the scenario behavior", "command": "sleep 30", "run_in_background": true})
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

func TestSleepUsesCommonBackgroundWait(t *testing.T) {
	bin := copySleep(t)
	dir := t.TempDir()
	jobs := NewJobs(dir)
	if err := jobs.Load(); err != nil {
		t.Fatal(err)
	}
	sh := &Shell{DefaultTimeout: 200 * time.Millisecond, MaxTimeout: 200 * time.Millisecond, Jobs: jobs}
	raw, err := json.Marshal(map[string]string{"description": "Verify the scenario behavior", "command": bin + " 30"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := sh.Run(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "background ") {
		t.Fatalf("%s", out)
	}
	if _, err := jobs.Cancel(jobIDFrom(out)); err != nil {
		t.Fatal(err)
	}
	jobs.Shutdown()
	waitGone(t, bin)
}

func TestPollingUsesCommonBackgroundWait(t *testing.T) {
	bin := copySleep(t)
	dir := t.TempDir()
	jobs := NewJobs(dir)
	if err := jobs.Load(); err != nil {
		t.Fatal(err)
	}
	sh := &Shell{DefaultTimeout: 300 * time.Millisecond, MaxTimeout: 300 * time.Millisecond, Jobs: jobs}
	raw, err := json.Marshal(map[string]string{"description": "Verify the scenario behavior", "command": "while true; do " + bin + " 1; done"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := sh.Run(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "background ") {
		t.Fatalf("%s", out)
	}
	if _, err := jobs.Cancel(jobIDFrom(out)); err != nil {
		t.Fatal(err)
	}
	jobs.Shutdown()
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
		Command: "printf 'one\\ntwo\\nthree\\n'", Background: true, Timeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	id := jobIDFrom(out)
	t.Cleanup(func() { _, _ = jobs.Cancel(id) })
	deadline := time.Now().Add(2 * time.Second)
	for {
		st, err := jobs.Status(id)
		if err == nil && st.Status == "done" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("status %+v err %v", st, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	tail, err := jobs.Output(id, 0, 1)
	if err != nil || !strings.Contains(tail, "three") {
		t.Fatalf("tail %q err %v", tail, err)
	}
	if strings.Contains(tail, "\none\n") || strings.HasPrefix(strings.TrimSpace(tail), "one\n") {
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
	if strings.Contains(again, "\none\n") || !strings.Contains(again, "two") {
		t.Fatalf("offset %s", again)
	}
}

func TestBackgroundFileRedactsSecret(t *testing.T) {
	dir := t.TempDir()
	jobs := NewJobs(dir)
	jobs.Redact = func(s string) string { return strings.ReplaceAll(s, "SUPERSECRET", "[secret]") }
	jobs.Forms = func() []string { return []string{"SUPERSECRET"} }
	if err := jobs.Load(); err != nil {
		t.Fatal(err)
	}
	out, err := jobs.Run(context.Background(), RunRequest{
		Command: "printf 'SUPERSECRET\\n'; sleep 30", Background: true, Timeout: time.Second,
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
	if view, err := next.Status(id); err != nil || view.Local || view.Status != "running" {
		t.Fatalf("foreign view: %+v %v", view, err)
	}
	if _, err := next.Cancel(id); err != ErrForeignJob {
		t.Fatalf("foreign job was adopted for cancellation: %v", err)
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
	out, err := sh.Run(context.Background(), json.RawMessage(`{"description":"Verify the scenario behavior","command":"echo hello"}`))
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
	jobs.Forms = func() []string { return []string{"sk-ABCDEFGHIJKLMNOPQRST"} }
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
	if view, err := other.Status(id); err != nil || view.Local || view.Status != "running" {
		t.Fatalf("foreign view: %+v %v", view, err)
	}
	if _, err := other.Cancel(id); err != ErrForeignJob {
		t.Fatalf("foreign cancellation: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, id+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"status":"running"`) || !processAlive(st.PID) {
		t.Fatalf("meta %s alive %v", raw, processAlive(st.PID))
	}
}

func TestLongMultilineSecretOnDisk(t *testing.T) {
	var b strings.Builder
	b.WriteString("-----BEGIN " + "PRIVATE KEY-----\n")
	for i := 0; i < 26; i++ {
		b.WriteString(strings.Repeat(string(rune('A'+i%26)), 64) + "\n")
	}
	b.WriteString("-----END " + "PRIVATE KEY-----")
	secret := b.String()
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "key.pem")
	if err := os.WriteFile(keyFile, []byte(secret+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	jobs := NewJobs(filepath.Join(dir, "shell-jobs"))
	jobs.Redact = func(s string) string { return strings.ReplaceAll(s, secret, "[secret]") }
	jobs.Forms = func() []string { return []string{secret} }
	if err := jobs.Load(); err != nil {
		t.Fatal(err)
	}
	out, err := jobs.Run(context.Background(), RunRequest{Command: "cat " + keyFile + "; sleep 0.2", Timeout: time.Minute, Background: true})
	if err != nil {
		t.Fatal(err)
	}
	id := strings.Fields(out)[1]
	t.Cleanup(func() { _, _ = jobs.Cancel(id) })
	deadline := time.Now().Add(3 * time.Second)
	for {
		st, err := jobs.Status(id)
		if err == nil && st.Status != "running" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("status %+v err %v", st, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	disk, err := os.ReadFile(filepath.Join(jobs.Dir, id+".log"))
	if err != nil {
		t.Fatal(err)
	}
	body := strings.Repeat("M", 64)
	if strings.Contains(string(disk), body) {
		t.Fatalf("secret (%d bytes) written to job log unredacted (%d bytes on disk)", len(secret), len(disk))
	}
	read, err := jobs.Output(id, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(read, body) {
		t.Fatalf("Jobs.Output returns the unredacted secret")
	}
}

func TestLongSecretSplitAcrossWrites(t *testing.T) {
	secret := strings.Repeat("M", 1500)
	f, err := os.Create(filepath.Join(t.TempDir(), "o.log"))
	if err != nil {
		t.Fatal(err)
	}
	w := newJobWriter(f, defaultJobBytes, func(s string) string {
		return strings.ReplaceAll(s, secret, "[secret]")
	})
	w.forms = func() []string { return []string{secret} }
	if _, err := w.Write([]byte(secret[:800])); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(secret[800:] + "\n")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "MMMM") || !strings.Contains(string(body), "[secret]") {
		t.Fatalf("file %q", body)
	}
}

func TestStreamOffsetDoesNotMoveBackward(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "o.log"))
	if err != nil {
		t.Fatal(err)
	}
	w := newJobWriter(f, 4096, func(s string) string {
		return strings.ReplaceAll(s, "SUPERSECRET", "[secret]")
	})
	w.forms = func() []string { return []string{"SUPERSECRET"} }
	if _, err := w.Write([]byte("pre SUPERSE")); err != nil {
		t.Fatal(err)
	}
	first, _ := w.span()
	if _, err := w.Write([]byte("CRET post")); err != nil {
		t.Fatal(err)
	}
	second, _ := w.span()
	if second < first {
		t.Fatalf("stream end moved backward %d -> %d", first, second)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	third, _ := w.span()
	if third < second {
		t.Fatalf("stream end moved backward %d -> %d", second, third)
	}
}

func TestJobWriterTrickleStaysSmall(t *testing.T) {
	res := testing.Benchmark(func(b *testing.B) {
		f, err := os.Create(filepath.Join(b.TempDir(), "o.log"))
		if err != nil {
			b.Fatal(err)
		}
		redact, forms := benchRedactor()
		w := newJobWriter(f, defaultJobBytes, redact)
		w.forms = forms
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

func BenchmarkJobWriterBulk(b *testing.B) {
	line := []byte("ok  \txbot/internal/some/package\t0.123s some more output text\n")
	var chunk []byte
	for len(chunk) < 32<<10 {
		chunk = append(chunk, line...)
	}
	f, err := os.Create(filepath.Join(b.TempDir(), "o.log"))
	if err != nil {
		b.Fatal(err)
	}
	redact, forms := benchRedactor()
	w := newJobWriter(f, defaultJobBytes, redact)
	w.forms = forms
	if _, err := w.Write(chunk); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := w.Write(chunk); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkJobWriterTrickle(b *testing.B) {
	f, err := os.Create(filepath.Join(b.TempDir(), "o.log"))
	if err != nil {
		b.Fatal(err)
	}
	redact, forms := benchRedactor()
	w := newJobWriter(f, defaultJobBytes, redact)
	w.forms = forms
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

// benchRedactor replaces a short token and a ~1.7 KiB PEM form, so the
// prefix-hold scan runs on the same shape of secret the vault would list.
func benchRedactor() (func(string) string, func() []string) {
	secret := "sk-ABCDEFGHIJKLMNOPQRST"
	var b strings.Builder
	b.WriteString("-----BEGIN " + "PRIVATE KEY-----\n")
	line := strings.Repeat("M", 64)
	for i := 0; i < 26; i++ {
		b.WriteString(line + "\n")
	}
	b.WriteString("-----END " + "PRIVATE KEY-----")
	pem := b.String()
	forms := []string{pem, secret}
	redact := func(s string) string {
		s = strings.ReplaceAll(s, pem, "[redacted]")
		return strings.ReplaceAll(s, secret, "[redacted]")
	}
	return redact, func() []string { return forms }
}

func TestPrefixSuffixHold(t *testing.T) {
	cases := []struct {
		raw, form string
		want      int
	}{
		{"xxSECRET", "SECRET", 6},
		{"xxSECR", "SECRET", 4},
		{"hello\n", "SECRET", 0},
		{"SECRET", "SECRET", 6},
		{"SECRET\n", "SECRET", 0},
		{"aaaa", "aaa", 3},
		{"baaa", "aaa", 3},
		{"xyz", "aaa", 0},
		{"pre SUPERSE", "SUPERSECRET", 7},
	}
	for _, tc := range cases {
		got := prefixSuffixLen(tc.raw, tc.form, prefixFunc(tc.form))
		if got != tc.want {
			t.Errorf("raw %q form %q: got %d want %d", tc.raw, tc.form, got, tc.want)
		}
	}
}

func TestShortRunningOutputIsVisible(t *testing.T) {
	dir := t.TempDir()
	jobs := NewJobs(dir)
	if err := jobs.Load(); err != nil {
		t.Fatal(err)
	}
	out, err := jobs.Run(context.Background(), RunRequest{
		Command:    "printf 'Compiling…\\nerror: missing dep foo\\n'; sleep 30",
		Background: true,
		Timeout:    time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	id := jobIDFrom(out)
	t.Cleanup(func() { _, _ = jobs.Cancel(id) })
	deadline := time.Now().Add(2 * time.Second)
	var text string
	for {
		text, err = jobs.Output(id, 0, 0)
		if err == nil && strings.Contains(text, "Compiling") && strings.Contains(text, "error: missing dep foo") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("output %q err %v", text, err)
		}
		time.Sleep(15 * time.Millisecond)
	}
	if strings.Contains(text, "held_bytes:") {
		t.Fatalf("banner was held back: %s", text)
	}
	if !strings.Contains(text, "next_offset:") {
		t.Fatalf("%s", text)
	}
	st, err := jobs.Status(id)
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != "running" || st.Bytes == 0 {
		t.Fatalf("status %+v", st)
	}
	disk, err := os.ReadFile(st.Output)
	if err != nil || !strings.Contains(string(disk), "missing dep foo") {
		t.Fatalf("file %q err %v", disk, err)
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
// process. It deliberately uses a sleep-shaped name to cover the shared wait policy.
func copySleep(t *testing.T) string {
	t.Helper()
	return privateSleep(t, "sleep")
}

// privateSleep puts sleep at a fresh path so pgrep -f can find exactly that
// process by its argv[0]. It is a symlink, not a copy: macOS SIGKILLs a copied
// platform binary such as /bin/sleep the moment it runs.
func privateSleep(t *testing.T, name string) string {
	t.Helper()
	if _, err := exec.LookPath("pgrep"); err != nil {
		t.Skip("pgrep not installed")
	}
	bin := filepath.Join(t.TempDir(), name)
	if err := os.Symlink("/bin/sleep", bin); err != nil {
		t.Skip("cannot link /bin/sleep")
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

// Cancel and Shutdown return only after OnDone, which may still write into the
// job directory (a wake, the notified mark). A job that has just exited counts
// too: Status says done before its record is written. Returning earlier let a
// test's TempDir cleanup race those writes (#222, #223, #51).
func TestStopWaitsForOnDone(t *testing.T) {
	for _, stop := range []string{"cancel", "shutdown", "cancel-exited"} {
		t.Run(stop, func(t *testing.T) {
			dir := t.TempDir()
			j := NewJobs(dir)
			var finished atomic.Bool
			j.OnDone = func(job Job) {
				time.Sleep(100 * time.Millisecond)
				_ = os.WriteFile(filepath.Join(dir, job.ID+".wake"), nil, 0600)
				finished.Store(true)
			}
			cmd := "sleep 30"
			if stop == "cancel-exited" {
				cmd = "true"
			}
			out, err := j.Run(context.Background(), RunRequest{Command: cmd, Background: true})
			if err != nil {
				t.Fatal(err)
			}
			id := jobIDFrom(out)
			switch stop {
			case "shutdown":
				j.Shutdown()
			case "cancel-exited":
				deadline := time.Now().Add(5 * time.Second)
				for {
					st, err := j.Status(id)
					if err == nil && st.Status != "running" {
						break
					}
					if time.Now().After(deadline) {
						t.Fatalf("status %+v err %v", st, err)
					}
					time.Sleep(5 * time.Millisecond)
				}
				fallthrough
			default:
				if _, err := j.Cancel(id); err != nil {
					t.Fatal(err)
				}
			}
			if !finished.Load() {
				t.Fatal(stop + " returned before OnDone finished writing")
			}
		})
	}
}
