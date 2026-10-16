package tools

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestShellEchoAndExit(t *testing.T) {
	sh := NewShell()
	out, err := sh.Run(context.Background(), json.RawMessage(`{"command":"echo hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "exit 0") || !strings.Contains(out, "hello") {
		t.Fatalf("%q", out)
	}
	out, err = sh.Run(context.Background(), json.RawMessage(`{"command":"exit 3"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "exit 3") {
		t.Fatalf("%q", out)
	}
}

func TestShellOmitsProviderKeysUnlessOptIn(t *testing.T) {
	t.Setenv("XBOT_API_KEY", "sekret-xbot")
	t.Setenv("DEEPSEEK_API_KEY", "sekret-deep")
	t.Setenv("XBOT_KEEP_ME", "kept-value")
	sh := NewShell()
	sh.KeyEnv = []string{"DEEPSEEK_API_KEY"}
	out, err := sh.Run(context.Background(), json.RawMessage(`{"command":"printenv XBOT_API_KEY DEEPSEEK_API_KEY XBOT_KEEP_ME || true"}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "sekret-xbot") || strings.Contains(out, "sekret-deep") || !strings.Contains(out, "kept-value") {
		t.Fatalf("%q", out)
	}
	need, summary, err := sh.Confirm(json.RawMessage(`{"command":"printenv XBOT_API_KEY","keys":true}`))
	if err != nil || need || !strings.Contains(summary, "with provider keys") {
		t.Fatalf("need=%v summary=%q err=%v", need, summary, err)
	}
	out, err = sh.Run(context.Background(), json.RawMessage(`{"command":"printenv XBOT_API_KEY DEEPSEEK_API_KEY","keys":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "sekret-xbot") || !strings.Contains(out, "sekret-deep") {
		t.Fatalf("%q", out)
	}
}

func TestShellOptInRestoresHeldKeys(t *testing.T) {
	t.Setenv("XBOT_API_KEY", "sekret-held")
	if err := os.Unsetenv("XBOT_API_KEY"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { SetKeyLookup(nil) })
	SetKeyLookup(func(name string) (string, bool) {
		if name == "XBOT_API_KEY" {
			return "sekret-held", true
		}
		return "", false
	})
	sh := NewShell()
	out, err := sh.Run(context.Background(), json.RawMessage(`{"command":"printenv XBOT_API_KEY || true"}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "sekret-held") {
		t.Fatalf("default call saw the held key: %q", out)
	}
	out, err = sh.Run(context.Background(), json.RawMessage(`{"command":"printenv XBOT_API_KEY","keys":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "sekret-held") {
		t.Fatalf("opt-in missed the held key: %q", out)
	}
}

// Run still refuses an off machine even though the kernel review path decides
// confirmation. This is the last line of defense for a direct call.
func TestShellRunRefusesMachineOff(t *testing.T) {
	t.Cleanup(func() { SetMachineGate(nil) })
	SetMachineGate(func() (string, error) { return "off", nil })
	sh := NewShell()
	if _, err := sh.Run(context.Background(), json.RawMessage(`{"command":"echo no"}`)); err == nil || !strings.Contains(err.Error(), "off") {
		t.Fatal(err)
	}
}

// Confirm never decides; it only builds the card summary.
func TestShellConfirmOnlyBuildsSummary(t *testing.T) {
	sh := NewShell()
	need, summary, err := sh.Confirm(json.RawMessage(`{"command":"date"}`))
	if err != nil || need || summary != "date" {
		t.Fatalf("date need=%v summary=%q err=%v", need, summary, err)
	}
	need, summary, err = sh.Confirm(json.RawMessage(`{"command":"rm -rf /tmp/nope"}`))
	if err != nil || need || summary != "rm -rf /tmp/nope" {
		t.Fatalf("confirm need=%v summary=%q err=%v", need, summary, err)
	}
	if _, err := sh.Run(context.Background(), json.RawMessage(`{"command":"  "}`)); err == nil {
		t.Fatal("expected empty command error")
	}
}

func TestWriteEditRunRefusesMachineOff(t *testing.T) {
	t.Cleanup(func() { SetMachineGate(nil) })
	SetMachineGate(func() (string, error) { return "off", nil })
	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	writeArgs := json.RawMessage(`{"path":"` + path + `","content":"x"}`)
	editArgs := json.RawMessage(`{"path":"` + path + `","old_string":"x","new_string":"y"}`)
	w := &WriteFile{}
	ed := &EditFile{}
	if _, err := w.Run(context.Background(), writeArgs); err == nil || !strings.Contains(err.Error(), "off") {
		t.Fatalf("off write run err=%v", err)
	}
	if _, err := ed.Run(context.Background(), editArgs); err == nil || !strings.Contains(err.Error(), "off") {
		t.Fatalf("off edit run err=%v", err)
	}
}

// ResolvedPath reports the path the kernel uses to decide a self-edit.
func TestWriteEditResolvedPath(t *testing.T) {
	dir := t.TempDir()
	base, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(base, "note.txt")
	w := &WriteFile{Root: dir}
	if got := w.ResolvedPath(json.RawMessage(`{"path":"note.txt","content":"x"}`)); got != want {
		t.Fatalf("write path %q want %q", got, want)
	}
	ed := &EditFile{Root: dir}
	if got := ed.ResolvedPath(json.RawMessage(`{"path":"note.txt","old_string":"a","new_string":"b"}`)); got != want {
		t.Fatalf("edit path %q want %q", got, want)
	}
}

func TestShellTimeoutKillsGroup(t *testing.T) {
	if _, err := exec.LookPath("pgrep"); err != nil {
		t.Skip("pgrep not installed")
	}
	marker := "xbottest-" + strings.ReplaceAll(t.Name(), "/", "-")
	bin := filepath.Join(t.TempDir(), marker)
	if err := exec.Command("cp", "/bin/sleep", bin).Run(); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]string{"command": bin + " 30"})
	if err != nil {
		t.Fatal(err)
	}
	sh := &Shell{DefaultTimeout: 200 * time.Millisecond, MaxTimeout: 200 * time.Millisecond, MaxOutput: 1024}
	start := time.Now()
	out, err := sh.Run(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("timeout too slow: %s (%s)", time.Since(start), out)
	}
	if !strings.Contains(out, "timed out") {
		t.Fatalf("%q", out)
	}
	time.Sleep(150 * time.Millisecond)
	// The bracket keeps pgrep from matching its own command line.
	pgrep, err := exec.Command("pgrep", "-f", "[x]"+marker[1:]).CombinedOutput()
	if err == nil {
		t.Fatalf("process still running: %s", pgrep)
	}
}

func TestShellTruncates(t *testing.T) {
	sh := &Shell{MaxOutput: 64}
	out, err := sh.Run(context.Background(), json.RawMessage(`{"command":"awk 'BEGIN{while(c++<100) print \"0123456789\"}'"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[truncated]") {
		t.Fatalf("expected truncation, got %d bytes: %q", len(out), out)
	}
	if len(out) > 256 {
		t.Fatalf("output too large: %d", len(out))
	}
}

func TestReadFileOffsetLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\nfour\n"), 0644); err != nil {
		t.Fatal(err)
	}
	rd := &ReadFile{}
	need, _, err := rd.Confirm(json.RawMessage(`{"path":"` + path + `","offset":2,"limit":2}`))
	if err != nil || need {
		t.Fatalf("read should not require confirm: need=%v err=%v", need, err)
	}
	out, err := rd.Run(context.Background(), json.RawMessage(`{"path":"`+path+`","offset":2,"limit":2}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "2|two") || !strings.Contains(out, "3|three") || strings.Contains(out, "4|four") || strings.Contains(out, "1|one") {
		t.Fatalf("%q", out)
	}
	out, err = rd.Run(context.Background(), json.RawMessage(`{"path":"`+path+`","offset":50}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "past end") {
		t.Fatalf("%q", out)
	}
	if _, err := rd.Run(context.Background(), json.RawMessage(`{"path":"`+dir+`"}`)); err == nil {
		t.Fatal("expected directory error")
	}
	if _, err := rd.Run(context.Background(), json.RawMessage(`{"path":"`+filepath.Join(dir, "missing")+`"}`)); err == nil {
		t.Fatal("expected missing file error")
	}
}

func TestReadFileByteCap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.txt")
	var b strings.Builder
	for i := 0; i < 50; i++ {
		b.WriteString(strings.Repeat("x", 40) + "\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0644); err != nil {
		t.Fatal(err)
	}
	rd := &ReadFile{MaxBytes: 80}
	out, err := rd.Run(context.Background(), json.RawMessage(`{"path":"`+path+`","limit":50}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[truncated]") {
		t.Fatalf("%q", out)
	}
}

func TestWriteFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "note.txt")
	w := &WriteFile{}
	args := json.RawMessage(`{"path":"` + path + `","content":"hello"}`)
	need, summary, err := w.Confirm(args)
	if err != nil || need || !strings.Contains(summary, path) {
		t.Fatalf("confirm need=%v summary=%q err=%v", need, summary, err)
	}
	out, err := w.Run(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "wrote 5 bytes") {
		t.Fatalf("%q", out)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "hello" {
		t.Fatalf("file %q err=%v", got, err)
	}
	if _, err := w.Run(context.Background(), json.RawMessage(`{"path":"","content":"x"}`)); err == nil {
		t.Fatal("expected empty path error")
	}
	big := strings.Repeat("a", maxWriteBytes+1)
	raw, _ := json.Marshal(map[string]string{"path": path, "content": big})
	if _, err := w.Run(context.Background(), raw); err == nil {
		t.Fatal("expected size error")
	}
	if _, err := w.Run(context.Background(), json.RawMessage(`{"path":"`+dir+`","content":"x"}`)); err == nil {
		t.Fatal("expected directory error")
	}
}

func TestWriteFileSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.Mkdir(real, 0755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(link, "f.txt")
	w := &WriteFile{}
	if _, err := w.Run(context.Background(), json.RawMessage(`{"path":"`+path+`","content":"via-link"}`)); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(real, "f.txt"))
	if err != nil || string(got) != "via-link" {
		t.Fatalf("got %q err=%v", got, err)
	}
}

func TestEditFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("alpha beta alpha"), 0644); err != nil {
		t.Fatal(err)
	}
	ed := &EditFile{}
	if _, err := ed.Run(context.Background(), json.RawMessage(`{"path":"`+path+`","old_string":"alpha","new_string":"A"}`)); err == nil {
		t.Fatal("expected ambiguous match")
	}
	if _, err := ed.Run(context.Background(), json.RawMessage(`{"path":"`+path+`","old_string":"missing","new_string":"x"}`)); err == nil {
		t.Fatal("expected not found")
	}
	if _, err := ed.Run(context.Background(), json.RawMessage(`{"path":"`+path+`","old_string":"","new_string":"x"}`)); err == nil {
		t.Fatal("expected empty old_string")
	}
	if _, err := ed.Run(context.Background(), json.RawMessage(`{"path":"`+path+`","old_string":"beta","new_string":"beta"}`)); err == nil {
		t.Fatal("expected identical strings")
	}
	need, _, err := ed.Confirm(json.RawMessage(`{"path":"` + path + `","old_string":"beta","new_string":"BETA"}`))
	if err != nil || need {
		t.Fatalf("confirm: %v need=%v", err, need)
	}
	out, err := ed.Run(context.Background(), json.RawMessage(`{"path":"`+path+`","old_string":"beta","new_string":"BETA"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "updated") {
		t.Fatalf("%q", out)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "alpha BETA alpha" {
		t.Fatalf("%q err=%v", got, err)
	}
}

func TestResolveRejectsNUL(t *testing.T) {
	if _, err := Resolve("a\x00b"); err == nil {
		t.Fatal("expected invalid path")
	}
}
