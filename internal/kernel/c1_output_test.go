package kernel

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"xbot/internal/inbox"
	"xbot/internal/link"
	"xbot/internal/provider"
	"xbot/internal/tools"
	"xbot/internal/work"
)

type c1Rich struct {
	*c1Slow
	path string
}

func (*c1Rich) Name() string { return "c1_rich" }
func (s *c1Rich) Run(ctx context.Context, _ json.RawMessage) (string, error) {
	if _, err := s.c1Slow.Run(ctx, nil); err != nil {
		return "", err
	}
	raw, _ := json.Marshal(map[string]string{"path": s.path, "content": "one completed write"})
	if _, err := (&tools.WriteFile{}).Run(ctx, raw); err != nil {
		return "", err
	}
	var img bytes.Buffer
	if err := png.Encode(&img, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		return "", err
	}
	tools.AttachImage(ctx, tools.AttachedImage{MIME: "image/png", Data: img.Bytes()})
	return strings.Repeat("full result facts\n", 700) + "FINAL_FACT_91733", nil
}

func TestC1BackgroundOutputImagesAndEffectsUseActualCompletion(t *testing.T) {
	slow := &c1Rich{c1Slow: &c1Slow{entered: make(chan struct{}), release: make(chan struct{})}, path: filepath.Join(t.TempDir(), "artifact.txt")}
	k, p := c1Fixture(t, slow)
	var once sync.Once
	defer func() { once.Do(func() { close(slow.release) }); _ = k.Close() }()
	p.resps = []provider.Message{toolCall("rich", slow.Name(), "{}"), {Role: provider.RoleAssistant, Content: "已转到后台"}}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "rich", time.Now())); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(slow.path); !os.IsNotExist(err) {
		t.Fatal("effect committed at receipt instead of completion")
	}
	if len(k.turn.notable.Items()) != 0 {
		t.Fatal("receipt committed effects")
	}
	once.Do(func() { close(slow.release) })
	end := time.Now().Add(time.Second)
	for len(k.Inbox.Pending()) == 0 && time.Now().Before(end) {
		time.Sleep(time.Millisecond)
	}
	if len(k.Inbox.Pending()) != 1 {
		t.Fatalf("events=%+v", k.Inbox.Pending())
	}
	ev := k.Inbox.Pending()[0]
	if len(ev.Images) != 1 {
		t.Fatalf("image missing: %+v", ev)
	}
	img, err := work.ReadPasted(k.workspace(), ev.Images[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := png.Decode(bytes.NewReader(img)); err != nil {
		t.Fatal(err)
	}
	jobs := k.shellJobs().List()
	if len(jobs) != 1 {
		t.Fatalf("jobs=%+v", jobs)
	}
	job := jobs[0]
	if len(job.Notable) != 1 || job.Notable[0].Kind != "write_outside" {
		t.Fatalf("facts=%+v", job.Notable)
	}
	if !strings.Contains(ev.Content, "值得留意：") || !strings.Contains(ev.Content, job.Output) {
		t.Fatalf("report missing facts/path: %s", ev.Content)
	}
	if strings.Contains(ev.Content, "FINAL_FACT_91733") {
		t.Fatal("long result was not previewed")
	}
	text, err := os.ReadFile(job.Output)
	if err != nil || !strings.Contains(string(text), "FINAL_FACT_91733") {
		t.Fatalf("full result lost: %v", err)
	}
	reader := &tools.ReadFile{}
	k.bindFileCwd(reader)
	args, _ := json.Marshal(map[string]any{"path": job.Output, "offset": 695, "limit": 10})
	read, err := reader.Run(context.Background(), args)
	if err != nil || !strings.Contains(read, "FINAL_FACT_91733") {
		t.Fatalf("reported path is not readable through read_file: %q %v", read, err)
	}
}

func TestC1LostToolJobReportsOnceAfterRestart(t *testing.T) {
	k, _, _ := freshBot(t)
	in, err := inbox.Open(k.Bot.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	k.Inbox = in
	dir := k.shellJobs().Dir
	id := "sh_9876abcd"
	raw, _ := json.Marshal(tools.Job{ID: id, Tool: "web_fetch", Status: "running", Background: true, Output: filepath.Join(dir, id+".log"), Started: time.Now()})
	if err := os.WriteFile(filepath.Join(dir, id+".json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	// Reconstruct the actual kernel owner rather than creating another seen ledger.
	fresh := &Kernel{Bot: k.Bot, Inbox: k.Inbox, WorkDir: k.WorkDir}
	if fresh.Inbox == nil {
		t.Fatal("fixture requires inbox")
	}
	if err := fresh.RecoverTasks(); err != nil {
		t.Fatal(err)
	}
	if len(fresh.Inbox.Pending()) != 1 || !strings.Contains(fresh.Inbox.Pending()[0].Content, "lost") {
		t.Fatalf("missing lost report: %+v", fresh.Inbox.Pending())
	}
	again := &Kernel{Bot: k.Bot, Inbox: k.Inbox, WorkDir: k.WorkDir}
	if err := again.RecoverTasks(); err != nil {
		t.Fatal(err)
	}
	if len(again.Inbox.Pending()) != 1 {
		t.Fatalf("duplicate lost report: %+v", again.Inbox.Pending())
	}
}

func TestC1JobControlsWorkAtCapacity(t *testing.T) {
	k, _, _ := freshBot(t)
	defer k.Close()
	jobs := k.shellJobs()
	var first string
	for i := 0; i < 32; i++ {
		r := jobs.RunCall(context.Background(), tools.CallRequest{Name: "held", Background: true, Run: func(ctx context.Context) tools.CallResult { <-ctx.Done(); return tools.CallResult{Err: ctx.Err()} }})
		if r.Err != nil || r.Job == nil {
			t.Fatalf("admission %d: %+v", i, r)
		}
		if first == "" {
			first = r.Job.ID
		}
	}
	args, _ := json.Marshal(map[string]string{"id": first})
	for _, tool := range []tools.Tool{shellStatusTool{k}, shellOutputTool{k}, shellCancelTool{k}} {
		tool := reviewedTool{Tool: tool, k: k}
		r := k.runToolCall(context.Background(), tool, args, func(ctx context.Context) tools.CallResult {
			out, err := tool.Run(ctx, args)
			return tools.CallResult{Text: out, Err: err}
		})
		if r.Err != nil || r.Job != nil {
			t.Fatalf("full-capacity %s: %+v", tool.Name(), r)
		}
	}
	job, err := jobs.Status(first)
	if err != nil || job.Status != "cancelled" {
		t.Fatalf("not actually cancelled: %+v %v", job, err)
	}
}

func TestC1ForegroundCompletionFollowsItsOriginalFork(t *testing.T) {
	k, _, _ := freshBot(t)
	k.WorkDir = t.TempDir()
	sub := filepath.Join(k.WorkDir, "child")
	if err := os.Mkdir(sub, 0700); err != nil {
		t.Fatal(err)
	}
	k.storeCwd(k.WorkDir)
	owner := k.ensureTurn()
	ctx := context.WithValue(context.Background(), turnKey{}, owner)
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan tools.CallResult, 1)
	go func() {
		done <- k.runToolCall(ctx, &c1Slow{}, nil, func(ctx context.Context) tools.CallResult {
			close(entered)
			<-release
			k.shellAfterDir(ctx, sub)
			return tools.CallResult{Text: "done"}
		})
	}()
	<-entered
	k.adoptForeground("bg_original")
	owner.mu.Lock()
	owner.branch = "bg_original"
	owner.mu.Unlock()
	close(release)
	r := <-done
	defer k.Close()
	if r.Err != nil || r.Job != nil {
		t.Fatal(r)
	}
	if got := k.readScope(""); got != k.WorkDir {
		t.Fatalf("old branch moved foreground cwd: %s", got)
	}
	if got := k.readScope("bg_original"); got != sub {
		t.Fatalf("branch cwd=%s", got)
	}
}
