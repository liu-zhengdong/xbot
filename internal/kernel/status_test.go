package kernel

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/inbox"
	"xbot/internal/memory"
	"xbot/internal/provider"
)

// blockProvider signals when a model call starts, then waits for release.
type blockProvider struct {
	started chan struct{}
	release chan struct{}
	reply   string
}

func (p *blockProvider) Chat(context.Context, provider.Request, func(string)) (provider.Message, error) {
	close(p.started)
	<-p.release
	return provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent(p.reply)}, nil
}

func TestBotStatusTransitions(t *testing.T) {
	b := testBot(t, inbox.NameTerminal)
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	p := &blockProvider{started: make(chan struct{}), release: make(chan struct{}), reply: "hello"}
	k := &Kernel{Bot: b, Provider: p, Memory: memory.NopStore{}, MaxSteps: 2}
	if got := k.Status().State; got != StateIdle {
		t.Fatalf("start state %q", got)
	}
	done := make(chan error, 1)
	go func() {
		done <- k.Handle(context.Background(), inbox.TerminalEvent("u1", "hi", time.Unix(10, 0).UTC()))
	}()
	<-p.started
	if got := k.Status().State; got != StateRunning {
		t.Fatalf("mid state %q", got)
	}
	close(p.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	st := k.Status()
	if st.State != StateIdle {
		t.Fatalf("end state %q", st.State)
	}
	if st.LastMessage != "hello" {
		t.Fatalf("last message %q", st.LastMessage)
	}
}

func TestStepsRingCapAndRedaction(t *testing.T) {
	b := testBot(t, inbox.NameTerminal)
	k := &Kernel{Bot: b, Memory: memory.NopStore{}, Redact: func(s string) string {
		return strings.ReplaceAll(s, "SECRET", "[secret]")
	}}
	turnObj := &turn{started: time.Unix(1, 0).UTC()}
	k.turnMu.Lock()
	k.turn = turnObj
	k.turnMu.Unlock()
	for i := 0; i < stepRingCap+5; i++ {
		k.beginStep(turnObj, "tool-call", "tool", "SECRET value "+strings.Repeat("x", 200))
		k.endStep(turnObj, "tool-call", "ok")
	}
	steps := k.Steps(stepRingCap)
	if len(steps) != stepRingCap {
		t.Fatalf("ring cap %d", len(steps))
	}
	for _, s := range steps {
		if strings.Contains(s.Summary, "SECRET") {
			t.Fatalf("secret leaked: %q", s.Summary)
		}
		if n := len([]rune(s.Summary)); n > stepSummaryCap {
			t.Fatalf("summary not clipped: %d", n)
		}
	}
	if steps[len(steps)-1].State != StepDone {
		t.Fatalf("last step %q", steps[len(steps)-1].State)
	}
}

func TestStepErrorState(t *testing.T) {
	k := &Kernel{}
	turnObj := &turn{}
	k.turnMu.Lock()
	k.turn = turnObj
	k.turnMu.Unlock()
	k.beginStep(turnObj, "boom-call", "boom", "run it")
	k.endStep(turnObj, "boom-call", "error: tool exploded")
	steps := k.Steps(1)
	if len(steps) != 1 || steps[0].State != StepError {
		t.Fatalf("steps %+v", steps)
	}
	k.beginStep(turnObj, "shell-call", "shell", "curl")
	k.endStep(turnObj, "shell-call", "exit 35\ncurl: (35) TLS connect error")
	if steps := k.Steps(1); len(steps) != 1 || steps[0].State != StepError {
		t.Fatalf("failed shell exit shown as %+v", steps)
	}
}

func TestAwaitingState(t *testing.T) {
	b := testBot(t, inbox.NameTerminal)
	k := &Kernel{Bot: b, Memory: memory.NopStore{}}
	turnObj := &turn{started: time.Now().UTC()}
	k.beginStep(turnObj, "shell-call", "shell", "rm -rf x")
	k.beginAwait(turnObj, "shell-call")
	k.turnMu.Lock()
	k.turn = turnObj
	k.turnMu.Unlock()
	st := k.Status()
	if st.State != StateAwaiting || st.Since == "" {
		t.Fatalf("awaiting status %+v", st)
	}
	if steps := k.Steps(1); len(steps) != 1 || steps[0].State != StepAwaiting {
		t.Fatalf("step %+v", steps)
	}
	k.endAwait(turnObj)
	steps := k.Steps(1)
	if len(steps) != 1 || steps[0].State != StepRunning {
		t.Fatalf("after end await %+v", steps)
	}
}

func TestIdleStatusTailPreviewRedacted(t *testing.T) {
	root := t.TempDir()
	b, err := bot.Create(root, "ada", "p", []string{inbox.NameTerminal})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Append(bot.Entry{Time: time.Unix(5, 0).UTC(), Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("<environment_update>ignore</environment_update>")}}); err != nil {
		t.Fatal(err)
	}
	long := "SECRETTOKEN " + strings.Repeat("字", 200)
	if err := b.Append(bot.Entry{Time: time.Unix(6, 0).UTC(), Message: provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent(long)}}); err != nil {
		t.Fatal(err)
	}
	old := bot.SetRedactText(func(s string) string { return strings.ReplaceAll(s, "SECRETTOKEN", "[redacted]") })
	defer old()
	st := IdleStatus(b, time.Unix(9, 0).UTC())
	if st.State != StateIdle {
		t.Fatalf("state %q", st.State)
	}
	if strings.Contains(st.LastMessage, "SECRETTOKEN") {
		t.Fatalf("not redacted: %q", st.LastMessage)
	}
	if n := len([]rune(st.LastMessage)); n > statusPreview {
		t.Fatalf("preview %d runes", n)
	}
	if !strings.HasPrefix(st.LastMessage, "[redacted]") {
		t.Fatalf("preview %q", st.LastMessage)
	}
}

