package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/inbox"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

func TestShellJobSurvivesTurns(t *testing.T) {
	k, p, now := freshBot(t)
	args := mustArgs(t, map[string]any{"command": "sleep 30", "run_in_background": true})
	runTurn(t, k, now, "start it",
		toolCall("c1", "shell", args),
		provider.Message{Role: provider.RoleAssistant, Content: "started"},
	)
	got := bodies(t, k)
	if len(got) != 1 || !strings.Contains(got[0], "background sh_") {
		t.Fatalf("%q", got)
	}
	id := strings.TrimSpace(strings.TrimPrefix(strings.Split(got[0], "\n")[0], "background "))
	t.Cleanup(func() {
		if jobs := k.shellJobs(); jobs != nil {
			_, _ = jobs.Cancel(id)
		}
	})
	advance(now, time.Second)
	statusArgs := mustArgs(t, map[string]string{"id": id})
	p.resps = nil
	p.i = 0
	runTurn(t, k, now, "status",
		toolCall("c2", "shell_status", statusArgs),
		provider.Message{Role: provider.RoleAssistant, Content: "still going"},
	)
	got = bodies(t, k)
	if len(got) < 2 || !strings.Contains(got[len(got)-1], "running") {
		t.Fatalf("%q", got)
	}
	desc := ""
	for _, tool := range p.reqs[len(p.reqs)-1].Tools {
		if tool.Name == "shell" {
			desc = tool.Description
		}
	}
	for _, s := range []string{"run_in_background", "shell_output", "tight loop"} {
		if !strings.Contains(desc, s) {
			t.Fatalf("model shell description missing %q: %s", s, desc)
		}
	}
}

