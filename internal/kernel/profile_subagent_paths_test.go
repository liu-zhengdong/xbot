package kernel

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/exec"
	"xbot/internal/link"
	"xbot/internal/mcp"
	"xbot/internal/work"
)

func TestProfileSubagentParentEventUsesCurrentInboxAndUnlockedWake(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	root := t.TempDir()
	original, err := bot.Create(root, "before", "duty", nil)
	if err != nil {
		t.Fatal(err)
	}
	manager := mcp.NewManager(ctx, mcp.Options{})
	defer manager.Close()
	resources, err := NewBotResources(ctx, original, manager)
	if err != nil {
		t.Fatal(err)
	}
	defer resources.Close()
	k := &Kernel{Bot: original, Resources: resources, Inbox: resources.box, Dispatch: true, WorkDir: work.BotDir(root, "before")}
	if err := resources.RenameTo("after"); err != nil {
		t.Fatal(err)
	}
	woken := ""
	SetWaker(root, func(current *bot.Bot) {
		if !resources.route.TryLock() {
			t.Error("wake called while route held")
			return
		}
		resources.route.Unlock()
		woken = current.Name()
	})
	defer SetWaker(root, nil)
	events := make(chan exec.Event, 1)
	events <- exec.Event{ID: "subagent:parent-note:1", AgentID: "sa_123456789abcdef0", Kind: link.TypeMessage, Text: "same child after move", TurnSeq: 1}
	close(events)
	k.forwardExec(events)
	if woken != "after" {
		t.Fatalf("wake used stale source: %q", woken)
	}
	pending := resources.box.Pending()
	if len(pending) != 1 || pending[0].ID != "subagent:parent-note:1" || pending[0].Content != "same child after move" {
		t.Fatalf("parent event not in current inbox: %+v", pending)
	}
	if status := k.Status(); status.Bot != "after" {
		t.Fatalf("status identity stale: %+v", status)
	}
	if snapshot := k.ContextSnapshot(); snapshot.Bot != "after" {
		t.Fatalf("context identity stale: %+v", snapshot)
	}
	if _, err := os.Stat(bot.Dir(root, "before")); !os.IsNotExist(err) {
		t.Fatalf("source event revived old directory: %v", err)
	}
}

func TestProfileRecoveryUsesCurrentTaskAndInboxAfterMove(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	root := t.TempDir()
	original, err := bot.Create(root, "before", "duty", nil)
	if err != nil {
		t.Fatal(err)
	}
	manager := mcp.NewManager(ctx, mcp.Options{})
	defer manager.Close()
	resources, err := NewBotResources(ctx, original, manager)
	if err != nil {
		t.Fatal(err)
	}
	defer resources.Close()
	id := "sa_0123456789abcdef"
	if err := os.MkdirAll(original.AgentDir(id), 0700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(exec.Meta{ID: id, Status: "running", Mode: exec.ModeBrief, PID: 99999999})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(original.AgentDir(id), "meta.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	k := &Kernel{Bot: original, Resources: resources, Inbox: resources.box, WorkDir: work.BotDir(root, "before")}
	if err := resources.RenameTo("after"); err != nil {
		t.Fatal(err)
	}
	if err := k.recoverTasks(); err != nil {
		t.Fatal(err)
	}
	current, err := bot.Load(root, "after")
	if err != nil {
		t.Fatal(err)
	}
	meta, err := exec.ReadMeta(current.AgentDir(id))
	if err != nil {
		t.Fatal(err)
	}
	if meta.Status != bot.ForkInterrupted || meta.ID != id {
		t.Fatalf("cold task not recovered at current path: %+v", meta)
	}
	pending := resources.box.Pending()
	if len(pending) != 1 || pending[0].ID != "ev_"+id+"_interrupted" || pending[0].Sender != id {
		t.Fatalf("recovery lost original event identity/current inbox: %+v", pending)
	}
	if err := k.recoverTasks(); err != nil {
		t.Fatal(err)
	}
	if len(resources.box.Pending()) != 1 {
		t.Fatal("recovery duplicated terminal event")
	}
	if _, err := os.Stat(bot.Dir(root, "before")); !os.IsNotExist(err) {
		t.Fatalf("cold recovery revived old directory: %v", err)
	}
}

func TestProfileRestoreRebasesDescriptorBeforeScopeInitialization(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	root := t.TempDir()
	original, err := bot.Create(root, "before", "duty", nil)
	if err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(work.BotDir(root, "before"), "project")
	// TestMain keeps t.TempDir inside this repository. Give the child's
	// project an explicit checkout boundary so resource admission cannot
	// legitimately resolve to the enclosing implementation repository.
	if err := os.MkdirAll(filepath.Join(project, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	manager := mcp.NewManager(ctx, mcp.Options{})
	defer manager.Close()
	resources, err := NewBotResources(ctx, original, manager)
	if err != nil {
		t.Fatal(err)
	}
	defer resources.Close()
	// Message read this descriptor, then rename completed before Restore starts.
	descriptor := exec.Descriptor{Version: 1, ID: "sa_123456789abcdef0", Bot: "before", Type: "dev", Mode: exec.ModeBrief, CWD: project, Provider: "fixture", Model: "original-model", Tools: []string{"sh", "read_file"}, MaxSteps: 31, TimeoutSec: 22}
	if err := resources.RenameTo("after"); err != nil {
		t.Fatal(err)
	}
	current, err := bot.Load(root, "after")
	if err != nil {
		t.Fatal(err)
	}
	k := &Kernel{Bot: current, Resources: resources, Inbox: resources.box, WorkDir: work.BotDir(root, "after")}
	_ = k.readScope("") // Existing cwd state already knows "after"; not the old name.
	req, err := k.restoreSubagent(ctx, descriptor)
	if err != nil {
		t.Fatal(err)
	}
	wantProject := filepath.Join(work.BotDir(root, "after"), "project")
	if got := k.cwdScope(descriptor.ID); got != wantProject {
		t.Fatalf("scope fell back after pre-Restore rename: got %q want %q", got, wantProject)
	}
	if req.Bot != "after" || req.Dir != current.AgentDir(descriptor.ID) || req.Descriptor.Bot != "after" || req.Descriptor.CWD != wantProject {
		t.Fatalf("stale restored identity/path: %+v descriptor=%+v", req, req.Descriptor)
	}
	if req.Descriptor.Provider != descriptor.Provider || req.Descriptor.Model != descriptor.Model || req.ID != descriptor.ID || req.MaxSteps != descriptor.MaxSteps || !reflect.DeepEqual(req.Descriptor.Tools, descriptor.Tools) {
		t.Fatal("path restore changed original child policy/identity")
	}
	if req.Resource != "repo:"+wantProject {
		t.Fatalf("restored admission resource lost project: %q; descriptor cwd=%q, want=%q, root=%q", req.Resource, req.Descriptor.CWD, wantProject, root)
	}
}
