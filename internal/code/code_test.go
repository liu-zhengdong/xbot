package code

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/inbox"
	"xbot/internal/link"
)

func TestRecordQueueCancelAndFinish(t *testing.T) {
	root := t.TempDir()
	b, err := bot.Create(root, "ada", "persona", []string{link.NameCoding})
	if err != nil {
		t.Fatal(err)
	}
	l := Open(bot.Dir(b.Root, b.Name()), b.InboxPath())
	rec := l.Record()
	rec.Now = func() time.Time { return time.Unix(100, 0).UTC() }
	work := t.TempDir()
	first, err := rec.Start(Task{WorkDir: work, Repo: "local/repo", MaxSteps: 40, TimeoutSec: 3600, Goal: "fix the test"})
	if err != nil || first.Status != StatusRunning || !strings.HasPrefix(first.ID, "ca_") {
		t.Fatalf("%+v %v", first, err)
	}
	second, err := rec.Start(Task{WorkDir: work, MaxSteps: 10, TimeoutSec: 60, Goal: "then the docs"})
	if err != nil || second.Status != StatusQueued {
		t.Fatalf("%+v %v", second, err)
	}
	if _, err := rec.Control(second.ID, KindSteer, "too soon"); err == nil {
		t.Fatal("steered a queued task")
	}
	steered, err := rec.Control(first.ID, KindSteer, "keep the helper")
	if err != nil || len(steered.Notes) != 1 || steered.Notes[0].Kind != KindSteer {
		t.Fatalf("%+v %v", steered, err)
	}
	if _, err := rec.Control(first.ID, KindQueue, "run lint after"); err != nil {
		t.Fatal(err)
	}
	if _, err := rec.Control(first.ID, KindInterrupt, "stop the rename"); err != nil {
		t.Fatal(err)
	}
	if _, err := rec.Start(Task{WorkDir: "relative", MaxSteps: 1, TimeoutSec: 1, Goal: "no"}); err == nil {
		t.Fatal("relative work dir")
	}
	if _, err := rec.Start(Task{WorkDir: work, MaxSteps: 0, TimeoutSec: 1, Goal: "no"}); err == nil {
		t.Fatal("default step cap")
	}

	cancelled, err := rec.Control(first.ID, KindCancel, "")
	if err != nil || cancelled.Status != StatusCancelled {
		t.Fatalf("%+v %v", cancelled, err)
	}
	promoted, err := rec.Get(second.ID)
	if err != nil || promoted.Status != StatusRunning {
		t.Fatalf("%+v %v", promoted, err)
	}
	done, err := rec.Finish(second.ID, "docs updated", false)
	if err != nil || done.Status != StatusDone {
		t.Fatalf("%+v %v", done, err)
	}
	if _, err := rec.Finish(second.ID, "again", false); err == nil {
		t.Fatal("finished twice")
	}

	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	pending := box.Pending()
	if len(pending) != 2 {
		t.Fatalf("events %d", len(pending))
	}
	if pending[0].Source != link.NameCoding || pending[0].Type != StatusCancelled || pending[0].Sender != first.ID || !pending[0].Untrusted {
		t.Fatalf("%+v", pending[0])
	}
	if pending[1].Type != StatusDone || !strings.Contains(pending[1].Content, "docs updated") || !strings.Contains(pending[1].Content, second.ID) {
		t.Fatalf("%+v", pending[1])
	}
	raw, err := os.ReadFile(filepath.Join(bot.Dir(b.Root, b.Name()), "code", first.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(bot.Dir(b.Root, b.Name()), "code", first.ID+".json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("mode %v %v", info, err)
	}
	if !strings.Contains(string(raw), "local/repo") || !strings.Contains(string(raw), "keep the helper") {
		t.Fatal(string(raw))
	}
}

func TestCodeToolsRoundTrip(t *testing.T) {
	root := t.TempDir()
	b, err := bot.Create(root, "ada", "persona", []string{link.NameCoding})
	if err != nil {
		t.Fatal(err)
	}
	l := Open(bot.Dir(b.Root, b.Name()), b.InboxPath())
	byName := map[string]interface {
		Run(context.Context, json.RawMessage) (string, error)
	}{}
	for _, tool := range l.Tools() {
		byName[tool.Name()] = tool
	}
	work := t.TempDir()
	args, _ := json.Marshal(map[string]any{
		"work_dir": work, "repo": "git@example", "max_steps": 12, "timeout_sec": 90, "goal": "repair the build",
	})
	out, err := byName["code_start"].Run(context.Background(), args)
	if err != nil || !strings.HasPrefix(out, "running ca_") {
		t.Fatalf("%q %v", out, err)
	}
	id := strings.TrimPrefix(out, "running ")
	note, _ := json.Marshal(map[string]string{"id": id, "text": "also the makefile"})
	if out, err = byName["code_steer"].Run(context.Background(), note); err != nil || out != "steer "+id {
		t.Fatalf("%q %v", out, err)
	}
	shown, err := byName["code_show"].Run(context.Background(), json.RawMessage(`{"id":"`+id+`"}`))
	if err != nil || !strings.Contains(shown, "repair the build") || !strings.Contains(shown, "also the makefile") || !strings.Contains(shown, work) {
		t.Fatalf("%q %v", shown, err)
	}
	listed, err := byName["code_show"].Run(context.Background(), json.RawMessage(`{}`))
	if err != nil || !strings.Contains(listed, id) || !strings.Contains(listed, "running") {
		t.Fatalf("%q %v", listed, err)
	}
	failed, err := l.Record().Finish(id, "no compiler", true)
	if err != nil || failed.Status != StatusFailed {
		t.Fatalf("%+v %v", failed, err)
	}
	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	pending := box.Pending()
	if len(pending) != 1 || pending[0].Type != StatusFailed || !strings.Contains(pending[0].Content, "no compiler") {
		t.Fatalf("%+v", pending)
	}
}