func TestShellNoticeWaitsUntilToolReturns(t *testing.T) {
	b := testBot(t, link.NameTerminal, link.NameTools)
	in, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	p := &scripted{}
	entered := make(chan struct{})
	release := make(chan struct{})
	k := &Kernel{
		Bot: b, Provider: p, Inbox: in, Memory: memory.NopStore{}, MaxSteps: 4,
		Links: []link.Link{link.Terminal{}, link.NewToolsLink(), shellHoldLink{entered: entered, release: release}},
		Now:   func() time.Time { return time.Unix(1_700_000_000, 0).UTC() },
	}
	p.resps = []provider.Message{
		toolCall("h1", "hold", "{}"),
		{Role: provider.RoleAssistant, Content: "done"},
	}
	errc := make(chan error, 1)
	go func() {
		errc <- k.Handle(context.Background(), link.TerminalEvent("u1", "go", time.Unix(1_700_000_000, 0).UTC()))
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("hold did not start")
	}
	out, err := k.shellJobs().Run(context.Background(), tools.RunRequest{
		Command: "echo finished-marker", Background: true, Timeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "background sh_") {
		t.Fatalf("%s", out)
	}
	id := strings.TrimSpace(strings.Split(strings.TrimPrefix(out, "background "), "\n")[0])
	deadline := time.Now().Add(2 * time.Second)
	for {
		st, err := k.shellJobs().Status(id)
		if err == nil && st.Status == "done" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("status %+v err %v", st, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if n := len(in.Pending()); n != 0 {
		t.Fatalf("woke during an in-flight tool: %d", n)
	}
	if len(p.reqs) != 1 {
		t.Fatalf("model was called again during the tool (%d)", len(p.reqs))
	}
	close(release)
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
	if len(p.reqs) < 2 {
		t.Fatal("no second request")
	}
	last := p.reqs[len(p.reqs)-1]
	blob := last.Messages[len(last.Messages)-1].Content
	if !strings.Contains(blob, "<shell_job>") || !strings.Contains(blob, id) {
		t.Fatalf("next request missed the reminder: %s", blob)
	}
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Message.Content, "<shell_job>") {
			t.Fatal("reminder was written into the transcript")
		}
	}
	raw, err := os.ReadFile(filepath.Join(bot.Dir(b.Root, b.Name()), "reminders.jsonl"))
	if err != nil || !strings.Contains(string(raw), "shell_job") || !strings.Contains(string(raw), id) {
		t.Fatalf("sidecar %s err %v", raw, err)
	}
}

func TestIdleShellJobWakesAndStaysANormalTurn(t *testing.T) {
	b := testBot(t, link.NameTerminal, link.NameTools)
	in, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	p := &scripted{resps: []provider.Message{
		toolCall("s1", "shell", `{"command":"echo poll-x"}`),
		toolCall("s2", "shell", `{"command":"echo poll-x"}`),
		toolCall("s3", "shell", `{"command":"echo poll-x"}`),
		toolCall("s4", "shell", `{"command":"echo poll-x"}`),
		{Role: provider.RoleAssistant, Content: "stopped"},
	}}
	k := &Kernel{
		Bot: b, Provider: p, Inbox: in, Memory: memory.NopStore{}, MaxSteps: 8,
		Links: []link.Link{link.Terminal{}, link.NewToolsLink()},
		Now:   func() time.Time { return time.Unix(1_700_000_100, 0).UTC() },
	}
	out, err := k.shellJobs().Run(context.Background(), tools.RunRequest{
		Command: "echo idle-done", Background: true, Timeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	id := strings.TrimSpace(strings.Split(strings.TrimPrefix(out, "background "), "\n")[0])
	var ev link.Event
	deadline := time.Now().Add(2 * time.Second)
	for {
		pending := in.Pending()
		if len(pending) > 0 {
			ev = pending[0]
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("idle bot was not woken")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if ev.Source != "shell" || link.OwnerTrusted(ev.Source, ev.Sender, b.Config().Owners) {
		t.Fatalf("wake should be an ordinary untrusted event: %+v", ev)
	}
	if err := k.Handle(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	if len(k.Backgrounds()) != 0 {
		t.Fatalf("wake opened a background segment: %+v", k.Backgrounds())
	}
	if len(p.reqs) == 0 {
		t.Fatal("no model request")
	}
	var saw bool
	for _, m := range p.reqs[0].Messages {
		if strings.Contains(m.Content, id) && strings.Contains(m.Content, "shell_output") {
			saw = true
		}
	}
	if !saw {
		t.Fatal("wake turn did not name the job")
	}
	var stopped bool
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Message.Content, "<stop_reason>"+StopSameToolArgs+"</stop_reason>") {
			stopped = true
		}
	}
	if !stopped {
		t.Fatal("wake turn did not keep the breaker")
	}
}

func TestShellJobFileUsesKernelRedactor(t *testing.T) {
	b := testBot(t, link.NameTerminal, link.NameTools)
	k := &Kernel{
		Bot: b,
		Redact: func(s string) string {
			return strings.ReplaceAll(s, "SUPERSECRET", "[secret]")
		},
		Now: func() time.Time { return time.Unix(1_700_000_300, 0).UTC() },
	}
	out, err := k.shellJobs().Run(context.Background(), tools.RunRequest{
		Command: "printf SUPERSECRET; sleep 30", Background: true, Timeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	id := strings.TrimSpace(strings.Split(strings.TrimPrefix(out, "background "), "\n")[0])
	t.Cleanup(func() { _, _ = k.shellJobs().Cancel(id) })
	st, err := k.shellJobs().Status(id)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	var body []byte
	for {
		body, err = os.ReadFile(st.Output)
		if err == nil && strings.Contains(string(body), "[secret]") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("file %q err %v", body, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if strings.Contains(string(body), "SUPERSECRET") {
		t.Fatal("secret landed in the job file")
	}
	meta, err := os.ReadFile(strings.TrimSuffix(st.Output, ".log") + ".json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(meta), "SUPERSECRET") {
		t.Fatalf("secret in meta %s", meta)
	}
}

func TestShellDoneDoesNotExtendSegment(t *testing.T) {
	b := testBot(t, link.NameTerminal, link.NameTools)
	in, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	k := &Kernel{
		Bot: b, Inbox: in, MaxSegments: 1,
		Now: func() time.Time { return time.Unix(1_700_000_200, 0).UTC() },
	}
	tr := &turn{started: k.now(), segments: k.segmentCap(), capStopped: true}
	k.turnMu.Lock()
	k.turn = tr
	k.turnMu.Unlock()
	k.onShellDone(tools.Job{ID: "sh_aabbccdd", Status: "done", Output: "x"})
	tr.mu.Lock()
	segs := tr.segments
	tr.mu.Unlock()
	if segs != k.segmentCap() {
		t.Fatalf("segments %d", segs)
	}
	if n := len(in.Pending()); n != 0 {
		t.Fatalf("busy turn was woken: %d", n)
	}
}

type shellHoldLink struct {
	entered chan struct{}
	release chan struct{}
}

func (shellHoldLink) Name() string { return "hold" }
func (h shellHoldLink) Tools() []tools.Tool {
	return []tools.Tool{shellHoldTool{entered: h.entered, release: h.release}}
}

type shellHoldTool struct {
	entered chan struct{}
	release chan struct{}
}

func (shellHoldTool) Name() string        { return "hold" }
func (shellHoldTool) Description() string { return "block until released" }
func (shellHoldTool) Parameters() any {
	return map[string]any{"type": "object", "properties": map[string]any{}, "required": []any{}, "additionalProperties": false}
}
func (shellHoldTool) Confirm(json.RawMessage) (bool, string, error) {
	return false, "hold", nil
}
func (h shellHoldTool) Run(context.Context, json.RawMessage) (string, error) {
	close(h.entered)
	<-h.release
	return "held", nil
}

func TestRestartDoesNotRewakeFinishedJobs(t *testing.T) {
	k, p, _ := freshBot(t)
	in, err := inbox.Open(k.Bot.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	k.Inbox = in
	dir := filepath.Join(bot.Dir(k.Bot.Root, k.Bot.Name()), "shell-jobs")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for i := 0; i < 6; i++ {
		id := fmt.Sprintf("sh_%08x", 0xa0000000+i)
		ids = append(ids, id)
		job := tools.Job{
			ID: id, Command: "make", Status: "done",
			Output:  filepath.Join(dir, id+".log"),
			Started: time.Unix(10, 0), Ended: time.Unix(20, 0),
		}
		raw, err := json.Marshal(job)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, id+".json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(job.Output, []byte("ok\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := k.reminders().Record(1, KindShellJob, "<shell_job>\n"+strings.Join(ids, "\n")+"\n</shell_job>", time.Unix(30, 0)); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0).UTC()
	p.resps = append(p.resps, provider.Message{Role: provider.RoleAssistant, Content: "hi"})
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "hello", now)); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, ev := range in.Pending() {
		if ev.Type == "shell_job" {
			n++
		}
	}
	if n > 0 {
		t.Fatalf("restart queued %d wakes for jobs already reported", n)
	}
}

func TestShellClaimedMapRace(t *testing.T) {
	k, _, _ := freshBot(t)
	in, err := inbox.Open(k.Bot.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	k.Inbox = in
	dir := filepath.Join(bot.Dir(k.Bot.Root, k.Bot.Name()), "shell-jobs")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 30; i++ {
		id := fmt.Sprintf("sh_%08x", 0xb0000000+i)
		job := tools.Job{
			ID: id, Command: "make", Status: "done", Local: true,
			Output:  filepath.Join(dir, id+".log"),
			Started: time.Unix(10, 0), Ended: time.Unix(20, 0),
		}
		raw, _ := json.Marshal(job)
		if err := os.WriteFile(filepath.Join(dir, id+".json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if k.shellJobs() == nil {
		t.Fatal("no jobs")
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			k.flushShellWake()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			k.shellNotice(nil)
		}
	}()
	wg.Wait()
}

func TestAttachShellJobsOnce(t *testing.T) {
	k, _, _ := freshBot(t)
	var sh *tools.Shell
	for _, l := range k.snapshotLinks() {
		for _, tl := range l.Tools() {
			if s, ok := tl.(*tools.Shell); ok {
				sh = s
			}
		}
	}
	if sh == nil {
		t.Fatal("no shell")
	}
	k.attachShellJobs()
	args, _ := json.Marshal(map[string]any{"command": "true"})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			_, _ = k.modelTools()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			_, _ = sh.Run(context.Background(), args)
		}
	}()
	wg.Wait()
}

type plainShellLink struct{}

func (plainShellLink) Name() string { return link.NameTools }
func (plainShellLink) Tools() []tools.Tool {
	return []tools.Tool{tools.NewShell(), &tools.ReadFile{}, &tools.WriteFile{}, &tools.EditFile{}}
}

func TestEvalNamedToolsLinkOmitsShellJobTools(t *testing.T) {
	k, p, _ := freshBot(t)
	k.Links = []link.Link{link.Terminal{}, plainShellLink{}}
	p.resps = []provider.Message{{Role: provider.RoleAssistant, Content: "hi"}}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "hello", time.Unix(1_700_000_500, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range p.reqs[0].Tools {
		names = append(names, tool.Name)
		if tool.Name == "shell" && strings.Contains(tool.Description, "run_in_background") {
			t.Fatal("shell without a job table offers backgrounding")
		}
	}
	got := strings.Join(names, ",")
	if strings.Contains(got, "shell_output") || strings.Contains(got, "shell_status") || strings.Contains(got, "shell_cancel") {
		t.Fatalf("job tools on a shell with no job table: %s", got)
	}
}
