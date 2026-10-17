package kernel

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"

	"xbot/internal/link"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

type writeTool struct{ ran int }

func (*writeTool) Name() string        { return "write_file" }
func (*writeTool) Description() string { return "write" }
func (*writeTool) Parameters() any     { return map[string]any{"type": "object"} }
func (*writeTool) Confirm(json.RawMessage) (bool, string, error) {
	return false, "a.txt", nil
}
func (w *writeTool) Run(context.Context, json.RawMessage) (string, error) {
	w.ran++
	return "ok", nil
}

// backgroundWait bounds a test that is waiting for a background to fork or to
// write its fork_end event. Past this, the helper fails instead of sitting
// until the process timeout.
const backgroundWait = 30 * time.Second

// waitNoBackgrounds waits until each background has its fork_end event in the
// inbox. An empty id list watches whatever is running now (or already finished).
// "No backgrounds left" is not the signal: finishBackground drops the turn
// from the running set before it posts the event, so a helper that returns on
// an empty list lets the caller read the inbox one step too early.
func waitNoBackgrounds(t *testing.T, k *Kernel, ids ...string) {
	t.Helper()
	if len(ids) == 0 {
		ids = watchBackgroundIDs(t, k)
	}
	if len(ids) == 0 {
		t.Fatal("no background to wait for")
	}
	for _, id := range ids {
		waitForkEnd(t, k, id)
	}
}

