package exec

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"xbot/internal/provider"
	"xbot/internal/tools"
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
	if _, err := loc.Start(ctx, StartRequest{ID: id, Task: "first", Mode: ModeBrief, Dir: dir}); err != nil {
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

func TestMaxConcurrentQueues(t *testing.T) {
	block := make(chan struct{})
	loc := NewLocal(Limits{MaxConcurrent: 2, Timeout: 10 * time.Second, ProgressEvery: -1})
	loc.Prov = &blockProv{block: block}
	ctx := context.Background()
	var release sync.Once
	defer func() {
		release.Do(func() { close(block) })
		until := time.Now().Add(3 * time.Second)
		for taskCount(loc) > 0 && time.Now().Before(until) {
			time.Sleep(time.Millisecond)
		}
	}()
	dir := t.TempDir()
	for _, id := range []string{"sa_aaaaaaaa", "sa_bbbbbbbb"} {
		if _, err := loc.Start(ctx, StartRequest{ID: id, Task: "t", Mode: ModeBrief, Dir: dir + "/" + id}); err != nil {
			t.Fatal(err)
		}
	}
	_, err := loc.Start(ctx, StartRequest{ID: "sa_cccccccc", Task: "t", Mode: ModeBrief, Dir: dir + "/c"})
	if err != nil {
		t.Fatal(err)
	}
	tasks, _ := loc.Tasks()
	if len(tasks) != 3 || tasks[2].ID != "sa_cccccccc" || tasks[2].Status != "queued" || tasks[2].Position != 1 {
		t.Fatalf("tasks = %+v", tasks)
	}
	meta, err := ReadMeta(dir + "/c")
	if err != nil || meta.Status != "queued" {
		t.Fatalf("queued record = %+v, %v", meta, err)
	}
	release.Do(func() { close(block) })
	got := 0
	deadline := time.Now().Add(2 * time.Second)
	for got < 3 && time.Now().Before(deadline) {
		select {
		case <-loc.Events():
			got++
		case <-time.After(20 * time.Millisecond):
		}
	}
	if got != 3 {
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

type roundImage struct{ n int }

func (roundImage) Name() string                                  { return "view_image" }
func (roundImage) Description() string                           { return "view" }
func (roundImage) Parameters() any                               { return map[string]any{"type": "object"} }
func (roundImage) Confirm(json.RawMessage) (bool, string, error) { return false, "", nil }
func (im *roundImage) Run(ctx context.Context, _ json.RawMessage) (string, error) {
	im.n++
	tools.AttachImage(ctx, tools.AttachedImage{MIME: "image/png", Data: []byte{byte(im.n)}})
	return "path: a.png", nil
}

type imageRounds struct {
	mu   sync.Mutex
	reqs [][]provider.Message
	n    int
	hold chan struct{}
}

func (p *imageRounds) Chat(_ context.Context, req provider.Request, _ func(string)) (provider.Message, error) {
	p.mu.Lock()
	n := p.n
	p.n++
	hold := p.hold
	p.mu.Unlock()
	if n == 0 && hold != nil {
		<-hold
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	snap := append([]provider.Message(nil), req.Messages...)
	for i := range snap {
		if len(snap[i].Images) == 0 {
			continue
		}
		snap[i].Images = append([]provider.Image(nil), snap[i].Images...)
		for j := range snap[i].Images {
			snap[i].Images[j].Data = append([]byte(nil), snap[i].Images[j].Data...)
		}
	}
	p.reqs = append(p.reqs, snap)
	if n%2 == 0 {
		return provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "c", Type: "function", Function: provider.FunctionCall{Name: "view_image", Arguments: "{}"},
		}}}, nil
	}
	return provider.Message{Role: provider.RoleAssistant, Content: "saw"}, nil
}

func TestSubagentImageRoundDropsAfterReply(t *testing.T) {
	hold := make(chan struct{})
	p := &imageRounds{hold: hold}
	tool := &roundImage{}
	loc := NewLocal(Limits{Timeout: 10 * time.Second, ProgressEvery: -1, MaxSteps: 12})
	loc.Prov = p
	loc.ToolSet = func(string, string) []tools.Tool { return []tools.Tool{tool} }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		for range loc.Events() {
		}
	}()
	dir := t.TempDir()
	id := "sa_imground1"
	if _, err := loc.Start(ctx, StartRequest{ID: id, Task: "look", Mode: ModeBrief, Dir: dir}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if err := loc.Control(ctx, id, Control{Kind: KindQueue, Text: "again"}); err != nil {
			t.Fatal(err)
		}
	}
	close(hold)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		meta, _, err := ReadStatus(dir)
		if err == nil && meta.Status == statusDone {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	meta, _, err := ReadStatus(dir)
	if err != nil || meta.Status != statusDone {
		t.Fatalf("status %+v %v", meta, err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.reqs) < 10 || tool.n != 5 {
		t.Fatalf("requests %d views %d", len(p.reqs), tool.n)
	}
	var first []byte
	for _, m := range p.reqs[1] {
		for _, img := range m.Images {
			first = append(first, img.Data...)
		}
	}
	if len(first) != 1 || first[0] != 1 {
		t.Fatalf("reply that consumed round 1 carried %v", first)
	}
	for _, m := range p.reqs[2] {
		if len(m.Images) != 0 {
			t.Fatal("round 1 image was still in the request after that reply")
		}
	}
	var fifth int
	var sawOld bool
	for _, m := range p.reqs[9] {
		for _, img := range m.Images {
			fifth++
			if len(img.Data) == 1 && img.Data[0] == 1 {
				sawOld = true
			}
			if len(img.Data) != 1 || img.Data[0] != 5 {
				t.Fatalf("5th image %v", img.Data)
			}
		}
	}
	if fifth != 1 || sawOld {
		t.Fatalf("5th request images %d old %v", fifth, sawOld)
	}
}

type streakRounds struct {
	mu   sync.Mutex
	reqs [][]provider.Message
	n    int
}

func (p *streakRounds) Chat(_ context.Context, req provider.Request, _ func(string)) (provider.Message, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := p.n
	p.n++
	snap := append([]provider.Message(nil), req.Messages...)
	for i := range snap {
		if len(snap[i].Images) == 0 {
			continue
		}
		snap[i].Images = append([]provider.Image(nil), snap[i].Images...)
		for j := range snap[i].Images {
			snap[i].Images[j].Data = append([]byte(nil), snap[i].Images[j].Data...)
		}
	}
	p.reqs = append(p.reqs, snap)
	if n < 5 {
		return provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "c", Type: "function", Function: provider.FunctionCall{Name: "view_image", Arguments: "{}"},
		}}}, nil
	}
	return provider.Message{Role: provider.RoleAssistant, Content: "saw five"}, nil
}

