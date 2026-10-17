package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestZZR3JobsRepeatedToken(t *testing.T) {
	tok := "sk-live-0123456789abcdefghijklmnopqrstuvwxyzABCD"
	dir := t.TempDir()
	j := NewJobs(filepath.Join(dir, "shell-jobs"))
	j.Redact = func(s string) string { return strings.ReplaceAll(s, tok, "[secret]") }
	if err := j.Load(); err != nil {
		t.Fatal(err)
	}
	cmd := `T=` + tok + `; for i in $(seq 60); do echo "Bearer $T"; done; printf "Bearer sk-live-0123456789abcdef"; sleep 0.3; echo "ghijklmnopqrstuvwxyzABCD"`
	out, err := j.Run(context.Background(), RunRequest{Command: cmd, Timeout: time.Minute, Background: true})
	if err != nil {
		t.Fatal(err)
	}
	id := strings.Fields(out)[1]
	t.Cleanup(func() { _, _ = j.Cancel(id) })
	deadline := time.Now().Add(3 * time.Second)
	for {
		st, err := j.Status(id)
		if err == nil && st.Status != "running" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("status %+v err %v", st, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	disk, err := os.ReadFile(j.get(id).snapshot().Output)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(disk), tok) {
		t.Errorf("full token on disk: ...%q", string(disk)[len(disk)-80:])
	}
}

func zzPEM(c byte) string {
	var b strings.Builder
	b.WriteString("-----BEGIN PRIVATE KEY-----\n")
	for i := 0; i < 26; i++ {
		b.WriteString(strings.Repeat(string(rune(int(c)+i%20)), 64) + "\n")
	}
	b.WriteString("-----END PRIVATE KEY-----")
	return b.String()
}

func zzWriter(t *testing.T, secrets ...string) (*jobWriter, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "out.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0600)
	if err != nil {
		t.Fatal(err)
	}
	max := 0
	for _, s := range secrets {
		if len(s) > max {
			max = len(s)
		}
	}
	w := newJobWriter(f, 256<<10, func(s string) string {
		for _, sec := range secrets {
			s = strings.ReplaceAll(s, sec, "[secret]")
		}
		return s
	})
	w.retain = func() int { return max + 1 }
	return w, path
}

// The same key printed twice (cat key.pem key.pem), with the pipe splitting the
// second copy. The complete first copy changes the redacted tail, so a suffix
// check fails and the whole block — including the raw first half of the second
// copy — used to be committed.
func TestZZR3SecondCopyCommittedRaw(t *testing.T) {
	s := zzPEM('A')
	w, path := zzWriter(t, s)
	if _, err := w.Write([]byte(s + "\n" + s[:800])); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(s[800:] + "\n")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	disk, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(disk), strings.Repeat("C", 64)) {
		t.Errorf("part of the secret committed unredacted:\n%q", string(disk))
	}
}

// Two different keys back to back, second split by the pipe.
func TestZZR3TwoKeysSplit(t *testing.T) {
	a, b := zzPEM('A'), zzPEM('a')
	w, path := zzWriter(t, a, b)
	if _, err := w.Write([]byte(a + "\n" + b[:900])); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(b[900:] + "\n")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	disk, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(disk), strings.Repeat("c", 64)) {
		t.Errorf("second key partly committed unredacted")
	}
}

// Live read while the secret is still arriving: the retained raw prefix must
// not be on disk or in a snapshot.
func TestZZR3PreviewShowsPartialSecret(t *testing.T) {
	s := zzPEM('A')
	w, path := zzWriter(t, s)
	if _, err := w.Write([]byte(s[:700])); err != nil {
		t.Fatal(err)
	}
	body, _, _, err := w.snapshot()
	if err != nil {
		t.Fatal(err)
	}
	disk, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Repeat("C", 64)
	if strings.Contains(string(body), line) || strings.Contains(string(disk), line) {
		t.Errorf("partial secret is visible before the rest arrives")
	}
	if _, err := w.Write([]byte(s[700:] + "\n")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

// A short token repeated on every line, with one occurrence split across writes.
func TestZZR3RepeatedShortTokenSplit(t *testing.T) {
	tok := "sk-live-0123456789abcdefghijklmnopqrstuvwxyzABCD"
	w, path := zzWriter(t, tok)
	w.retain = nil // MinRetain
	var b strings.Builder
	for i := 0; i < 60; i++ {
		b.WriteString("> Authorization: Bearer " + tok + "\n")
	}
	b.WriteString("> Authorization: Bearer " + tok[:24])
	if _, err := w.Write([]byte(b.String())); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(tok[24:] + "\n")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	disk, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(disk), tok[:20]) {
		tail := string(disk)
		if len(tail) > 120 {
			tail = tail[len(tail)-120:]
		}
		t.Errorf("first half of the token committed unredacted: %q", tail)
	}
}
