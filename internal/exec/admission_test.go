package exec

import (
	"context"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"xbot/internal/provider"
)

// Each job is individually released, so launch order and the concurrency cap
// are observed at the provider, not inferred from final metadata.
type admissionProvider struct {
	entered chan string
	release chan struct{}
}

func (p *admissionProvider) Chat(ctx context.Context, req provider.Request, _ func(string)) (provider.Message, error) {
	p.entered <- req.Messages[len(req.Messages)-1].Content
	select {
	case <-ctx.Done():
		return provider.Message{}, ctx.Err()
	case <-p.release:
	}
	return provider.Message{Role: provider.RoleAssistant, Content: "done"}, nil
}
func TestAdmissionQueueBoundAndFIFO(t *testing.T) {
	p := &admissionProvider{make(chan string, 16), make(chan struct{}, 16)}
	loc := NewLocal(Limits{MaxConcurrent: 1})
	loc.Prov = p
	defer func() {
		close(p.release)
		until := time.Now().Add(3 * time.Second)
		for taskCount(loc) > 0 && time.Now().Before(until) {
			time.Sleep(time.Millisecond)
		}
	}()
	root := t.TempDir()
	for i := 0; i <= maxWaiting; i++ {
		id := fmt.Sprintf("sa_%08d", i)
		if _, err := loc.Start(context.Background(), StartRequest{ID: id, Task: id, Mode: ModeBrief, Dir: filepath.Join(root, id)}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := loc.Start(context.Background(), StartRequest{ID: "sa_overflow", Task: "overflow", Mode: ModeBrief, Dir: filepath.Join(root, "overflow")}); err == nil {
		t.Fatal("queue overflow accepted")
	}
	if taskCount(loc) != 9 || loc.ActiveCount() != 1 {
		list, _ := loc.Tasks()
		t.Fatalf("tasks %+v, active %d", list, loc.ActiveCount())
	}
	for i := 0; i <= maxWaiting; i++ {
		select {
		case id := <-p.entered:
			if id != fmt.Sprintf("sa_%08d", i) {
				t.Fatalf("start %d = %s", i, id)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("next queued task never started")
		}
		if loc.ActiveCount() != 1 {
			t.Fatalf("concurrent = %d", loc.ActiveCount())
		}
		p.release <- struct{}{}
	}
	for i := 0; i <= maxWaiting; i++ {
		select {
		case <-loc.Events():
		case <-time.After(3 * time.Second):
			t.Fatal("missing completion")
		}
	}
}

func TestRemoteQueueSnapshot(t *testing.T) {
	block := make(chan struct{})
	loc := NewLocal(Limits{MaxConcurrent: 1})
	loc.Prov = &blockProv{block: block}
	srv := httptest.NewServer((&Server{Local: loc, Token: "test"}).Handler())
	defer srv.Close()
	remote := Dial(srv.URL, "test")
	defer remote.Close()
	defer func() {
		close(block)
		until := time.Now().Add(3 * time.Second)
		for taskCount(loc) > 0 && time.Now().Before(until) {
			time.Sleep(time.Millisecond)
		}
	}()
	root := t.TempDir()
	for _, id := range []string{"sa_aaaaaaaa", "sa_bbbbbbbb"} {
		if _, err := remote.Start(context.Background(), StartRequest{ID: id, Task: id, Mode: ModeBrief, Dir: filepath.Join(root, id)}); err != nil {
			t.Fatal(err)
		}
	}
	list, err := remote.Tasks()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[1].Status != "queued" || list[1].Position != 1 {
		t.Fatalf("remote tasks = %+v", list)
	}
}

func taskCount(e Executor) int { list, _ := e.Tasks(); return len(list) }