func TestBotInfoHandlersSameOwner(t *testing.T) {
	root := t.TempDir()
	ada, err := bot.Create(root, "ada", "p", []string{inbox.NameTerminal})
	if err != nil {
		t.Fatal(err)
	}
	if err := ada.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	bob, err := bot.Create(root, "bob", "p", []string{inbox.NameTerminal})
	if err != nil {
		t.Fatal(err)
	}
	if err := bob.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	if err := bob.Append(bot.Entry{Time: time.Now(), Message: provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("bob here")}}); err != nil {
		t.Fatal(err)
	}
	k := &Kernel{Bot: ada, Memory: memory.NopStore{}}
	handlers := k.BotHandlers()
	out, err := handlers.Info(context.Background(), cliops.BotInfoQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Bots == nil || len(*out.Bots) != 2 || (*out.Bots)[0].Bot != "ada" || (*out.Bots)[1].Bot != "bob" || (*out.Bots)[1].LastMessage != "bob here" {
		t.Fatalf("status %+v", out)
	}
	out, err = handlers.Info(context.Background(), cliops.BotInfoQuery{Bot: "bob", Steps: 3})
	if err != nil {
		t.Fatal(err)
	}
	if out.Bots == nil || len(*out.Bots) != 1 || (*out.Bots)[0].Bot != "bob" || out.Steps == nil || len(*out.Steps) != 0 {
		t.Fatalf("filtered %+v", out)
	}
	if _, err := handlers.Info(context.Background(), cliops.BotInfoQuery{Bot: "ghost"}); err == nil {
		t.Fatal("unknown bot accepted")
	}
	other, err := bot.Create(root, "zed", "p", []string{inbox.NameTerminal})
	if err != nil {
		t.Fatal(err)
	}
	if err := other.SetOwners([]string{"u2"}); err != nil {
		t.Fatal(err)
	}
	if _, err := handlers.Info(context.Background(), cliops.BotInfoQuery{Bot: "zed"}); err == nil {
		t.Fatal("other owner's bot visible")
	}
}

type botInfoStatusSource struct {
	statuses []BotStatus
	name     string
	limit    int
	calls    int
}

func (s *botInfoStatusSource) BotStatuses() []BotStatus { return s.statuses }
func (s *botInfoStatusSource) BotSteps(name string, n int) []Step {
	s.name, s.limit = name, n
	s.calls++
	return []Step{{Time: time.Unix(12, 0), Tool: "recall_history", Summary: "inspect history", State: StepDone}}
}

