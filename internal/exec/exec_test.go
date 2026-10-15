package exec

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"xbot/internal/provider"
)

type seqProv struct {
	mu   sync.Mutex
	n    int
	hold chan struct{}
}

func (p *seqProv) Chat(ctx context.Context, req provider.Request, _ func(string)) (provider.Message, error) {
	p.mu.Lock()
	n := p.n
	p.n++
	p.mu.Unlock()
	if n == 0 {
		select {
		case <-ctx.Done():
			return provider.Message{}, ctx.Err()
		case <-p.hold:
		}
		return provider.Message{Role: provider.RoleAssistant, Content: "part A done"}, nil
	}
	last := req.Messages[len(req.Messages)-1].Content
	if strings.Contains(last, "PART_B") {
		return provider.Message{Role: provider.RoleAssistant, Content: "part B done"}, nil
	}
	return provider.Message{Role: provider.RoleAssistant, Content: "unexpected"}, nil
}

func TestQueueRunsAfterCurrent(t *testing.T) {
	hold := make(chan struct{})
	loc := NewLocal(Limits{Timeout: 10 * time.Second, ProgressEvery: -1, MaxSteps: 4})
	loc.Prov = &seqProv{hold: hold}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		for range loc.Events() {
		}
	}()
	dir := t.TempDir()
	id := "sa_abcdef01"
	if err := loc.Start(ctx, StartRequest{ID: id, Task: "first", Mode: ModeBrief, Dir: dir}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	if err := loc.Control(ctx, id, Control{Kind: KindQueue, Text: "PART_B"}); err != nil {
		t.Fatal(err)
	}
	close(hold)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		meta, _, err := ReadStatus(dir)
		if err == nil && meta.Status == statusDone && strings.Contains(meta.Summary, "part B done") {
			raw := subFile(t, dir)
			if !strings.Contains(raw, "part A done") || !strings.Contains(raw, "PART_B") {
				t.Fatalf("transcript %s", raw)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal(subFile(t, dir))
}

func TestMaxConcurrent(t *testing.T) {
	block := make(chan struct{})
	loc := NewLocal(Limits{MaxConcurrent: 2, Timeout: 10 * time.Second, ProgressEvery: -1})
	loc.Prov = &blockProv{block: block}
	ctx := context.Background()
	dir := t.TempDir()
	for _, id := range []string{"sa_aaaaaaaa", "sa_bbbbbbbb"} {
		if err := loc.Start(ctx, StartRequest{ID: id, Task: "t", Mode: ModeBrief, Dir: dir + "/" + id}); err != nil {
			t.Fatal(err)
		}
	}
	err := loc.Start(ctx, StartRequest{ID: "sa_cccccccc", Task: "t", Mode: ModeBrief, Dir: dir + "/c"})
	if err == nil || !strings.Contains(err.Error(), "max concurrent") {
		t.Fatal(err)
	}
	close(block)
	got := 0
	deadline := time.Now().Add(2 * time.Second)
	for got < 2 && time.Now().Before(deadline) {
		select {
		case <-loc.Events():
			got++
		case <-time.After(20 * time.Millisecond):
		}
	}
	if got != 2 {
		t.Fatalf("jobs still running, events %d", got)
	}
}

type blockProv struct{ block chan struct{} }

func (p *blockProv) Chat(ctx context.Context, _ provider.Request, _ func(string)) (provider.Message, error) {
	select {
	case <-ctx.Done():
		return provider.Message{}, ctx.Err()
	case <-p.block:
		return provider.Message{Role: provider.RoleAssistant, Content: "done"}, nil
	}
}

func TestRunnerRejectsBadAuthAndRemote(t *testing.T) {
	loc := NewLocal(Limits{})
	h := (&Server{Local: loc, Token: "secret"}).Handler()
	req := httptest.NewRequest(http.MethodPost, "/v1/start", strings.NewReader(`{}`))
	req.RemoteAddr = "127.0.0.1:9"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodPost, "/v1/start", strings.NewReader(`{}`))
	req.RemoteAddr = "8.8.8.8:9"
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d", rec.Code)
	}
	if err := Listen("0.0.0.0:1", "secret", loc); err == nil {
		t.Fatal("expected non-loopback to be refused")
	}
}

func subFile(t *testing.T, dir string) string {
	t.Helper()
	entries, err := readEntries(dir)
	if err != nil {
		return err.Error()
	}
	var b strings.Builder
	for _, e := range entries {
		b.WriteString(e.Message.Role)
		b.WriteByte(' ')
		b.WriteString(e.Message.Content)
		b.WriteByte('\n')
	}
	meta, _ := readMeta(dir)
	b.WriteString(meta.Status)
	b.WriteByte(' ')
	b.WriteString(meta.Summary)
	return b.String()
}
