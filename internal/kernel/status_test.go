package kernel

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/link"
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
	return provider.Message{Role: provider.RoleAssistant, Content: p.reply}, nil
}

func TestBotStatusTransitions(t *testing.T) {
	b := testBot(t, link.NameTerminal)
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
		done <- k.Handle(context.Background(), link.TerminalEvent("u1", "hi", time.Unix(10, 0).UTC()))
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
	b := testBot(t, link.NameTerminal)
	k := &Kernel{Bot: b, Memory: memory.NopStore{}, Redact: func(s string) string {
		return strings.ReplaceAll(s, "SECRET", "[secret]")
	}}
	turnObj := &turn{started: time.Unix(1, 0).UTC()}
	k.turnMu.Lock()
	k.turn = turnObj
	k.turnMu.Unlock()
	for i := 0; i < stepRingCap+5; i++ {
		k.beginStep(turnObj, "tool", "SECRET value "+strings.Repeat("x", 200))
		k.endStep(turnObj, "tool", "ok")
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
	k.beginStep(turnObj, "boom", "run it")
	k.endStep(turnObj, "boom", "error: tool exploded")
	steps := k.Steps(1)
	if len(steps) != 1 || steps[0].State != StepError {
		t.Fatalf("steps %+v", steps)
	}
	k.beginStep(turnObj, "shell", "curl")
	k.endStep(turnObj, "shell", "exit 35\ncurl: (35) TLS connect error")
	if steps := k.Steps(1); len(steps) != 1 || steps[0].State != StepError {
		t.Fatalf("failed shell exit shown as %+v", steps)
	}
}

func TestAwaitingState(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	k := &Kernel{Bot: b, Memory: memory.NopStore{}}
	turnObj := &turn{started: time.Now().UTC()}
	k.beginStep(turnObj, "shell", "rm -rf x")
	k.beginAwait(turnObj, "shell")
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
	b, err := bot.Create(root, "ada", "p", []string{link.NameTerminal})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Append(bot.Entry{Time: time.Unix(5, 0).UTC(), Message: provider.Message{Role: provider.RoleUser, Content: "<environment_update>ignore</environment_update>"}}); err != nil {
		t.Fatal(err)
	}
	long := "SECRETTOKEN " + strings.Repeat("字", 200)
	if err := b.Append(bot.Entry{Time: time.Unix(6, 0).UTC(), Message: provider.Message{Role: provider.RoleAssistant, Content: long}}); err != nil {
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

func TestBotStatusToolSameOwner(t *testing.T) {
	root := t.TempDir()
	ada, err := bot.Create(root, "ada", "p", []string{link.NameTerminal})
	if err != nil {
		t.Fatal(err)
	}
	if err := ada.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	bob, err := bot.Create(root, "bob", "p", []string{link.NameTerminal})
	if err != nil {
		t.Fatal(err)
	}
	if err := bob.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	if err := bob.Append(bot.Entry{Time: time.Now(), Message: provider.Message{Role: provider.RoleAssistant, Content: "bob here"}}); err != nil {
		t.Fatal(err)
	}
	k := &Kernel{Bot: ada, Memory: memory.NopStore{}}
	out, err := (botStatusTool{k}).Run(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "ada") || !strings.Contains(out, "bob") || !strings.Contains(out, "bob here") {
		t.Fatalf("status %q", out)
	}
	out, err = (botStatusTool{k}).Run(context.Background(), json.RawMessage(`{"bot":"bob","steps":3}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "ada") || !strings.Contains(out, "bob") {
		t.Fatalf("filtered %q", out)
	}
	if _, err := (botStatusTool{k}).Run(context.Background(), json.RawMessage(`{"bot":"ghost"}`)); err == nil {
		t.Fatal("unknown bot accepted")
	}
	other, err := bot.Create(root, "zed", "p", []string{link.NameTerminal})
	if err != nil {
		t.Fatal(err)
	}
	if err := other.SetOwners([]string{"u2"}); err != nil {
		t.Fatal(err)
	}
	if _, err := (botStatusTool{k}).Run(context.Background(), json.RawMessage(`{"bot":"zed"}`)); err == nil {
		t.Fatal("other owner's bot visible")
	}
}
