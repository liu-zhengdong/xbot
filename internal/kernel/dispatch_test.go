package kernel

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"xbot/internal/agenttype"
	"xbot/internal/bot"
	"xbot/internal/browser"
	"xbot/internal/code"
	"xbot/internal/compact"
	"xbot/internal/desktop"
	"xbot/internal/exec"
	"xbot/internal/inbox"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
	"xbot/internal/tools"
	"xbot/internal/vault"
)

// roleModel answers the foreground and its subagents from separate scripts.
// A request whose system prompt is a subagent's goes to sub.
type roleModel struct {
	mu      sync.Mutex
	fore    []provider.Message
	sub     []provider.Message
	foreReq []provider.Request
	subReq  []provider.Request
	// hold, when set, keeps the subagent's first call waiting until it is
	// closed, so its result cannot be absorbed into the turn that sent it.
	hold chan struct{}
}

func (m *roleModel) Chat(_ context.Context, req provider.Request, _ func(string)) (provider.Message, error) {
	isSub := len(req.Messages) > 0 && strings.HasPrefix(req.Messages[0].Text(), exec.SubPersona)
	if isSub && m.hold != nil {
		<-m.hold
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if isSub {
		m.subReq = append(m.subReq, snapshotReq(req))
		if len(m.sub) == 0 {
			return provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("Findings\nnone")}, nil
		}
		msg := m.sub[0]
		m.sub = m.sub[1:]
		return msg, nil
	}
	m.foreReq = append(m.foreReq, snapshotReq(req))
	if len(m.fore) == 0 {
		return provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("ok")}, nil
	}
	msg := m.fore[0]
	m.fore = m.fore[1:]
	return msg, nil
}

func dispatchKernel(t *testing.T, p provider.Provider) (*Kernel, *bot.Bot, *inbox.Inbox) {
	t.Helper()
	b := testBot(t, link.NameTerminal, link.NameTools, link.NameAgents)
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	k := &Kernel{
		Bot: b, Provider: p, Memory: memory.NopStore{}, Inbox: box, MaxSteps: 8,
		Dispatch: true, WorkDir: dir,
		Links: []link.Link{link.Terminal{}, link.NewToolsLinkAt(dir, ""), link.Agents{}},
	}
	return k, b, box
}

