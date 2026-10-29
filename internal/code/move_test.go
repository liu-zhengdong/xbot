package code

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/inbox"
)

func TestMoveKeepsRunningTaskAndDelivery(t *testing.T) {
	root := t.TempDir()
	oldDir, newDir := filepath.Join(root, "old"), filepath.Join(root, "new")
	if err := os.MkdirAll(oldDir, 0700); err != nil {
		t.Fatal(err)
	}
	marker, release := filepath.Join(root, "started"), filepath.Join(root, "release")
	script := filepath.Join(root, "agent")
	body := "#!/bin/sh\ntouch '" + marker + "'\nwhile [ ! -f '" + release + "' ]; do sleep 0.02; done\necho complete\n"
	if err := os.WriteFile(script, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	l := Open(oldDir, filepath.Join(oldDir, "inbox.jsonl"))
	l.UseCommand(script)
	l.UseTranscript(func(s string) error { return os.WriteFile(filepath.Join(oldDir, "card"), []byte(s), 0600) })
	task, err := l.rec.Start(Task{WorkDir: root, Goal: "test", MaxSteps: 2, TimeoutSec: 10})
	if err != nil {
		t.Fatal(err)
	}
	l.launch(task)
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("task did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	err = l.Move(newDir, filepath.Join(newDir, "inbox.jsonl"), func(s string) error { return os.WriteFile(filepath.Join(newDir, "card"), []byte(s), 0600) }, func() error { return os.Rename(oldDir, newDir) })
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(release, nil, 0600); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(3 * time.Second)
	for {
		got, err := l.rec.Get(task.ID)
		if err == nil && got.Status == StatusDone {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("task did not finish: %+v %v", got, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	box, err := inbox.Open(filepath.Join(newDir, "inbox.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(3 * time.Second)
	for len(box.Pending()) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("completion was not delivered")
		}
		time.Sleep(10 * time.Millisecond)
		box, err = inbox.Open(filepath.Join(newDir, "inbox.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
	}
	card, err := os.ReadFile(filepath.Join(newDir, "card"))
	if err != nil || !strings.Contains(string(card), "complete") {
		t.Fatalf("card %s %v", card, err)
	}
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Fatalf("old directory recreated: %v", err)
	}
}