func TestBotInfoHandlersUseLateStatusSourceAndNamedSteps(t *testing.T) {
	k := &Kernel{Bot: testBot(t, inbox.NameTerminal)}
	handlers := k.BotHandlers()
	// The host attaches its status source after publishing the listener. Its
	// owner view can contain a bot outside the source's fallback owner view.
	source := &botInfoStatusSource{statuses: []BotStatus{{Bot: "host-only", State: StateRunning}, {Bot: "second", State: StateIdle}}}
	k.Statuses = source
	all, err := handlers.Info(context.Background(), cliops.BotInfoQuery{Steps: 20})
	if err != nil || all.Bots == nil || len(*all.Bots) != 2 || (*all.Bots)[0].Bot != "host-only" || source.calls != 0 {
		t.Fatalf("host all %+v, step calls %d: %v", all, source.calls, err)
	}
	named, err := handlers.Info(context.Background(), cliops.BotInfoQuery{Bot: "host-only", Steps: 20})
	if err != nil || named.Bots == nil || len(*named.Bots) != 1 || source.name != "host-only" || source.limit != 10 || source.calls != 1 || named.Steps == nil || len(*named.Steps) != 1 {
		t.Fatalf("named %+v, source %+v: %v", named, source, err)
	}
	step := (*named.Steps)[0]
	if step.Tool != "recall_history" || step.State != StepDone || step.Summary != "inspect history" || !step.Time.Equal(time.Unix(12, 0)) {
		t.Fatalf("step %+v", step)
	}
	source.statuses = source.statuses[:1]
	if _, err := handlers.Info(context.Background(), cliops.BotInfoQuery{Steps: 3}); err != nil || source.name != "host-only" || source.limit != 3 || source.calls != 2 {
		t.Fatalf("singleton steps %+v: %v", source, err)
	}
	if _, err := handlers.Info(context.Background(), cliops.BotInfoQuery{Bot: "host-only", Steps: 0}); err != nil || source.calls != 2 {
		t.Fatalf("zero steps called source: %+v %v", source, err)
	}
}

func TestToolSummaryKeepsDescriptionUntilTurnEnds(t *testing.T) {
	b := testBot(t, inbox.NameTerminal)
	now := time.Now().Truncate(time.Second)
	k := &Kernel{Bot: b, Memory: memory.NopStore{}, Now: func() time.Time { return now }, Redact: func(s string) string { return strings.ReplaceAll(s, "SECRET", "[secret]") }}
	tr := &turn{started: now.Add(-time.Minute)}
	k.turn = tr
	k.beginStepDisplay(tr, "shell-call", "shell", "curl SECRET", "检查 SECRET\nprivate command")
	if got := k.Status().ToolSummary; got != "检查 [secret]" {
		t.Fatalf("tool summary = %q", got)
	}
	k.endStep(tr, "shell-call", "ok")
	if st := k.Status(); st.State != StateRunning || st.Tool != "" || st.ToolSummary != "检查 [secret]" || st.Since != statusStamp(tr.started) {
		t.Fatalf("model wait lost description or kept a running tool: %+v", st)
	}
	// Empty descriptions must not erase progress, even after history is trimmed.
	for i := 0; i < stepRingCap+1; i++ {
		id := fmt.Sprint(i)
		k.beginStepDisplay(tr, id, "read_file", "read /private/file", "")
		if st := k.Status(); st.Tool != "read_file" || st.ToolSummary != "检查 [secret]" {
			t.Fatalf("empty description erased progress: %+v", st)
		}
		k.endStep(tr, id, "ok")
	}
	if got := k.Status().ToolSummary; got != "检查 [secret]" {
		t.Fatalf("trimmed history lost description: %q", got)
	}
	tr.finished = true
	if st := k.Status(); st.Tool != "" || st.ToolSummary != "" {
		t.Fatalf("finished turn still has progress: %+v", st)
	}
	k.turn = &turn{started: now}
	if got := k.Status().ToolSummary; got != "" {
		t.Fatalf("new turn inherited description: %q", got)
	}
}

func TestSlowToolSurvivesParallelCompletions(t *testing.T) {
	b := testBot(t, inbox.NameTerminal)
	k := &Kernel{Bot: b, Memory: memory.NopStore{}}
	tr := &turn{started: time.Now().Add(-time.Minute)}
	k.turn = tr
	k.beginStep(tr, "shell-call", "shell", "sleep 5")
	k.beginStep(tr, "read_file-call", "read_file", "config.json")
	k.endStep(tr, "read_file-call", "ok")
	if st := k.Status(); st.Tool != "shell" || st.ToolSummary != "sleep 5" {
		t.Fatalf("quick parallel result hid slow call: %+v", st)
	}
}

