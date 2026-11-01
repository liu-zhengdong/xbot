package exec

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

type continueProvider struct {
	call func(context.Context, provider.Request) (provider.Message, error)
}

func (p continueProvider) Chat(ctx context.Context, r provider.Request, _ func(string)) (provider.Message, error) {
	return p.call(ctx, r)
}
func answer(text string) provider.Message {
	return provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent(text)}
}
func persistentLocal(root string, p provider.Provider) *Local {
	l := NewLocal(Limits{MaxConcurrent: 1, MaxSteps: 2, Timeout: 5 * time.Second})
	l.AgentsDir = root
	l.Prov = p
	l.Restore = func(_ context.Context, d Descriptor) (StartRequest, error) {
		return StartRequest{Resource: d.CWD, Workspace: d.CWD}, nil
	}
	return l
}
func persistentStart(t *testing.T, l *Local, id, task string) Task {
	t.Helper()
	d := Descriptor{Version: 1, ID: id, Bot: "test", Type: "dev", Mode: ModeBrief, CWD: "repo", MaxSteps: 2, TimeoutSec: 5, Tools: []string{"read_file"}}
	a, err := l.Start(context.Background(), StartRequest{ID: id, Bot: "test", Task: task, Mode: ModeBrief, Dir: filepath.Join(l.AgentsDir, id), Resource: d.CWD, Descriptor: &d})
	if err != nil {
		t.Fatal(err)
	}
	return a
}
func terminal(t *testing.T, l *Local) Event {
	t.Helper()
	for {
		select {
		case e := <-l.Events():
			if e.Kind == EvDone || e.Kind == EvFailed {
				return e
			}
		case <-time.After(3 * time.Second):
			t.Fatal("missing terminal")
		}
	}
}
func eventResult(t *testing.T, root string, e Event) string {
	t.Helper()
	path := filepath.Join(root, e.AgentID, "results", fmt.Sprintf("%d.txt", e.TurnSeq))
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.Text, path) {
		t.Fatalf("event lost immutable reference: %+v", e)
	}
	return string(b)
}

