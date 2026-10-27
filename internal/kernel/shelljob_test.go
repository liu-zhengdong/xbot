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
	"xbot/internal/config"
	"xbot/internal/inbox"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
	"xbot/internal/tools"
	"xbot/internal/vault"
)

func TestShellJobSurvivesTurns(t *testing.T) {
	k, p, now := freshBot(t)
	args := mustArgs(t, map[string]any{"description": "Verify the scenario behavior", "command": "sleep 30", "run_in_background": true})
	runTurn(t, k, now, "start it",
		toolCall("c1", "sh", args),
		provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("started")},
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
		provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("still going")},
	)
	got = bodies(t, k)
	if len(got) < 2 || !strings.Contains(got[len(got)-1], "running") {
		t.Fatalf("%q", got)
	}
	desc := ""
	for _, tool := range p.reqs[len(p.reqs)-1].Tools {
		if tool.Name == "sh" {
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
		{Role: provider.RoleAssistant, Content: provider.TextContent("done")},
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
	if n := len(in.Pending()); n != 1 {
		t.Fatalf("completed job must enter ordinary inbox even during a tool: %d", n)
	}
	if len(p.reqs) != 1 {
		t.Fatalf("model called during tool: %d", len(p.reqs))
	}
	close(release)
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
	if len(p.reqs) < 2 {
		t.Fatal("no next request")
	}
	text := messagesText(p.reqs[len(p.reqs)-1].Messages)
	if !strings.Contains(text, id) || !strings.Contains(text, "finished-marker") {
		t.Fatalf("missing ordinary report: %s", text)
	}
	es, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, e := range es {
		if e.EventID == "shelljob_"+id {
			found++
		}
	}
	if found != 1 {
		t.Fatalf("report rows=%d", found)
	}

}

func TestIdleShellJobWakesAndStaysANormalTurn(t *testing.T) {
	b := testBot(t, link.NameTerminal, link.NameTools)
	in, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	p := &scripted{resps: []provider.Message{
		toolCall("s1", "sh", `{"description":"Verify the scenario behavior","command":"echo poll-x"}`),
		toolCall("s2", "sh", `{"description":"Verify the scenario behavior","command":"echo poll-x"}`),
		toolCall("s3", "sh", `{"description":"Verify the scenario behavior","command":"echo poll-x"}`),
		toolCall("s4", "sh", `{"description":"Verify the scenario behavior","command":"echo poll-x"}`),
		{Role: provider.RoleAssistant, Content: provider.TextContent("stopped")},
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
		if strings.Contains(m.Text(), id) && strings.Contains(m.Text(), "idle-done") {
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
		if strings.Contains(e.Message.Text(), "<stop_reason>"+StopSameToolArgs+"</stop_reason>") {
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
		secrets: map[string]map[string]string{"turn": {"s": "SUPERSECRET"}},
		Now:     func() time.Time { return time.Unix(1_700_000_300, 0).UTC() },
	}
	out, err := k.shellJobs().Run(context.Background(), tools.RunRequest{
		Command: "printf 'SUPERSECRET\\n'; sleep 30", Background: true, Timeout: time.Second,
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

// TestShellDoneDoesNotWakeABusyTurn checks a finished shell job does not post a
// wake while a turn is running: the live turn hears about it at its next
// boundary through the reminder channel.
func TestToolDonePostsWhileBusy(t *testing.T) {
	k, _, _ := freshBot(t)
	in, err := inbox.Open(k.Bot.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	k.Inbox = in
	k.turn = &turn{started: k.now()}
	jobs := k.shellJobs()
	defer jobs.Shutdown()
	jobs.RunCall(context.Background(), tools.CallRequest{Name: "any_tool", Background: true, Run: func(context.Context) tools.CallResult { return tools.CallResult{Text: "FINISHED_WHILE_BUSY"} }})
	end := time.Now().Add(time.Second)
	for len(in.Pending()) == 0 && time.Now().Before(end) {
		time.Sleep(time.Millisecond)
	}
	if len(in.Pending()) != 1 || !strings.Contains(in.Pending()[0].Content, "FINISHED_WHILE_BUSY") {
		t.Fatalf("missing report: %+v", in.Pending())
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
			ID: id, Command: "make", Status: "done", Notified: true,
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

	now := time.Unix(1_700_000_000, 0).UTC()
	p.resps = append(p.resps, provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("hi")})
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

func TestToolJobConcurrentInboxFlush(t *testing.T) {
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
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("sh_%08x", 0xb0000000+i)
		job := tools.Job{
			ID: id, Command: "make", Status: "done", Background: true,
			Output:  filepath.Join(dir, id+".log"),
			Started: k.now(), Ended: k.now(),
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
	if k.shellJobs() == nil {
		t.Fatal("no jobs")
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			k.flushToolJobs()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			k.flushToolJobs()
		}
	}()
	wg.Wait()
	if n := len(in.Pending()); n != 5 {
		t.Fatalf("posted %d jobs, want 5", n)
	}
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
	args, _ := json.Marshal(map[string]any{"description": "Verify the scenario behavior", "command": "true"})
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
	p.resps = []provider.Message{{Role: provider.RoleAssistant, Content: provider.TextContent("hi")}}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "hello", time.Unix(1_700_000_500, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range p.reqs[0].Tools {
		names = append(names, tool.Name)
		if tool.Name == "sh" && strings.Contains(tool.Description, "run_in_background") {
			t.Fatal("shell without a job table offers backgrounding")
		}
	}
	got := strings.Join(names, ",")
	if strings.Contains(got, "shell_output") || strings.Contains(got, "shell_status") || strings.Contains(got, "shell_cancel") {
		t.Fatalf("job tools on a shell with no job table: %s", got)
	}
}

func TestShellFormsFollowVault(t *testing.T) {
	k, _, _ := freshBot(t)
	line := strings.Repeat("M", 64)
	var b strings.Builder
	b.WriteString("-----BEGIN " + "PRIVATE KEY-----\n")
	for i := 0; i < 26; i++ {
		b.WriteString(line + "\n")
	}
	b.WriteString("-----END " + "PRIVATE KEY-----")
	value := b.String()
	store, err := vault.Open(t.TempDir(), filepath.Join(t.TempDir(), "k"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set("pem", value, []string{"example.test"}); err != nil {
		t.Fatal(err)
	}
	k.Vault = store
	unreg := vault.Register(store)
	t.Cleanup(unreg)
	if !containsForm(k.shellForms(), value) {
		t.Fatal("vault secret missing from shell forms")
	}
	k.secrets = map[string]map[string]string{"turn": {"n": "short-secret"}}
	forms := k.shellForms()
	if !containsForm(forms, value) || !containsForm(forms, "short-secret") {
		t.Fatalf("forms dropped a value: %d", len(forms))
	}
	longer := value + "\n" + value
	if err := store.Set("pem", longer, []string{"example.test"}); err != nil {
		t.Fatal(err)
	}
	if containsForm(k.shellForms(), value) || !containsForm(k.shellForms(), longer) {
		t.Fatal("forms did not follow the vault")
	}
}

func TestShellFormsHoldSplitKey(t *testing.T) {
	const secret = "heldkey-0123456789abcdef"
	t.Setenv("MAGPIE_API_KEY", secret)
	t.Cleanup(config.ForgetKeys)
	config.HoldKeys([]string{"MAGPIE_API_KEY"})
	k, _, _ := freshBot(t)
	if !containsForm(k.shellForms(), secret) {
		t.Fatal("held key missing from shell forms")
	}
	jobs := k.shellJobs()
	if jobs == nil {
		t.Fatal("no jobs")
	}
	out, err := jobs.Run(context.Background(), tools.RunRequest{
		Command:    "printf 'before\\n'; printf 'heldkey-0123'; sleep 30",
		Background: true,
		Timeout:    time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	id := strings.TrimSpace(strings.Split(strings.TrimPrefix(out, "background "), "\n")[0])
	t.Cleanup(func() { _, _ = jobs.Cancel(id) })
	deadline := time.Now().Add(2 * time.Second)
	var text string
	for {
		text, err = jobs.Output(id, 0, 0)
		if err == nil && strings.Contains(text, "before") && strings.Contains(text, "held_bytes:") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("split key was not held back: err %v", err)
		}
		time.Sleep(15 * time.Millisecond)
	}
	if strings.Contains(text, "heldkey") || strings.Contains(text, secret) {
		t.Fatal("held key leaked into shell output")
	}
}

func containsForm(forms []string, want string) bool {
	for _, f := range forms {
		if f == want {
			return true
		}
	}
	return false
}

func TestRunningShortOutputAndHeldPrefix(t *testing.T) {
	k, _, _ := freshBot(t)
	secret := "sk-live-0123456789abcdef"
	k.secrets = map[string]map[string]string{"turn": {"tok": secret}}
	jobs := k.shellJobs()
	if jobs == nil {
		t.Fatal("no jobs")
	}
	out, err := jobs.Run(context.Background(), tools.RunRequest{
		Command:    "printf 'Compiling…\\nerror: missing dep foo\\n'; printf 'sk-live-0123'; sleep 30",
		Background: true,
		Timeout:    time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	id := strings.TrimSpace(strings.Split(strings.TrimPrefix(out, "background "), "\n")[0])
	t.Cleanup(func() { _, _ = jobs.Cancel(id) })
	deadline := time.Now().Add(2 * time.Second)
	var text string
	for {
		text, err = jobs.Output(id, 0, 0)
		if err == nil && strings.Contains(text, "missing dep foo") && strings.Contains(text, "held_bytes:") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("output %q err %v", text, err)
		}
		time.Sleep(15 * time.Millisecond)
	}
	if strings.Contains(text, "sk-live") || strings.Contains(text, secret) {
		t.Fatalf("secret prefix visible: %s", text)
	}
	st, err := jobs.Status(id)
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != "running" || st.Bytes == 0 {
		t.Fatalf("status %+v", st)
	}
	got, err := shellStatusTool{k: k}.Run(context.Background(), json.RawMessage(`{"id":"`+id+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "held_bytes:") {
		t.Fatalf("status tool %s", got)
	}
}

func TestScrubSecretsLongestFirst(t *testing.T) {
	values := map[string]string{}
	var raw, want strings.Builder
	for i := 0; i < 16; i++ {
		short := fmt.Sprintf("pre%02dXXXX", i)
		long := short + "TAIL"
		values[fmt.Sprintf("s%d", i)] = short
		values[fmt.Sprintf("l%d", i)] = long
		raw.WriteString(long)
		raw.WriteByte('\n')
		want.WriteString("[secret]\n")

		overShort := fmt.Sprintf("ov%02dYYYY", i)
		overLong := "abc" + overShort
		values[fmt.Sprintf("os%d", i)] = overShort
		values[fmt.Sprintf("ol%d", i)] = overLong
		raw.WriteString(overLong)
		raw.WriteString("Z")
		raw.WriteByte('\n')
		want.WriteString("[secret]Z\n")
	}
	for n := 0; n < 20; n++ {
		got := scrubSecrets(raw.String(), values)
		if got != want.String() {
			t.Fatalf("iter %d\n got %q\nwant %q", n, got, want.String())
		}
	}
}

func TestScrubSecretsStableTieBreak(t *testing.T) {
	values := map[string]string{"a": "abcd", "b": "bcde"}
	const want = "[secret]e"
	for i := 0; i < 40; i++ {
		got := scrubSecrets("abcde", values)
		if got != want {
			t.Fatalf("iter %d got %q", i, got)
		}
	}
}

func TestToolWakeFailureRemainsRetryable(t *testing.T) {
	k, _, _ := freshBot(t)
	in, err := inbox.Open(k.Bot.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	k.Inbox = in
	jobs := k.shellJobs()
	defer jobs.Shutdown()
	release := make(chan struct{})
	result := jobs.RunCall(context.Background(), tools.CallRequest{Name: "custom", Background: true, Run: func(context.Context) tools.CallResult { <-release; return tools.CallResult{Text: "RECOVER_OUTPUT"} }})
	id := result.Job.ID
	path := k.Bot.InboxPath()
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	close(release)
	end := time.Now().Add(time.Second)
	var st tools.Job
	for time.Now().Before(end) {
		st, _ = jobs.Status(id)
		if st.Status != "running" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	_, _ = jobs.Cancel(id)
	st, _ = jobs.Status(id)
	if st.Notified {
		t.Fatal("failed post was acknowledged")
	}
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	in2, err := inbox.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	k.Inbox = in2
	k.onToolDone(st)
	if len(in2.Pending()) != 1 {
		t.Fatalf("wakes %d", len(in2.Pending()))
	}
	st, _ = jobs.Status(id)
	if !st.Notified {
		t.Fatal("successful post not acknowledged")
	}
}

// The job writer holds a split secret back only for forms it is given; a
// table without Forms would commit secret prefixes as they arrive (#17).
func TestKernelShellJobsHaveForms(t *testing.T) {
	k, _, _ := freshBot(t)
	jobs := k.shellJobs()
	if jobs == nil || jobs.Forms == nil || jobs.Redact == nil {
		t.Fatalf("jobs %+v", jobs)
	}
}

// Staged secrets are scrubbed in one pass: an inserted [secret] is not
// scanned again, so a value that matches across it cannot mangle it (#17).
func TestScrubSecretsIsOnePass(t *testing.T) {
	got := scrubSecrets("XYZ12345tail1", map[string]string{"a": "XYZ12345", "b": "et]tail1"})
	if got != "[secret]tail1" {
		t.Fatalf("got %q", got)
	}
}

// leaveRenamedRunningJob writes a still-running job record and its output file
// into the bot's shell-jobs directory, as a serve that died mid-rename leaves
// behind: the files moved with the bot, the output path inside did not.
func leaveRenamedRunningJob(t *testing.T, root, name string) string {
	t.Helper()
	jobsDir := filepath.Join(bot.Dir(root, name), "shell-jobs")
	if err := os.MkdirAll(jobsDir, 0700); err != nil {
		t.Fatal(err)
	}
	id := "sh_0abc12de"
	meta, err := json.Marshal(tools.Job{
		ID: id, Command: "sleep 30", Status: "running",
		Output:  filepath.Join(jobsDir, id+".log"),
		Started: time.Unix(1_700_000_000, 0).UTC(), PID: 1 << 22, ProcStart: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jobsDir, id+".json"), meta, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jobsDir, id+".log"), []byte("running output\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return id
}

// A rename leaves a running job record whose output names the old bot
// directory. A fresh kernel still gets a working job table: the record turns
// lost with the output repointed, and tools, send_message_to_user included,
// keep answering instead of reporting storage unavailable.
func TestRenameKeepsToolJobsWorking(t *testing.T) {
	k, _, now := freshBot(t)
	root := k.Bot.Root
	id := leaveRenamedRunningJob(t, root, "ada")
	if err := RenameBot(root, "ada", "grace"); err != nil {
		t.Fatal(err)
	}
	loaded, err := bot.Load(root, "grace")
	if err != nil {
		t.Fatal(err)
	}
	p := &scripted{}
	k2 := &Kernel{
		Bot: loaded, Provider: p, Memory: memory.NopStore{}, MaxSteps: 8,
		Links: []link.Link{link.Terminal{}, link.NewToolsLink()},
		Now:   func() time.Time { return *now },
	}
	next := *now
	runTurn(t, k2, &next, "say it",
		toolCall("m1", "send_message_to_user", mustArgs(t, map[string]string{"text": "改名以后还能说话"})),
		provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("done")},
	)
	got := bodies(t, k2)
	if len(got) == 0 {
		t.Fatal("no tool results")
	}
	if last := got[len(got)-1]; strings.Contains(last, "unavailable") || !strings.Contains(last, "sent to the owner's chat") {
		t.Fatalf("send_message_to_user after rename: %q", last)
	}
	jobs := k2.shellJobs()
	if jobs == nil {
		t.Fatal("job table missing after rename")
	}
	st, err := jobs.Status(id)
	if err != nil || st.Status != "lost" {
		t.Fatalf("leftover job %+v err %v", st, err)
	}
	if want := filepath.Join(bot.Dir(root, "grace"), "shell-jobs", id+".log"); st.Output != want {
		t.Fatalf("output %q, want %q", st.Output, want)
	}
	for _, job := range jobs.List() {
		if job.Tool == "send_message_to_user" && job.Output != "" &&
			!strings.HasPrefix(job.Output, filepath.Join(bot.Dir(root, "grace"), "shell-jobs")) {
			t.Fatalf("new record left the bot directory: %+v", job)
		}
	}
}

// bot_update_profile renames inside a live kernel. The job set follows the
// bot to the new directory, so tool calls later in the same turn do not write
// back into the old one.
func TestShellJobsFollowRename(t *testing.T) {
	k, _, now := freshBot(t)
	root := k.Bot.Root
	before := k.shellJobs()
	if before == nil || before.Dir != filepath.Join(bot.Dir(root, "ada"), "shell-jobs") {
		t.Fatalf("before %+v", before)
	}
	if err := k.renameBot("grace"); err != nil {
		t.Fatal(err)
	}
	after := k.shellJobs()
	if after == nil || after.Dir != filepath.Join(bot.Dir(root, "grace"), "shell-jobs") {
		t.Fatalf("after %+v", after)
	}
	runTurn(t, k, now, "say it",
		toolCall("m1", "send_message_to_user", mustArgs(t, map[string]string{"text": "同一轮里继续说"})),
		provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("done")},
	)
	got := bodies(t, k)
	if last := got[len(got)-1]; strings.Contains(last, "unavailable") || !strings.Contains(last, "sent to the owner's chat") {
		t.Fatalf("send_message_to_user after in-kernel rename: %q", last)
	}
	for _, job := range k.shellJobs().List() {
		if job.Tool == "send_message_to_user" && !strings.HasPrefix(job.Output, filepath.Join(bot.Dir(root, "grace"), "shell-jobs")) {
			t.Fatalf("record written into the old directory: %+v", job)
		}
	}
}

// The real self-rename: bot_update_profile is itself a running job in the old
// directory when it moves the bot. Its completion lands in the new directory,
// the old bot directory is not recreated, and the same turn keeps working.
func TestProfileRenameDoesNotRecreateOldDir(t *testing.T) {
	b := testBot(t, link.NameTerminal, link.NameTools, link.NamePersona)
	root := b.Root
	now := time.Unix(1_700_000_000, 0).UTC()
	p := &scripted{resps: []provider.Message{
		toolCall("expand", "expand_tool_group", `{"group":"bot_profile"}`),
		toolCall("rename", "bot_update_profile", `{"name":"grace"}`),
		toolCall("say", "send_message_to_user", mustArgs(t, map[string]string{"text": "改名以后还能说话"})),
		{Role: provider.RoleAssistant, Content: provider.TextContent("done")},
	}}
	k := &Kernel{
		Bot: b, Provider: p, Memory: memory.NopStore{}, MaxSteps: 8,
		Links: []link.Link{link.Terminal{}, link.NewToolsLink(), link.Persona{}},
		Now:   func() time.Time { return now },
	}
	k.Approve = func(context.Context, string, string) (bool, error) { return true, nil }
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "改名叫 grace", now)); err != nil {
		t.Fatal(err)
	}
	if k.Bot.Name() != "grace" {
		t.Fatalf("bot %q, results %q", k.Bot.Name(), bodies(t, k))
	}
	got := bodies(t, k)
	if len(got) < 3 || !strings.Contains(got[1], "profile saved") || !strings.Contains(got[2], "sent to the owner's chat") {
		t.Fatalf("tool results %q", got)
	}
	if _, err := os.Stat(bot.Dir(root, "ada")); !os.IsNotExist(err) {
		t.Fatalf("old bot directory recreated after rename: %v", err)
	}
	newJobs := filepath.Join(bot.Dir(root, "grace"), "shell-jobs")
	if d := k.shellJobs().Dir; d != newJobs {
		t.Fatalf("job table at %q, want %q", d, newJobs)
	}
	ents, err := os.ReadDir(newJobs)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		raw, err := os.ReadFile(filepath.Join(newJobs, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(e.Name(), ".json") && strings.Contains(string(raw), `"running"`) {
			t.Fatalf("record left running in the new directory: %s", raw)
		}
	}
}

// Calls racing on the first lookup after a rename all get the one live table,
// so a job started on it stays queryable, cancellable, and in the new place.
func TestShellJobsKeepOneTableAfterRename(t *testing.T) {
	k, _, _ := freshBot(t)
	root := k.Bot.Root
	before := k.shellJobs()
	started := make(chan struct{})
	res := before.RunCall(context.Background(), tools.CallRequest{Name: "any_tool", Background: true, Run: func(ctx context.Context) tools.CallResult {
		close(started)
		<-ctx.Done()
		return tools.CallResult{Err: ctx.Err()}
	}})
	<-started
	if err := k.renameBot("grace"); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	tables := make([]*tools.Jobs, 8)
	for i := range tables {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tables[i] = k.shellJobs()
		}()
	}
	wg.Wait()
	for _, j := range tables {
		if j != before {
			t.Fatal("a second job table replaced the live one")
		}
	}
	id := res.Job.ID
	st, err := before.Status(id)
	if want := filepath.Join(bot.Dir(root, "grace"), "shell-jobs", id+".log"); err != nil || st.Status != "running" || st.Output != want {
		t.Fatalf("status %+v err %v, want running at %s", st, err, want)
	}
	if _, err := before.Cancel(id); err != nil {
		t.Fatal(err)
	}
	st, _ = before.Status(id)
	if st.Status != "cancelled" {
		t.Fatalf("after cancel %+v", st)
	}
	if _, err := os.Stat(bot.Dir(root, "ada")); !os.IsNotExist(err) {
		t.Fatalf("old bot directory recreated: %v", err)
	}
}

// Calls start and finish on the table while the bot renames. Each one either
// lands before the directory moves or after it, in the new place: none fails
// on a stale path, the table is never pointed back, and the old bot directory
// is not recreated.
func TestRenameRacesRunningCalls(t *testing.T) {
	k, _, _ := freshBot(t)
	root := k.Bot.Root
	jobs := k.shellJobs()
	stop := make(chan struct{})
	errs := make(chan error, 64)
	var wg, finished sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				bg := i%2 == 0
				if bg {
					finished.Add(1)
				}
				req := tools.CallRequest{Name: "any_tool", Background: bg, Run: func(context.Context) tools.CallResult {
					return tools.CallResult{Text: "ok"}
				}}
				if bg {
					req.OnDone = func(tools.Job) { finished.Done() }
				}
				res := k.shellJobs().RunCall(context.Background(), req)
				if res.Err != nil {
					if bg {
						finished.Done()
					}
					errs <- res.Err
					return
				}
			}
		}()
	}
	time.Sleep(20 * time.Millisecond)
	if err := k.renameBot("grace"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	close(stop)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("call across rename: %v", err)
	}
	finished.Wait()
	if k.shellJobs() != jobs {
		t.Fatal("a second job table replaced the live one")
	}
	newJobs := filepath.Join(bot.Dir(root, "grace"), "shell-jobs")
	if jobs.Dir != newJobs {
		t.Fatalf("job table at %q, want %q", jobs.Dir, newJobs)
	}
	for _, job := range jobs.List() {
		if job.Status != "done" || job.Output != filepath.Join(newJobs, job.ID+".log") {
			t.Fatalf("job across rename %+v", job)
		}
	}
	if _, err := os.Stat(bot.Dir(root, "ada")); !os.IsNotExist(err) {
		t.Fatalf("old bot directory recreated: %v", err)
	}
}