func TestSubagentImageStreakKeepsNewest(t *testing.T) {
	p := &streakRounds{}
	tool := &roundImage{}
	loc := NewLocal(Limits{Timeout: 10 * time.Second, ProgressEvery: -1, MaxSteps: 12})
	loc.Prov = p
	loc.ToolSet = func(string, string) []tools.Tool { return []tools.Tool{tool} }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		for range loc.Events() {
		}
	}()
	dir := t.TempDir()
	id := "sa_imgstreak"
	if _, err := loc.Start(ctx, StartRequest{ID: id, Task: "look", Mode: ModeBrief, Dir: dir}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		meta, _, err := ReadStatus(dir)
		if err == nil && meta.Status == statusDone {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	meta, _, err := ReadStatus(dir)
	if err != nil || meta.Status != statusDone {
		t.Fatalf("status %+v %v", meta, err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.reqs) != 6 || tool.n != 5 {
		t.Fatalf("requests %d views %d", len(p.reqs), tool.n)
	}
	var got []byte
	for _, m := range p.reqs[5] {
		for _, img := range m.Images {
			got = append(got, img.Data...)
		}
	}
	if len(got) != 1 || got[0] != 5 {
		t.Fatalf("6th request carried %v", got)
	}
	for _, msgs := range p.reqs {
		for _, m := range msgs {
			if strings.Contains(m.Content, "image omitted: turn image cap") {
				t.Fatalf("%s: %s", m.Role, m.Content)
			}
		}
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
	meta, _ := ReadMeta(dir)
	b.WriteString(meta.Status)
	b.WriteByte(' ')
	b.WriteString(meta.Summary)
	return b.String()
}

// Two subagents writing the same resource never run at once: the later one
// waits, queued, and starts when the earlier ends. One with no resource (a
// research subagent) runs beside them. A queued one can be cancelled.
func TestSameResourceQueues(t *testing.T) {
	block := make(chan struct{})
	loc := NewLocal(Limits{MaxConcurrent: 3, Timeout: 10 * time.Second, ProgressEvery: -1})
	loc.Prov = &blockProv{block: block}
	ctx := context.Background()
	dir := t.TempDir()
	start := func(id, res string) {
		t.Helper()
		if _, err := loc.Start(ctx, StartRequest{ID: id, Task: "t", Mode: ModeBrief, Dir: dir + "/" + id, Resource: res}); err != nil {
			t.Fatal(err)
		}
	}
	status := func(id string) string {
		meta, _, _ := ReadStatus(dir + "/" + id)
		return meta.Status
	}
	start("sa_dev00001", "repo:/x")
	start("sa_res00001", "")
	start("sa_dev00002", "repo:/x")
	start("sa_dev00003", "repo:/x")
	deadline := time.Now().Add(2 * time.Second)
	for status("sa_res00001") != statusRunning && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if s := status("sa_res00001"); s != statusRunning {
		t.Fatalf("research waited: %s", s)
	}
	if s := status("sa_dev00002"); s != statusQueued {
		t.Fatalf("second dev on the same repo is %s, want queued", s)
	}
	if err := loc.Control(ctx, "sa_dev00003", Control{Kind: KindCancel}); err != nil {
		t.Fatal(err)
	}
	if s := status("sa_dev00003"); s != statusCancelled {
		t.Fatalf("cancelled queued job is %s", s)
	}
	close(block)
	var order []string
	deadline = time.Now().Add(5 * time.Second)
	for len(order) < 4 && time.Now().Before(deadline) {
		select {
		case ev := <-loc.Events():
			order = append(order, ev.AgentID+":"+ev.Kind)
		case <-time.After(20 * time.Millisecond):
		}
	}
	joined := strings.Join(order, " ")
	first, second := strings.Index(joined, "sa_dev00001:done"), strings.Index(joined, "sa_dev00002:done")
	if first < 0 || second < 0 || second < first || !strings.Contains(joined, "sa_dev00003:failed") {
		t.Fatalf("events %v", order)
	}
}
