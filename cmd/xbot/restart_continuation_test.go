package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/exec"
	"xbot/internal/inbox"
	"xbot/internal/link"
)

func TestRestartContinuationPersistsAndWakesEveryWorkingBot(t *testing.T) {
	root := t.TempDir()
	makeBot := func(name string) *bot.Bot {
		t.Helper()
		b, err := bot.Create(root, name, "persona", []string{link.NameTerminal})
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	front := makeBot("front")
	running := makeBot("running")
	queued := makeBot("queued")
	idle := makeBot("idle")
	frontLock, err := front.LockHandle()
	if err != nil {
		t.Fatal(err)
	}
	defer frontLock.Release()

	for _, status := range []string{"running", "queued"} {
		b := running
		if status == "queued" {
			b = queued
		}
		dir := b.AgentDir("sa_test")
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		meta, err := json.Marshal(exec.Meta{ID: "sa_test", Status: status, Task: "continue"})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "meta.json"), meta, 0600); err != nil {
			t.Fatal(err)
		}
	}
	var restartReason string
	if err := requestBotRestart(root, "继续检查设备同步", func(reason string) { restartReason = reason }); err != nil {
		t.Fatal(err)
	}
	if restartReason != "Bot requested restart" {
		t.Fatalf("restart signal %q", restartReason)
	}
	if _, err := os.Stat(restartContinuationPath(root)); err != nil {
		t.Fatalf("continuation not persisted before restart: %v", err)
	}
	persisted, err := os.ReadFile(restartContinuationPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if err := deliverRestartContinuation(root); err != nil {
		t.Fatal(err)
	}
	for _, b := range []*bot.Bot{front, running, queued} {
		box, err := inbox.Open(b.InboxPath())
		if err != nil {
			t.Fatal(err)
		}
		pending := box.Pending()
		if len(pending) != 1 || pending[0].Source != "restart" || pending[0].Type != link.TypeMessage || !pending[0].Untrusted || !strings.Contains(pending[0].Content, "XBot 已重启") || !strings.Contains(pending[0].Content, "用户的续跑提示：继续检查设备同步") {
			t.Fatalf("%s continuation event = %+v", b.Name(), pending)
		}
	}
	box, err := inbox.Open(idle.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	if got := box.Pending(); len(got) != 0 {
		t.Fatalf("idle Bot received restart event: %+v", got)
	}
	// Simulate a crash after inbox posts but before the pending file is removed.
	if err := os.WriteFile(restartContinuationPath(root), persisted, 0600); err != nil {
		t.Fatal(err)
	}
	if err := deliverRestartContinuation(root); err != nil {
		t.Fatal(err)
	}
	for _, b := range []*bot.Bot{front, running, queued} {
		box, err := inbox.Open(b.InboxPath())
		if err != nil {
			t.Fatal(err)
		}
		if got := box.Pending(); len(got) != 1 {
			t.Fatalf("retry duplicated %s continuation: %+v", b.Name(), got)
		}
	}
}