func TestContinuableColdResumeKeepsHistoryAndResult(t *testing.T) {
	root := t.TempDir()
	var requests []provider.Request
	p := continueProvider{func(_ context.Context, r provider.Request) (provider.Message, error) {
		requests = append(requests, r)
		return answer(fmt.Sprintf("result-%d", len(requests))), nil
	}}
	l := persistentLocal(root, p)
	first := persistentStart(t, l, "sa_continue01", "FIRST_FACT")
	e1 := terminal(t, l)
	if e1.ID != TerminalEventID(first.ID, first.TurnSeq) || eventResult(t, root, e1) != "result-1" {
		t.Fatal(e1)
	}
	if _, ok := ActiveTask(filepath.Join(root, first.ID)); ok {
		t.Fatal("idle child retained admission")
	}
	l2 := persistentLocal(root, p)
	var restored Descriptor
	l2.Restore = func(_ context.Context, d Descriptor) (StartRequest, error) { restored = d; return StartRequest{}, nil }
	second, err := l2.Message(context.Background(), first.ID, Control{Kind: KindSteer, Text: "SECOND_FACT", Sender: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	e2 := terminal(t, l2)
	if second.ID != first.ID || e2.TurnSeq <= e1.TurnSeq || e2.ID == e1.ID {
		t.Fatalf("bad turn identity %+v %+v", e1, e2)
	}
	if eventResult(t, root, e1) != "result-1" || eventResult(t, root, e2) != "result-2" {
		t.Fatal("result overwritten")
	}
	if restored.Tools[0] != "read_file" {
		t.Fatal("policy snapshot lost")
	}
	var all strings.Builder
	for _, m := range requests[1].Messages {
		all.WriteString(m.Text())
	}
	if !strings.Contains(all.String(), "FIRST_FACT") || !strings.Contains(all.String(), "result-1") || !strings.Contains(all.String(), "来自用户的消息：\nSECOND_FACT") {
		t.Fatal(all.String())
	}
	entries, err := readRecords(filepath.Join(root, first.ID))
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, e := range entries {
		if strings.Contains(e.Message.Text(), "SECOND_FACT") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("input stored %d times", count)
	}
}

func TestContinuableQueueReentersAdmissionAndHasOwnBudget(t *testing.T) {
	root := t.TempDir()
	entered := make(chan string, 4)
	release := make(chan struct{}, 4)
	p := continueProvider{func(ctx context.Context, r provider.Request) (provider.Message, error) {
		text := r.Messages[len(r.Messages)-1].Text()
		entered <- text
		select {
		case <-release:
			return answer(text), nil
		case <-ctx.Done():
			return provider.Message{}, ctx.Err()
		}
	}}
	l := persistentLocal(root, p)
	l.Limits.MaxSteps = 1
	persistentStart(t, l, "sa_queue0001", "A")
	<-entered
	if _, err := l.Message(context.Background(), "sa_queue0001", Control{Kind: KindQueue, Text: "A_NEXT"}); err != nil {
		t.Fatal(err)
	}
	b := persistentStart(t, l, "sa_queue0002", "B")
	if b.Status != statusQueued {
		t.Fatal(b)
	}
	release <- struct{}{}
	e1 := terminal(t, l)
	if got := <-entered; !strings.HasSuffix(got, "B") {
		t.Fatalf("new turn jumped older waiter: %q", got)
	}
	release <- struct{}{}
	terminal(t, l)
	if got := <-entered; !strings.HasSuffix(got, "A_NEXT") {
		t.Fatal(got)
	}
	release <- struct{}{}
	e3 := terminal(t, l)
	if e1.AgentID != e3.AgentID || e1.TurnSeq == e3.TurnSeq || e3.Kind != EvDone {
		t.Fatalf("queue shared a turn/budget %+v %+v", e1, e3)
	}
}

func TestContinuableClosingMessageRoutesOriginalOwner(t *testing.T) {
	root := t.TempDir()
	closing := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	p := continueProvider{func(_ context.Context, r provider.Request) (provider.Message, error) {
		return answer(r.Messages[len(r.Messages)-1].Text()), nil
	}}
	l := persistentLocal(root, p)
	l.CurrentCWD = func(StartRequest) string { once.Do(func() { close(closing); <-release }); return "saved-cwd" }
	persistentStart(t, l, "sa_closing01", "FIRST")
	<-closing
	other := persistentLocal(root, continueProvider{func(context.Context, provider.Request) (provider.Message, error) {
		t.Error("wrong owner provider")
		return answer("wrong"), nil
	}})
	if _, err := other.Message(context.Background(), "sa_closing01", Control{Kind: KindSteer, Text: "DURING_CLOSE"}); err != nil {
		t.Fatal(err)
	}
	if a, ok := ActiveTask(filepath.Join(root, "sa_closing01")); !ok || !a.Continuable {
		t.Fatal(a, ok)
	}
	close(release)
	terminal(t, l)
	e2 := terminal(t, l)
	if !strings.Contains(eventResult(t, root, e2), "DURING_CLOSE") {
		t.Fatal(e2)
	}
	d, err := ReadDescriptor(filepath.Join(root, "sa_closing01"))
	if err != nil || d.CWD != "saved-cwd" {
		t.Fatal(d, err)
	}
}

func TestContinuableStopDropsAcceptedQueueAndExplicitlyResumes(t *testing.T) {
	root := t.TempDir()
	entered := make(chan struct{})
	p := continueProvider{func(ctx context.Context, r provider.Request) (provider.Message, error) {
		if strings.HasSuffix(r.Messages[len(r.Messages)-1].Text(), "FIRST") {
			close(entered)
			<-ctx.Done()
			return provider.Message{}, ctx.Err()
		}
		return answer("RESUMED"), nil
	}}
	l := persistentLocal(root, p)
	persistentStart(t, l, "sa_stop00001", "FIRST")
	<-entered
	if _, err := l.Message(context.Background(), "sa_stop00001", Control{Kind: KindQueue, Text: "MUST_DROP"}); err != nil {
		t.Fatal(err)
	}
	stop, err := l.Stop(context.Background(), "sa_stop00001")
	if err != nil || stop.Status != "stopping" {
		t.Fatal(stop, err)
	}
	terminal(t, l)
	if _, err := l.Message(context.Background(), "sa_stop00001", Control{Text: "EXPLICIT_NEW"}); err != nil {
		t.Fatal(err)
	}
	if e := terminal(t, l); e.Kind != EvDone {
		t.Fatal(e)
	}
	entries, _ := readRecords(filepath.Join(root, "sa_stop00001"))
	received := 0
	dropped := 0
	for _, e := range entries {
		if e.Kind == "message_received" {
			received++
		}
		if e.Kind == "message_dropped" {
			dropped++
		}
	}
	if received != 3 || dropped != 1 {
		t.Fatalf("received %d dropped %d", received, dropped)
	}
	if taskCount(l) != 0 {
		t.Fatal("idle child holds slot")
	}
}

func TestContinuablePendingBoundAndAdmissionReservation(t *testing.T) {
	root := t.TempDir()
	entered := make(chan struct{})
	release := make(chan struct{})
	p := continueProvider{func(ctx context.Context, _ provider.Request) (provider.Message, error) {
		select {
		case entered <- struct{}{}:
		default:
		}
		select {
		case <-ctx.Done():
			return provider.Message{}, ctx.Err()
		case <-release:
			return answer("done"), nil
		}
	}}
	l := persistentLocal(root, p)
	persistentStart(t, l, "sa_capacity1", "first")
	<-entered
	for i := 0; i < 16; i++ {
		if _, err := l.Message(context.Background(), "sa_capacity1", Control{Kind: KindQueue, Text: fmt.Sprint(i)}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := l.Message(context.Background(), "sa_capacity1", Control{Kind: KindQueue, Text: "overflow"}); !errors.Is(err, ErrQueueFull) {
		t.Fatal(err)
	}
	for i := 0; i < 7; i++ {
		persistentStart(t, l, fmt.Sprintf("sa_wait%04d", i), "waiting")
	}
	d := Descriptor{Version: 1, ID: "sa_overflow1", Bot: "test", Mode: ModeBrief}
	if _, err := l.Start(context.Background(), StartRequest{ID: d.ID, Bot: d.Bot, Mode: ModeBrief, Task: "overflow", Dir: filepath.Join(root, d.ID), Descriptor: &d}); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("reservation allowed ninth waiter: %v", err)
	}
	// Stop the owner drops all 16 accepted followups before releasing the provider.
	if _, err := l.Stop(context.Background(), "sa_capacity1"); err != nil {
		t.Fatal(err)
	}
	close(release)
	for i := 0; i < 8; i++ {
		terminal(t, l)
	}
}

func TestContinuableRestartSealsCallsAndDropsOldInputs(t *testing.T) {
	root := t.TempDir()
	id := "sa_restart01"
	dir := filepath.Join(root, id)
	d := Descriptor{Version: 1, ID: id, Bot: "test", Mode: ModeBrief, MaxSteps: 2}
	if err := writeDescriptor(dir, d); err != nil {
		t.Fatal(err)
	}
	j := &job{dir: dir, id: id, turnSeq: 1}
	seq, err := j.receive(Control{Kind: KindSteer, Text: "OLD_INPUT"})
	if err != nil {
		t.Fatal(err)
	}
	if err := j.markInput("message_consumed", pendingInput{seq: seq}); err != nil {
		t.Fatal(err)
	}
	_, err = j.record(SeqEntry{Entry: bot.Entry{Message: provider.Message{Role: provider.RoleAssistant, Content: provider.ToolContent([]provider.ToolCall{{ID: "unfinished", Function: provider.FunctionCall{Name: "write_file", Arguments: "{}"}}})}}})
	if err != nil {
		t.Fatal(err)
	}
	j.receive(Control{Kind: KindQueue, Text: "OLD_QUEUED"})
	if err := writeMeta(dir, Meta{ID: id, Status: statusRunning, TurnSeq: 1}); err != nil {
		t.Fatal(err)
	}
	m, err := Interrupt(dir)
	if err != nil || m.Status != bot.ForkInterrupted || m.ResultPath == "" {
		t.Fatal(m, err)
	}
	before, _ := readRecords(dir)
	if _, err := Interrupt(dir); err != nil {
		t.Fatal(err)
	}
	after, _ := readRecords(dir)
	if len(after) != len(before) {
		t.Fatal("recovery appended twice")
	}
	p := continueProvider{func(_ context.Context, r provider.Request) (provider.Message, error) {
		var calls, results int
		for _, m := range r.Messages {
			calls += len(m.Calls())
			if m.Role == provider.RoleTool {
				results++
			}
			if strings.Contains(m.Text(), "OLD_QUEUED") {
				t.Error("replayed old queued input")
			}
		}
		if calls != 1 || results != 1 {
			t.Errorf("unpaired history: %d/%d", calls, results)
		}
		return answer("new"), nil
	}}
	l := persistentLocal(root, p)
	if _, err := l.Message(context.Background(), id, Control{Text: "NEW"}); err != nil {
		t.Fatal(err)
	}
	terminal(t, l)
}

func TestContinuableParentMessageUsesChildContextAndSequence(t *testing.T) {
	root := t.TempDir()
	l := persistentLocal(root, nil)
	l.Prov = continueProvider{func(ctx context.Context, _ provider.Request) (provider.Message, error) {
		e, err := l.SendParent(ctx, "sa_parent001", "KEY_DISCOVERY")
		if err != nil {
			t.Error(err)
		}
		if e.Kind != "message" || !strings.HasPrefix(e.ID, "subagent:sa_parent001:message:") {
			t.Error(e)
		}
		return answer("done"), nil
	}}
	if _, err := l.SendParent(context.Background(), "sa_parent001", "invalid"); err == nil {
		t.Fatal("arbitrary context may impersonate child")
	}
	persistentStart(t, l, "sa_parent001", "first")
	select {
	case e := <-l.Events():
		if e.Kind != "message" {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("no message")
	}
	terminal(t, l)
}

func TestContinuableRejectsLiveProcessAndSymlinkAlias(t *testing.T) {
	root := t.TempDir()
	id := "sa_external1"
	dir := filepath.Join(root, id)
	writeDescriptor(dir, Descriptor{Version: 1, ID: id, Mode: ModeBrief})
	pid, start := tools.CurrentProcess()
	writeMeta(dir, Meta{ID: id, Status: statusRunning, PID: pid, ProcStart: start})
	l := persistentLocal(root, continueProvider{func(context.Context, provider.Request) (provider.Message, error) {
		t.Error("copied running task")
		return answer("bad"), nil
	}})
	if _, err := l.Message(context.Background(), id, Control{Text: "new"}); !errors.Is(err, ErrActiveElsewhere) {
		t.Fatal(err)
	}
	if _, err := l.Stop(context.Background(), id); !errors.Is(err, ErrActiveElsewhere) {
		t.Fatal(err)
	}
	alias := "sa_alias0001"
	if err := os.Symlink(dir, filepath.Join(root, alias)); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Message(context.Background(), alias, Control{Text: "new"}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}