func waitEvent(t *testing.T, box *inbox.Inbox, typ string) link.Event {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_ = box.Reload()
		for _, ev := range box.Pending() {
			if ev.Source == link.SourceSubagent && ev.Type == typ {
				return ev
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no %s event from the subagent; pending %+v", typ, box.Pending())
	return link.Event{}
}

// The foreground hands a task to a research subagent. The subagent gets the
// type's prompt and report sections, no tool that writes, and a shell that only
// reads; its result comes back as an event.
func TestDispatchStartsATypedSubagent(t *testing.T) {
	m := &roleModel{
		fore: []provider.Message{
			assistantCall("d1", "", "dispatch", `{"type":"research","task":"find the release date of Go 1.30"}`),
			{Role: provider.RoleAssistant, Content: provider.TextContent("已派调研员去查。")},
		},
		sub:  []provider.Message{{Role: provider.RoleAssistant, Content: provider.TextContent("Findings\nAugust\n\nSources\ngo.dev\n\nNot found\nnone")}},
		hold: make(chan struct{}),
	}
	k, b, box := dispatchKernel(t, m)
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "Go 1.30 什么时候发布？", time.Unix(9, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	close(m.hold)
	ev := waitEvent(t, box, exec.EvDone)
	path := filepath.Join(b.AgentDir(ev.Sender), "result.txt")
	if strings.Contains(ev.Content, "Sources") || !strings.Contains(ev.Content, path) {
		t.Fatalf("event %q", ev.Content)
	}
	if raw, err := os.ReadFile(path); err != nil || string(raw) != "Findings\nAugust\n\nSources\ngo.dev\n\nNot found\nnone" {
		t.Fatalf("lost research report: %s %v", raw, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	fore := toolNamesOf(m.foreReq[0].Tools)
	if !contains(fore, "dispatch") || contains(fore, "spawn") {
		t.Fatalf("foreground tools %v", fore)
	}
	sub := m.subReq[0]
	names := toolNamesOf(sub.Tools)
	for _, banned := range []string{"write_file", "edit_file", "dispatch", "spawn"} {
		if contains(names, banned) {
			t.Fatalf("research subagent has %s: %v", banned, names)
		}
	}
	if !contains(names, "sh") || !contains(names, "read_file") {
		t.Fatalf("research subagent tools %v", names)
	}
	sys := sub.Messages[0].Text()
	for _, want := range []string{"You are a researcher.", "Findings, Sources, Not found"} {
		if !strings.Contains(sys, want) {
			t.Fatalf("system prompt lacks %q: %q", want, sys)
		}
	}
	if got := sub.Messages[1].Text(); got != "find the release date of Go 1.30" {
		t.Fatalf("task %q", got)
	}
	metas, _ := filepath.Glob(filepath.Join(b.Root, "bots", b.Name(), "agents", "sa_*", "meta.json"))
	if len(metas) != 1 {
		t.Fatalf("meta files %v", metas)
	}
	raw, _ := os.ReadFile(metas[0])
	var meta exec.Meta
	_ = json.Unmarshal(raw, &meta)
	if meta.Type != "research" {
		t.Fatalf("meta %+v", meta)
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func TestDispatchRejectsUnknownType(t *testing.T) {
	k, _, _ := dispatchKernel(t, &roleModel{})
	if _, err := k.dispatch(context.Background(), "painter", "draw"); err == nil || !strings.Contains(err.Error(), "unknown type") {
		t.Fatalf("err %v", err)
	}
	if _, err := k.dispatch(context.Background(), "dev", "  "); err == nil {
		t.Fatal("empty task accepted")
	}
}

// A bot's own type file adds a type the foreground can pick.
func TestDispatchOffersTheBotsOwnTypes(t *testing.T) {
	k, b, _ := dispatchKernel(t, &roleModel{})
	if err := os.MkdirAll(b.AgentTypesDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b.AgentTypesDir(), "writer.md"), []byte("---\nwhen: draft long prose\ntools: read_file, write_file\n---\nWrite plainly.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := dispatchTool{k}
	if !strings.Contains(d.Description(), "writer: draft long prose") {
		t.Fatalf("description %q", d.Description())
	}
	got := k.toolsFor(exec.StartRequest{ID: "sa_0123456789", Type: "writer"})
	var names []string
	for _, t := range got {
		names = append(names, t.Name())
	}
	if len(names) != 2 || !contains(names, "read_file") || !contains(names, "write_file") {
		t.Fatalf("writer tools %v", names)
	}
}

func TestReadOnlyShellRefusesWrites(t *testing.T) {
	dir := t.TempDir()
	sh := tools.NewShell()
	sh.Dir = dir
	ro := readOnlyShell{sh}
	for _, cmd := range []string{"echo hi > f.txt", "rm -rf x", "ls; rm x", "touch f"} {
		args, _ := json.Marshal(map[string]any{"description": "Verify the scenario behavior", "command": cmd})
		if _, err := ro.Run(context.Background(), args); err == nil {
			t.Fatalf("%q ran", cmd)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "f.txt")); err == nil {
		t.Fatal("the refused write happened")
	}
	args, _ := json.Marshal(map[string]any{"description": "Verify the scenario behavior", "command": "ls"})
	if _, err := ro.Run(context.Background(), args); err != nil {
		t.Fatalf("ls refused: %v", err)
	}
}

// A subagent can run for hours, so its requests are kept under the lines the
// same way a turn's are.
func TestSubagentRequestsAreFit(t *testing.T) {
	k := &Kernel{Bot: testBot(t, link.NameTerminal), Budget: compact.Budget{Ceiling: 6000, KeepTail: 1}}
	inner := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: provider.TextContent("ok")}}}
	p := k.wrapSub(exec.StartRequest{}, inner, 2)
	msgs := []provider.Message{{Role: provider.RoleSystem, Content: provider.TextContent("sys")}, {Role: provider.RoleUser, Content: provider.TextContent("task")}}
	for i := 0; i < 6; i++ {
		id := string(rune('a' + i))
		msgs = append(msgs, assistantCall(id, "step", "read_file", `{}`), toolResult(id, "read_file", strings.Repeat("x", 8000)))
	}
	if _, err := p.Chat(context.Background(), provider.Request{Messages: msgs}, nil); err != nil {
		t.Fatal(err)
	}
	sent := inner.reqs[0].Messages
	if n := compact.EstimateMessages(sent); n > 6000 {
		t.Fatalf("sent %d tokens", n)
	}
	if !strings.Contains(sent[3].Text(), "Read the file or run the call again") {
		t.Fatalf("note %q", sent[3].Text())
	}
}

// With dispatch on, the main line still does the work itself (ADR 0028): a
// file write and a shell command that writes both run, more calls than the old
// foreground limit allowed keep every tool, and the rules say to answer the
// owner first and hand long work out.
func TestMainLineDoesTheWork(t *testing.T) {
	m := &roleModel{}
	k, _, _ := dispatchKernel(t, m)
	k.Approve = func(context.Context, string, string) (bool, error) { return true, nil }
	read := func(id string) provider.Message {
		return toolCall(id, "sh", mustArgs(t, map[string]any{"description": "Verify the scenario behavior", "command": "ls"}))
	}
	m.fore = []provider.Message{
		toolCall("w1", "write_file", mustArgs(t, map[string]string{"path": filepath.Join(k.WorkDir, "a.txt"), "content": "x"})),
		toolCall("s1", "sh", mustArgs(t, map[string]any{"description": "Verify the scenario behavior", "command": "echo x > b.txt"})),
		read("r1"), read("r2"), read("r3"), read("r4"),
		{Role: provider.RoleAssistant, Content: provider.TextContent("done")},
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "go", time.Unix(9, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	got := bodies(t, k)
	if len(got) != 6 {
		t.Fatalf("results %q", got)
	}
	for i, body := range got {
		if strings.Contains(body, "subagent with dispatch") {
			t.Fatalf("call %d was refused: %q", i, body)
		}
	}
	for _, f := range []string{"a.txt", "b.txt"} {
		if _, err := os.Stat(filepath.Join(k.WorkDir, f)); err != nil {
			t.Fatalf("%s was not written: %v", f, err)
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.foreReq) != 7 {
		t.Fatalf("requests %d", len(m.foreReq))
	}
	first := toolNamesOf(m.foreReq[0].Tools)
	for _, want := range []string{"write_file", "edit_file", "sh", "dispatch"} {
		if !contains(first, want) {
			t.Fatalf("main line lacks %s: %v", want, first)
		}
	}
	// Every request that asked for a tool call kept the whole set; the old
	// limit cut it down to dispatch and the controls after the fourth call.
	for i, req := range m.foreReq[:6] {
		if names := toolNamesOf(req.Tools); len(names) != len(first) {
			t.Fatalf("request %d narrowed the tool set: %v -> %v", i, first, names)
		}
	}
	last := m.foreReq[len(m.foreReq)-1]
	if strings.Contains(messagesText(last.Messages), "foreground_limit") {
		t.Fatal("a foreground limit note reached the model")
	}
	sys := m.foreReq[0].Messages[0].Text()
	if !strings.Contains(sys, DispatchRule) || !strings.Contains(sys, "Answer the owner first, then work") {
		t.Fatal("the rules lack the reply-first dispatch rule")
	}
	// The shared language rule must agree with the dispatch rule, not tell the
	// model to call tools before saying anything.
	if !strings.Contains(sys, LanguageRule) || !strings.Contains(sys, "first tell the owner in a sentence or two") {
		t.Fatal("the language rule does not ask for a reply before slow work")
	}
	if strings.Contains(sys, "Call tools first, then give the result") {
		t.Fatal("the rules still tell the model to call tools before answering")
	}
	if strings.Contains(sys, "You do not do the work yourself") {
		t.Fatal("the ADR 0016 foreground rule is still injected")
	}
}

// Without dispatch the main line has no dispatch tool and no dispatch rule.
func TestNoDispatchRuleWithoutDispatch(t *testing.T) {
	m := &roleModel{fore: []provider.Message{{Role: provider.RoleAssistant, Content: provider.TextContent("ok")}}}
	k, _, _ := dispatchKernel(t, m)
	k.Dispatch = false
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "hi", time.Unix(9, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	names := toolNamesOf(m.foreReq[0].Tools)
	if !contains(names, "spawn") || contains(names, "dispatch") {
		t.Fatalf("tools %v", names)
	}
	if strings.Contains(m.foreReq[0].Messages[0].Text(), DispatchRule) {
		t.Fatal("dispatch rule without dispatch")
	}
}

// workLink offers tools in every work class, as a connector, an MCP server,
// or a later link might.
type workLink struct{}

func (workLink) Name() string { return "worklink" }
func (workLink) Tools() []tools.Tool {
	return []tools.Tool{namedTool{"send_email"}, namedTool{"delete_file"}, namedTool{"mcp_srv_post"}, hintTool{ro: true}}
}

// Guard: whatever links a bot has, the main line is offered the whole mounted
// directory even while dispatching, including tools that write, send out, or
// delete (ADR 0028). The general subagent type takes the same directory minus
// the tools that hand work out; a narrower type still narrows it, and no
// subagent can hand work out again.
func TestMainLineAndSubagentsReachTheMountedTools(t *testing.T) {
	b := testBot(t, link.NameTerminal, link.NameTools, link.NameAgents,
		link.NameCoding, link.NameDesktop, link.NameBrowser)
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	dir := bot.Dir(b.Root, b.Name())
	store, err := vault.Open(b.VaultDir(), b.VaultKeyPath())
	if err != nil {
		t.Fatal(err)
	}
	k := &Kernel{
		Bot: b, Provider: &roleModel{}, Memory: memory.NopStore{}, Inbox: box, MaxSteps: 8,
		Dispatch: true, WorkDir: t.TempDir(), Vault: store,
		Links: []link.Link{
			link.Terminal{}, link.NewToolsLinkAt(dir, ""), link.Agents{}, link.Memory{}, link.Context{},
			link.NewConnector(dir, b.SecretPath(), link.NameTelegram),
			code.Open(dir, b.InboxPath()), desktop.Open(b.Root), browser.Open(b.Root),
			workLink{}, fakeMCPLink{},
		},
	}
	mounted, err := k.mountedTools()
	if err != nil {
		t.Fatal(err)
	}
	namesOf := func(list []tools.Tool) []string {
		var out []string
		for _, tl := range list {
			out = append(out, tl.Name())
		}
		return out
	}
	main, err := k.modelTools()
	if err != nil {
		t.Fatal(err)
	}
	mainNames := namesOf(main)
	for _, tl := range mounted {
		if !contains(mainNames, tl.Name()) {
			t.Errorf("main line lacks mounted tool %s", tl.Name())
		}
	}
	// code, desktop, and browser tools are lazy until loaded, for the main line
	// and subagents alike; that is not a main-line limit.
	for _, want := range []string{"dispatch", "write_file", "sh", "http_request", "send_email", "delete_file", "mcp_srv_post"} {
		if !contains(mainNames, want) {
			t.Errorf("main line lacks %s", want)
		}
	}
	general := namesOf(k.toolsFor(exec.StartRequest{ID: "sa_0123456789", Type: "general"}))
	for _, tl := range mounted {
		if handingOut[tl.Name()] {
			continue
		}
		if !contains(general, tl.Name()) {
			t.Errorf("general subagent cannot reach %s", tl.Name())
		}
	}
	for _, never := range []string{"dispatch", "spawn"} {
		if contains(general, never) {
			t.Errorf("subagent offers %s; it could hand work out again", never)
		}
	}
	research := namesOf(k.toolsFor(exec.StartRequest{ID: "sa_0123456789", Type: "research"}))
	for _, never := range []string{"write_file", "send_email", "delete_file", "mcp_srv_post",
		"code_start", "desktop_click", "browser_open", "http_request"} {
		if contains(research, never) {
			t.Errorf("research subagent offers %s", never)
		}
	}
	for _, want := range []string{"read_file", "sh", "web_search", "web_fetch", "view_image"} {
		if !contains(research, want) {
			t.Errorf("research subagent lacks %s: %v", want, research)
		}
	}
}

// A dispatched subagent reaches the main line twice, handed out and handed
// back: its progress stays in its own transcript.
func TestDispatchProgressStaysOffTheMainLine(t *testing.T) {
	k, _, box := dispatchKernel(t, &roleModel{})
	ch := make(chan exec.Event, 4)
	ch <- exec.Event{ID: "ag_p1", AgentID: "sa_00000001", Kind: exec.EvProgress, Text: "step 1"}
	ch <- exec.Event{ID: "ag_p2", AgentID: "sa_00000001", Kind: exec.EvProgress, Text: "step 2"}
	ch <- exec.Event{ID: "ag_d1", AgentID: "sa_00000001", Kind: exec.EvDone, Text: "**Changed**\n- a.go"}
	close(ch)
	k.forwardExec(ch)
	if err := box.Reload(); err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, ev := range box.Pending() {
		kinds = append(kinds, ev.Type)
	}
	if strings.Join(kinds, ",") != exec.EvDone {
		t.Fatalf("inbox got %v, want only the result", kinds)
	}
}

// A research and a dev subagent run side by side; a second dev in the same
// directory waits for the first.
func TestDevSubagentsOnOneDirectoryQueue(t *testing.T) {
	m := &roleModel{hold: make(chan struct{})}
	k, b, box := dispatchKernel(t, m)
	ctx := context.Background()
	var ids []string
	for _, ty := range []string{"dev", "research", "dev"} {
		out, err := k.dispatch(ctx, ty, "do it")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, strings.Fields(out)[1])
	}
	status := func(id string) string {
		meta, _, _ := exec.ReadStatus(b.AgentDir(id))
		return meta.Status
	}
	deadline := time.Now().Add(3 * time.Second)
	for (status(ids[0]) != "running" || status(ids[1]) != "running") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if status(ids[0]) != "running" || status(ids[1]) != "running" || status(ids[2]) != "queued" {
		t.Fatalf("statuses %s %s %s", status(ids[0]), status(ids[1]), status(ids[2]))
	}
	close(m.hold)
	for time.Now().Before(deadline.Add(5 * time.Second)) {
		_ = box.Reload()
		done := 0
		for _, ev := range box.Pending() {
			if ev.Source == link.SourceSubagent && ev.Type == exec.EvDone {
				done++
			}
		}
		if done == 3 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("subagents did not all finish")
}

func TestWriteResource(t *testing.T) {
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(repo, "pkg", "a")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	types := agenttype.Builtin()
	dev, _ := agenttype.Find(types, "dev")
	research, _ := agenttype.Find(types, "research")
	if got := writeResource(dev, sub); got != "repo:"+repo {
		t.Fatalf("dev in a checkout: %q", got)
	}
	// No checkout anywhere above it (test temp dirs sit inside this repo).
	plain := "/no-such-xbot-dir/work"
	if got := writeResource(dev, plain); got != "dir:"+plain {
		t.Fatalf("dev in a plain dir: %q", got)
	}
	if got := writeResource(research, sub); got != "" {
		t.Fatalf("research got a resource: %q", got)
	}
}

// A subagent that loads a lazy link mid-task then calls it: the executor
// rebuilds the tool set every step, so the tool the subagent just unlocked is
// really in its next request, not only in the returned name list.
func TestSubagentCallsWhatItLoadsMidTask(t *testing.T) {
	m := &roleModel{
		fore: []provider.Message{
			assistantCall("d1", "", "dispatch", `{"type":"general","task":"tell the owner the build passed"}`),
			{Role: provider.RoleAssistant, Content: provider.TextContent("好。")},
		},
		sub: []provider.Message{
			assistantCall("s1", "", "links", `{"action":"load","id":"telegram"}`),
			assistantCall("s2", "", "telegram_send", `{"to":"u1","body":"the build passed"}`),
			{Role: provider.RoleAssistant, Content: provider.TextContent("Done\nnone")},
		},
	}
	b := testBot(t, link.NameTerminal, link.NameTools, link.NameAgents, link.NameTelegram)
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	dir := bot.Dir(b.Root, b.Name())
	k := &Kernel{
		Bot: b, Provider: m, Memory: memory.NopStore{}, Inbox: box, MaxSteps: 8,
		Dispatch: true, WorkDir: t.TempDir(),
		Links: []link.Link{
			link.Terminal{}, link.NewToolsLinkAt(dir, ""), link.Agents{},
			link.NewConnector(dir, b.SecretPath(), link.NameTelegram),
		},
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "让子代理通知我构建过了", time.Unix(9, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	// The subagent runs on its own goroutine and writes meta.json only once it
	// has started, so look for the file inside the wait, not before it. Return
	// only after the subagent reached a terminal state; the test's temp dir is
	// about to be removed underneath it.
	pattern := filepath.Join(b.Root, "bots", b.Name(), "agents", "sa_*", "meta.json")
	deadline := time.Now().Add(5 * time.Second)
	status := ""
	for {
		metas, _ := filepath.Glob(pattern)
		for _, path := range metas {
			raw, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			var meta exec.Meta
			if json.Unmarshal(raw, &meta) != nil {
				continue
			}
			if meta.Status == "done" || meta.Status == "failed" || meta.Status == "cancelled" {
				status = meta.Status
			}
		}
		if status != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the subagent did not finish within 5s: no terminal meta.json under %s", pattern)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if status != "done" {
		t.Fatalf("the subagent ended as %s, want done", status)
	}
	// The foreground may already have acknowledged the terminal event. Wait
	// for its durable post, then synchronize with Post via the inbox lock.
	for {
		done := false
		for _, event := range inboxPosts(t, b.InboxPath(), "") {
			done = done || event.Source == link.SourceSubagent && event.Type == exec.EvDone
		}
		if done {
			if err := box.Reload(); err != nil {
				t.Fatal(err)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("subagent terminal event was not persisted")
		}
		time.Sleep(10 * time.Millisecond)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.subReq) < 2 {
		t.Fatalf("subagent made %d requests, want the load step and the send step", len(m.subReq))
	}
	first := toolNamesOf(m.subReq[0].Tools)
	if contains(first, "telegram_send") {
		t.Fatalf("the lazy link was mounted before the subagent loaded it: %v", first)
	}
	second := toolNamesOf(m.subReq[1].Tools)
	if !contains(second, "telegram_send") {
		t.Fatalf("after links(load telegram) the next step still cannot call telegram_send: %v", second)
	}
}