func TestSameNameCallsFinishByIDAndKeepTheirStartTime(t *testing.T) {
	b := testBot(t, inbox.NameTerminal)
	now := time.Now().Truncate(time.Second)
	k := &Kernel{Bot: b, Memory: memory.NopStore{}, Now: func() time.Time { return now }}
	tr := &turn{started: now.Add(-time.Minute)}
	k.turn = tr
	k.beginStep(tr, "slow", "shell", "sleep 5")
	since := k.Status().Since
	now = now.Add(time.Second)
	k.beginStep(tr, "fast", "shell", "echo done")
	k.endStep(tr, "fast", "error: quick failure")
	st := k.Status()
	if st.ToolSummary != "sleep 5" || st.Since != since {
		t.Fatalf("same-name result replaced live call: %+v", st)
	}
	steps := k.Steps(20)
	if steps[0].State != StepRunning || steps[1].State != StepError {
		t.Fatalf("steps = %+v", steps)
	}
	k.endStep(tr, "slow", "ok")
	if st := k.Status(); st.Tool != "" || st.ToolSummary != "echo done" {
		t.Fatalf("finished tool or last description incorrect: %+v", st)
	}
}

func TestLiveCallsSurviveTheStepHistoryLimit(t *testing.T) {
	b := testBot(t, inbox.NameTerminal)
	k := &Kernel{Bot: b, Memory: memory.NopStore{}}
	tr := &turn{started: time.Now()}
	k.turn = tr
	for i := 0; i < stepRingCap+5; i++ {
		k.beginStep(tr, fmt.Sprint(i), "shell", fmt.Sprint("call ", i))
	}
	if len(tr.steps) != stepRingCap+5 || len(k.Steps(100)) != stepRingCap {
		t.Fatal("live calls dropped or public history unbounded")
	}
	for i := stepRingCap + 4; i > 0; i-- {
		k.endStep(tr, fmt.Sprint(i), "ok")
	}
	if st := k.Status(); st.ToolSummary != "call 0" {
		t.Fatalf("oldest live call lost: %+v", st)
	}
	if len(tr.steps) != stepRingCap {
		t.Fatalf("finished history not trimmed: %d", len(tr.steps))
	}
	k.endStep(tr, "0", "ok")
	if st := k.Status(); st.Tool != "" || st.ToolSummary != fmt.Sprint("call ", stepRingCap+4) {
		t.Fatalf("finished call or last description incorrect: %+v", st)
	}
}

func TestStatusDuringExecutorPublication(t *testing.T) {
	k := &Kernel{Bot: testBot(t, inbox.NameTerminal), Memory: memory.NopStore{}}
	done := make(chan struct{})
	go func() { defer close(done); k.ensureExec() }()
	for i := 0; i < 50; i++ {
		k.Status()
	}
	<-done
	if k.execSnapshot() == nil {
		t.Fatal("executor was not published")
	}
}

func TestToolSummaryKeepsOnlyTheDescriptionLine(t *testing.T) {
	b := testBot(t, inbox.NameTerminal)
	k := &Kernel{Bot: b, Memory: memory.NopStore{}}
	tr := &turn{started: time.Now()}
	k.turn = tr
	k.beginStep(tr, "shell-call", "shell", "添加一条记忆\nxbot memory add hi")
	if got := k.Status().ToolSummary; got != "添加一条记忆" {
		t.Fatalf("tool summary = %q", got)
	}
}

func TestToolDisplayKeepsConfirmSummaryOutOfStatus(t *testing.T) {
	b := testBot(t, inbox.NameTerminal)
	k := &Kernel{Bot: b, Memory: memory.NopStore{}}
	tr := &turn{started: time.Now()}
	k.turn = tr
	k.beginStepDisplay(tr, "read", "read_file", "read /private/file", "")
	if got := k.Status().ToolSummary; got != "" {
		t.Fatalf("empty description: %q", got)
	}
	if got := k.Steps(1)[0].Summary; got != "read /private/file" {
		t.Fatalf("step summary lost: %q", got)
	}
	k.endStep(tr, "read", "ok")
	k.beginStepDisplay(tr, "shell", "shell", "检查资料\ncommand", "检查资料\nmore")
	if got := k.Status().ToolSummary; got != "检查资料" {
		t.Fatalf("description: %q", got)
	}
}