// watchBackgroundIDs collects background ids until none are running. If the
// turn already finished before the first look, the ids come from the finished
// list, which is updated in the same step that clears the running set and
// before the inbox post.
func watchBackgroundIDs(t *testing.T, k *Kernel) []string {
	t.Helper()
	deadline := time.Now().Add(backgroundWait)
	seen := map[string]struct{}{}
	for {
		running := k.Backgrounds()
		for _, bg := range running {
			seen[bg.ID] = struct{}{}
		}
		if len(running) == 0 {
			if len(seen) == 0 {
				for _, bg := range k.FinishedBackgrounds() {
					seen[bg.ID] = struct{}{}
				}
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("background still running after %s: %s", backgroundWait, joinIDs(seen))
		}
		time.Sleep(10 * time.Millisecond)
	}
	return joinIDSlice(seen)
}

// waitForkEnd waits until the inbox log contains exactly one fork_end event
// for id. Reload then synchronizes with the post, so later reads of the turn
// see the writes that happened before the event.
func waitForkEnd(t *testing.T, k *Kernel, id string) link.Event {
	t.Helper()
	if id == "" {
		t.Fatal("fork_end wait missing background id")
	}
	deadline := time.Now().Add(backgroundWait)
	want := "ev_" + id + "_end"
	for {
		posted := inboxPosts(t, k.Bot.InboxPath(), want)
		switch len(posted) {
		case 1:
			if k.Inbox != nil {
				if err := k.Inbox.Reload(); err != nil {
					t.Fatalf("reload inbox after %s: %v", want, err)
				}
			}
			return posted[0]
		case 0:
		default:
			t.Fatalf("fork_end events %d for %s", len(posted), id)
		}
		if time.Now().After(deadline) {
			t.Fatalf("fork_end event %s not written within %s (running backgrounds: %d)", want, backgroundWait, len(k.Backgrounds()))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func joinIDs(seen map[string]struct{}) string {
	return strings.Join(joinIDSlice(seen), ", ")
}

func joinIDSlice(seen map[string]struct{}) []string {
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func TestBackgroundSteerAppliedAfterStep(t *testing.T) {
	k, g, box := forkKernel(t)
	ev := link.TerminalEvent("u1", "做个长任务", time.Unix(10, 0).UTC())
	_ = box.Post(ev)
	if err := k.Handle(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	id := k.Backgrounds()[0].ID
	if _, err := k.ControlSubagent(context.Background(), id, "steer", "用中文写"); err != nil {
		t.Fatal(err)
	}
	cs := k.Controls()
	if len(cs) != 1 || cs[0].State != "pending" || cs[0].Background != id {
		t.Fatalf("controls %+v", cs)
	}
	close(g.release)
	waitNoBackgrounds(t, k, id)
	p := k.Provider.(*scripted)
	last := p.reqs[len(p.reqs)-1].Messages
	var steered bool
	for _, m := range last {
		steered = steered || (m.Role == provider.RoleUser && m.Content == "[主人的补充] 用中文写")
	}
	if !steered {
		t.Fatal("steer was not given to the next model step")
	}
	if _, err := k.ControlSubagent(context.Background(), id, "steer", "晚了"); err == nil {
		t.Fatal("control on a finished background should fail")
	}
}

func TestBackgroundQueueCapPositionAndCancel(t *testing.T) {
	k, _, _ := forkKernel(t)
	k.bgs = map[string]*turn{"bg_a": {branch: "bg_a"}}
	for i := 0; i < QueueCap; i++ {
		if _, err := k.ControlSubagent(context.Background(), "bg_a", "queue", "活"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := k.ControlSubagent(context.Background(), "bg_a", "queue", "多一个"); err == nil || !strings.Contains(err.Error(), "队列已满") {
		t.Fatalf("sixth queue item: %v", err)
	}
	q := k.Queue()
	if len(q) != QueueCap || q[0].Position != 1 || q[4].Position != 5 || q[0].Reason != "queue" || q[0].Background != "bg_a" {
		t.Fatalf("queue %+v", q)
	}
	if _, err := k.ControlSubagent(context.Background(), "bg_", "cancel_queued", q[2].ID); err != nil {
		t.Fatal(err)
	}
	if n := len(k.Queue()); n != QueueCap-1 {
		t.Fatalf("queue after cancel %d", n)
	}
}

func TestForegroundWriteQueuesBehindConflictingBackground(t *testing.T) {
	k, _, _ := forkKernel(t)
	k.WorkDir = "/w"
	bg := &turn{branch: "bg_a"}
	bg.addResource("file:/w/a.txt")
	k.bgs = map[string]*turn{"bg_a": bg}
	fg := &turn{}
	ctx := context.WithValue(context.Background(), turnKey{}, fg)
	w := &writeTool{}
	rt := reviewedTool{Tool: w, k: k}
	out, err := rt.Run(ctx, json.RawMessage(`{"path":"a.txt"}`))
	if err != nil || w.ran != 0 || !strings.Contains(out, "bg_a") {
		t.Fatalf("out %q ran %d err %v", out, w.ran, err)
	}
	q := k.Queue()
	if len(q) != 1 || q[0].Reason != "conflict" || q[0].Resource != "file:/w/a.txt" || q[0].Background != "bg_a" {
		t.Fatalf("queue %+v", q)
	}
	// A different file runs in parallel.
	if out, _ := rt.Run(ctx, json.RawMessage(`{"path":"b.txt"}`)); out != "ok" || w.ran != 1 {
		t.Fatalf("parallel write %q", out)
	}
	// Queue full: refused, nothing queued, nothing run.
	for i := 0; i < QueueCap; i++ {
		bg.queueCtrl(&Control{ID: "z", Kind: "queue"})
	}
	out, _ = rt.Run(ctx, json.RawMessage(`{"path":"a.txt"}`))
	if !strings.Contains(out, "队列已满") || w.ran != 1 {
		t.Fatalf("full queue %q", out)
	}
}

func TestBackgroundConfirmationGoesToOwner(t *testing.T) {
	k, g, box := forkKernel(t)
	w := &confirmWrite{}
	k.Links = append(k.Links, gateLinkOf(w))
	k.Provider = &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c1", Type: "function", Function: provider.FunctionCall{Name: "gate", Arguments: "{}"}}}},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c2", Type: "function", Function: provider.FunctionCall{Name: "send_email", Arguments: "{}"}}}},
		{Role: provider.RoleAssistant, Content: "做完"},
	}}
	asked := make(chan string, 1)
	k.BackgroundApprove = func(_ context.Context, tool, summary string) (bool, error) {
		asked <- tool
		return true, nil
	}
	ev := link.TerminalEvent("u1", "做个长任务", time.Unix(10, 0).UTC())
	_ = box.Post(ev)
	if err := k.Handle(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	bgs := k.Backgrounds()
	if len(bgs) != 1 {
		t.Fatalf("backgrounds %d", len(bgs))
	}
	close(g.release)
	waitNoBackgrounds(t, k, bgs[0].ID)
	select {
	case tool := <-asked:
		if tool != "send_email" {
			t.Fatalf("asked for %q", tool)
		}
	default:
		t.Fatal("background confirmation was not routed to the owner")
	}
	if w.ran != 1 {
		t.Fatalf("approved tool ran %d times", w.ran)
	}
}

type confirmWrite struct{ ran int }

func (*confirmWrite) Name() string        { return "send_email" }
func (*confirmWrite) Description() string { return "send_email" }
func (*confirmWrite) Parameters() any     { return map[string]any{"type": "object"} }
func (*confirmWrite) Confirm(json.RawMessage) (bool, string, error) {
	return true, "risky thing", nil
}
func (w *confirmWrite) Run(context.Context, json.RawMessage) (string, error) {
	w.ran++
	return "ok", nil
}

type oneToolLink struct{ t tools.Tool }

func (oneToolLink) Name() string          { return "one" }
func (l oneToolLink) Tools() []tools.Tool { return []tools.Tool{l.t} }

func gateLinkOf(t tools.Tool) link.Link { return oneToolLink{t} }

// TestWaitNoBackgroundsWaitsForForkEndEvent is the regression for the helper
// returning as soon as the running set is empty. finishBackground clears that
// set and only then posts the inbox event; a caller that reads the inbox at
// the empty set sees zero fork_end events.
func TestWaitNoBackgroundsWaitsForForkEndEvent(t *testing.T) {
	k, _, box := forkKernel(t)
	id := "bg_waitend"
	k.bgMu.Lock()
	k.bgs = map[string]*turn{id: {branch: id, task: "hold"}}
	k.bgMu.Unlock()
	go func() {
		time.Sleep(30 * time.Millisecond)
		k.bgMu.Lock()
		delete(k.bgs, id)
		ended := time.Now()
		k.finished = append([]BackgroundInfo{{ID: id, Task: "hold", Status: "done", Ended: &ended}}, k.finished...)
		k.bgMu.Unlock()
		// The old helper has already returned in this window. The event is
		// still absent, which is the failure the suite was hitting.
		time.Sleep(300 * time.Millisecond)
		ev := link.Event{
			ID: "ev_" + id + "_end", Source: link.SourceSubagent, Type: "fork_end",
			Content: "done", Sender: id, Time: time.Now().UTC(),
		}
		if err := box.Post(ev); err != nil {
			t.Errorf("post fork_end: %v", err)
		}
	}()
	start := time.Now()
	waitNoBackgrounds(t, k, id)
	if n := len(inboxPosts(t, k.Bot.InboxPath(), "ev_"+id+"_end")); n != 1 {
		t.Fatalf("fork_end events %d", n)
	}
	if time.Since(start) < 250*time.Millisecond {
		t.Fatal("wait returned before the fork_end event was written")
	}
}
