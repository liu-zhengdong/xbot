package kernel

import (
	"context"
	"encoding/json"
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

func waitNoBackgrounds(t *testing.T, k *Kernel) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for len(k.Backgrounds()) > 0 {
		if time.Now().After(deadline) {
			t.Fatal("background did not finish")
		}
		time.Sleep(20 * time.Millisecond)
	}
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
	waitNoBackgrounds(t, k)
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
	close(g.release)
	waitNoBackgrounds(t, k)
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
