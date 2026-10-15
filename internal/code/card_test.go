package code

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestFakeCLIStreamsCardAndStops(t *testing.T) {
	t.Setenv("XBOT_API_KEY", "sk-provider")
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-agent")
	body := "#!/bin/sh\nif [ -n \"$XBOT_API_KEY\" ]; then echo leaked; exit 2; fi\necho alpha\necho beta >&2\n"
	if err := os.WriteFile(script, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	l, work := testLink(t)
	var mu sync.Mutex
	var cards []string
	l.UseTranscript(func(text string) error {
		mu.Lock()
		cards = append(cards, text)
		mu.Unlock()
		return nil
	})
	l.UseCommand(script)
	args := json.RawMessage(`{"work_dir":"` + work + `","max_steps":2,"timeout_sec":10,"goal":"ship the widget"}`)
	need, summary, err := (startTool{l}).Confirm(args)
	if err != nil || !need || !strings.Contains(summary, work) || !strings.Contains(summary, "ship the widget") || !strings.Contains(summary, "edits files") {
		t.Fatalf("confirm %v %q %v", need, summary, err)
	}
	if _, err := toolNamed(l, "code_start").Run(context.Background(), args); err != nil {
		t.Fatal(err)
	}
	ev := waitCode(t, l, 1)
	if ev[0].Type != StatusDone || strings.Contains(ev[0].Content, "leaked") || !strings.Contains(ev[0].Content, "alpha") || !strings.Contains(ev[0].Content, "beta") {
		t.Fatalf("%+v", ev[0])
	}
	mu.Lock()
	joined := strings.Join(cards, "\n")
	mu.Unlock()
	if !strings.Contains(joined, "alpha") || !strings.Contains(joined, "beta") || !strings.Contains(joined, `status="done"`) || strings.Contains(joined, "sk-provider") {
		t.Fatal(joined)
	}
	if !IsCard(cards[0]) {
		t.Fatal(cards[0])
	}

	slow := filepath.Join(dir, "slow-agent")
	if err := os.WriteFile(slow, []byte("#!/bin/sh\necho first\nsleep 30\n"), 0700); err != nil {
		t.Fatal(err)
	}
	l2, work2 := testLink(t)
	var mu2 sync.Mutex
	var live []string
	l2.UseTranscript(func(text string) error {
		mu2.Lock()
		live = append(live, text)
		mu2.Unlock()
		return nil
	})
	l2.UseCommand(slow)
	id := strings.TrimPrefix(mustStart(t, l2, work2, 2, 30, "wait"), "running ")
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu2.Lock()
		got := strings.Join(live, "\n")
		mu2.Unlock()
		if strings.Contains(got, "first") && strings.Contains(got, `status="running"`) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu2.Lock()
	got := strings.Join(live, "\n")
	mu2.Unlock()
	if !strings.Contains(got, "first") {
		t.Fatal(got)
	}
	show, err := toolNamed(l2, "code_show").Run(context.Background(), json.RawMessage(`{"id":"`+id+`"}`))
	if err != nil || !strings.Contains(show, "first") {
		t.Fatalf("show %q %v", show, err)
	}
	cancel, _ := json.Marshal(map[string]string{"id": id})
	if _, err := toolNamed(l2, "code_cancel").Run(context.Background(), cancel); err != nil {
		t.Fatal(err)
	}
	ev = waitCode(t, l2, 1)
	if ev[0].Type != StatusCancelled {
		t.Fatalf("%+v", ev[0])
	}
	waitIdle(t, l)
	waitIdle(t, l2)
}

func TestKnownCLIArgs(t *testing.T) {
	if got := strings.Join(commandArgs("/usr/bin/claude", "fix it"), " "); got != "-p fix it" {
		t.Fatal(got)
	}
	if got := strings.Join(commandArgs("/usr/bin/cursor-agent", "fix it"), " "); got != "-p fix it" {
		t.Fatal(got)
	}
	if got := strings.Join(commandArgs("/usr/bin/codex", "fix it"), " "); got != "exec fix it" {
		t.Fatal(got)
	}
	if got := strings.Join(commandArgs("/tmp/fake-agent", "fix it"), " "); got != "fix it" {
		t.Fatal(got)
	}
}
