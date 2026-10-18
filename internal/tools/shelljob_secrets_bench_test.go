package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/vault"
)

// manySecretsWriter is a job writer over a real vault with n secrets, and a
// chunk of output in which every line carries one of them (#15).
func manySecretsWriter(tb testing.TB, n int) (*jobWriter, []byte) {
	tb.Helper()
	dir := tb.TempDir()
	st, err := vault.Open(filepath.Join(dir, "vault"), filepath.Join(dir, "key"))
	if err != nil {
		tb.Fatal(err)
	}
	values := make([]string, n)
	for i := range values {
		values[i] = fmt.Sprintf("tok%02d-Zx8Qp2Lm9Vt4Rk7Wn3Hs6J", i)
		if err := st.Set(fmt.Sprintf("S%02d", i), values[i], []string{"example.com"}); err != nil {
			tb.Fatal(err)
		}
	}
	var chunk []byte
	for i := 0; len(chunk) < 32<<10; i++ {
		chunk = append(chunk, fmt.Sprintf("step %d: auth %s ok\n", i, values[i%n])...)
	}
	f, err := os.Create(filepath.Join(dir, "o.log"))
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { f.Close() })
	w := newJobWriter(f, defaultJobBytes, st.Redact)
	w.forms = st.Forms
	return w, chunk
}

func BenchmarkJobWriterManySecrets(b *testing.B) {
	w, chunk := manySecretsWriter(b, 50)
	b.SetBytes(int64(len(chunk)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := w.Write(chunk); err != nil {
			b.Fatal(err)
		}
	}
}

// Fifty vault secrets with one on every line used to cost about 28 ms per
// 32 KiB: forms were rebuilt on every call and every needle was tried at
// every byte (#15). The guard leaves room for slow CI machines.
func TestJobWriterManySecretsIsFast(t *testing.T) {
	res := testing.Benchmark(BenchmarkJobWriterManySecrets)
	if ns := res.NsPerOp(); ns > 10_000_000 {
		t.Fatalf("50 secrets: %d ns per 32 KiB", ns)
	}
}

// A value the vault learns after the job wrote it is still redacted when the
// output is read (#17).
func TestOutputRedactsValueLearnedLater(t *testing.T) {
	dir := t.TempDir()
	st, err := vault.Open(filepath.Join(dir, "vault"), filepath.Join(dir, "key"))
	if err != nil {
		t.Fatal(err)
	}
	const secret = "late-Zx8Qp2Lm9Vt4Rk7Wn3"
	j := NewJobs(filepath.Join(dir, "jobs"))
	j.Redact = st.Redact
	j.Forms = st.Forms
	out, err := j.Run(context.Background(), RunRequest{Command: "printf '" + secret + "\\n'", Background: true})
	if err != nil {
		t.Fatal(err)
	}
	id := jobIDFrom(out)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if st, err := j.Status(id); err == nil && st.Status == "done" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("job did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := j.Cancel(id); err != nil { // waits for the record
		t.Fatal(err)
	}
	if err := st.Set("L", secret, []string{"example.com"}); err != nil {
		t.Fatal(err)
	}
	text, err := j.Output(id, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, secret) || !strings.Contains(text, "[secret]") {
		t.Fatalf("output %q", text)
	}
}
