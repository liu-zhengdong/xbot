package tools

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestJobLogRedactionProperty feeds random streams through jobWriter. No secret
// form may show up in the file, a snapshot, or shell_output, and the finished
// file is exactly the redaction of the whole stream.
func TestJobLogRedactionProperty(t *testing.T) {
	const iters = 10000
	rng := rand.New(rand.NewSource(20261017))
	dir := t.TempDir()
	jobs := NewJobs(dir)
	if err := jobs.Load(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < iters; i++ {
		if err := runRedactionCase(jobs, dir, rng, i, false); err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
	}
}

func FuzzJobLogRedaction(f *testing.F) {
	f.Add([]byte("line one\nline two\n"), []byte("sk-live-0123456789abcdef"))
	f.Add([]byte("AAAA"), []byte("-----BEGIN"))
	f.Fuzz(func(t *testing.T, filler, secret []byte) {
		if len(secret) < 8 || len(secret) > 80 || len(filler) > 400 {
			return
		}
		for _, b := range secret {
			if b < 'a' || b > 'z' {
				return
			}
		}
		dir := t.TempDir()
		jobs := NewJobs(dir)
		if err := jobs.Load(); err != nil {
			t.Fatal(err)
		}
		rng := rand.New(rand.NewSource(int64(len(filler) + len(secret))))
		raw, forms := propStream(rng, []string{string(secret)}, filler)
		if err := driveRedaction(jobs, dir, raw, forms, true); err != nil {
			t.Fatal(err)
		}
	})
}

func runRedactionCase(jobs *Jobs, dir string, rng *rand.Rand, n int, bytewise bool) error {
	secrets := propSecrets(rng, n)
	raw, forms := propStream(rng, secrets, nil)
	if len(raw) > 250 && rng.Intn(10) == 0 {
		bytewise = false
	}
	if len(raw) <= 250 && rng.Intn(8) == 0 {
		bytewise = true
	}
	return driveRedaction(jobs, dir, raw, forms, bytewise)
}

func driveRedaction(jobs *Jobs, dir string, raw []byte, forms []string, bytewise bool) error {
	redact := spanRedactor(forms)
	retain := MinRetain
	for _, form := range forms {
		if len(form)+1 > retain {
			retain = len(form) + 1
		}
	}
	path := filepath.Join(dir, "prop.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	w := newJobWriter(f, 8<<20, redact)
	w.retain = func() int { return retain }
	jobs.mu.Lock()
	if jobs.jobs == nil {
		jobs.jobs = map[string]*live{}
	}
	const id = "sh_00000001"
	jobs.jobs[id] = &live{
		job:  Job{ID: id, Status: "running", Output: path},
		w:    w,
		done: make(chan struct{}),
	}
	jobs.mu.Unlock()

	prev := 0
	var got []byte
	off := 0
	chunks := partition(rand.New(rand.NewSource(int64(len(raw)+retain))), len(raw), bytewise)
	for _, n := range chunks {
		if _, err := w.Write(raw[off : off+n]); err != nil {
			return err
		}
		off += n
		soFar := raw[:off]
		body, written, _, err := w.snapshot()
		if err != nil {
			return err
		}
		if written < prev {
			return fmt.Errorf("stream end moved backward %d -> %d", prev, written)
		}
		prev = written
		disk, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out, err := jobs.Output(id, 0, 0)
		if err != nil {
			return err
		}
		full := redact(string(soFar))
		if !strings.HasPrefix(full, string(disk)) {
			return fmt.Errorf("file is not a redacted prefix\nfile %q\nwant prefix of %q", disk, full)
		}
		if string(body) != string(disk) {
			return fmt.Errorf("snapshot %q file %q", body, disk)
		}
		for _, form := range forms {
			if form == "" {
				continue
			}
			if strings.Contains(string(disk), form) || strings.Contains(string(body), form) || strings.Contains(out, form) {
				return fmt.Errorf("secret form %q leaked", form)
			}
		}
		got = disk
	}
	end, _ := w.span()
	if err := w.Close(); err != nil {
		return err
	}
	finalWritten, _ := w.span()
	if finalWritten < end {
		return fmt.Errorf("close moved the stream backward %d -> %d", end, finalWritten)
	}
	disk, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	want := redact(string(raw))
	if string(disk) != want {
		return fmt.Errorf("final file\n got %q\nwant %q", disk, want)
	}
	_ = got
	return nil
}

func propSecrets(rng *rand.Rand, n int) []string {
	count := 1 + rng.Intn(3)
	out := make([]string, 0, count)
	for i := 0; i < count; i++ {
		if n%5 == 0 && i == 0 {
			out = append(out, zzPEM(byte('A'+rune(n%20))))
			continue
		}
		var b strings.Builder
		b.WriteString("sk-")
		for j := 0; j < 12+rng.Intn(20); j++ {
			b.WriteByte(byte('a' + rng.Intn(26)))
		}
		out = append(out, b.String())
	}
	return out
}

func propStream(rng *rand.Rand, secrets []string, filler []byte) ([]byte, []string) {
	forms := propForms(secrets)
	var b strings.Builder
	writeFiller := func() {
		if len(filler) > 0 {
			b.Write(filler)
			return
		}
		n := rng.Intn(40)
		for i := 0; i < n; i++ {
			switch rng.Intn(4) {
			case 0:
				b.WriteByte('\n')
			case 1:
				b.WriteByte(' ')
			default:
				b.WriteByte('.')
			}
		}
	}
	repeats := 1 + rng.Intn(3)
	for r := 0; r < repeats; r++ {
		for i, form := range forms {
			if rng.Intn(3) != 0 {
				writeFiller()
			}
			b.WriteString(form)
			if i+1 < len(forms) && rng.Intn(2) == 0 {
				b.WriteString(forms[rng.Intn(len(forms))])
			}
		}
		writeFiller()
	}
	return []byte(b.String()), forms
}

func propForms(secrets []string) []string {
	var forms []string
	seen := map[string]bool{}
	add := func(s string) {
		if len(s) < 6 || seen[s] {
			return
		}
		seen[s] = true
		forms = append(forms, s)
	}
	for _, secret := range secrets {
		add(secret)
		add(url.QueryEscape(secret))
		add(url.PathEscape(secret))
		quoted, err := json.Marshal(secret)
		if err == nil {
			add(string(quoted))
		}
		add(base64.StdEncoding.EncodeToString([]byte(secret)))
		add(base64.RawStdEncoding.EncodeToString([]byte(secret)))
		add(base64.URLEncoding.EncodeToString([]byte(secret)))
		add(base64.RawURLEncoding.EncodeToString([]byte(secret)))
	}
	sort.Slice(forms, func(i, j int) bool { return len(forms[i]) > len(forms[j]) })
	return forms
}

func spanRedactor(forms []string) func(string) string {
	return func(s string) string {
		for _, form := range forms {
			s = strings.ReplaceAll(s, form, "[secret]")
		}
		return s
	}
}

func partition(rng *rand.Rand, n int, bytewise bool) []int {
	if n == 0 {
		return nil
	}
	if bytewise {
		out := make([]int, n)
		for i := range out {
			out[i] = 1
		}
		return out
	}
	var out []int
	left := n
	for left > 0 {
		m := 1
		switch rng.Intn(10) {
		case 0:
			m = 1
		case 1, 2:
			m = 1 + rng.Intn(min(8, left))
		default:
			m = 1 + rng.Intn(left)
		}
		if m > left {
			m = left
		}
		out = append(out, m)
		left -= m
	}
	return out
}
