package exec

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/code"
	"xbot/internal/work"
)

func TestMoveBotSymlinkRootKeepsPhysicalNestedPaths(t *testing.T) {
	f := newMoveBotFixture(t)
	physical, err := filepath.EvalSymlinks(f.root)
	if err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "root-alias")
	if err := os.Symlink(physical, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	f.root = alias
	oldNested := filepath.Join(work.BotDir(physical, "before"), "project", "nested")
	oldRepo := filepath.Join(work.BotDir(physical, "before"), "repo")
	for _, path := range []string{oldNested, oldRepo} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	external := t.TempDir()
	local := f.local()
	gate := &moveGate{entered: make(chan struct{}), release: make(chan struct{})}
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(gate.release) }) })
	local.Prov = gate
	req := f.request("sa_livephys1", true)
	req.Descriptor.CWD = oldNested
	req.Workspace = work.BotDir(physical, "before")
	req.Resource = "dir:" + oldNested
	if _, err := local.Start(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	<-gate.entered
	cold := *req.Descriptor
	cold.ID = "sa_coldphys1"
	if err := writeDescriptor(filepath.Join(f.agents(), cold.ID), cold); err != nil {
		t.Fatal(err)
	}
	oldResult := filepath.Join(bot.Dir(physical, "before"), "agents", cold.ID, "results", "7.txt")
	if err := writeMeta(filepath.Join(f.agents(), cold.ID), Meta{ID: cold.ID, Status: statusDone, Continuable: true, TurnSeq: 7, ResultPath: oldResult}); err != nil {
		t.Fatal(err)
	}
	outside := cold
	outside.ID = "sa_external1"
	outside.CWD = external
	if err := writeDescriptor(filepath.Join(f.agents(), outside.ID), outside); err != nil {
		t.Fatal(err)
	}
	records := &code.Record{Dir: filepath.Join(bot.Dir(alias, "before"), "code")}
	coding, err := records.Start(code.Task{WorkDir: oldNested, Repo: oldRepo, Goal: "physical project", MaxSteps: 2, TimeoutSec: 10})
	if err != nil {
		t.Fatal(err)
	}
	relative, err := records.Start(code.Task{WorkDir: external, Repo: "./repo", Goal: "external", MaxSteps: 2, TimeoutSec: 10})
	if err != nil {
		t.Fatal(err)
	}
	if err := records.Move(filepath.Join(bot.Dir(alias, "after"), "code"), func() error { _, err := f.move("after", nil); return err }); err != nil {
		t.Fatal(err)
	}
	expected := filepath.Join(work.BotDir(physical, "after"), "project", "nested")
	local.mu.Lock()
	snapshot := local.jobs[req.ID].requestSnapshot()
	local.mu.Unlock()
	if snapshot.Descriptor.CWD != expected || snapshot.Resource != "dir:"+expected || snapshot.Workspace != work.BotDir(physical, "after") {
		t.Errorf("live paths retained old physical prefix: %+v descriptor=%+v", snapshot, *snapshot.Descriptor)
	}
	stored, err := ReadDescriptor(filepath.Join(f.agents(), cold.ID))
	if err != nil {
		t.Fatal(err)
	}
	if stored.CWD != expected || stored.Bot != "after" {
		t.Errorf("cold descriptor=%+v", stored)
	}
	meta, err := ReadMeta(filepath.Join(f.agents(), cold.ID))
	if err != nil {
		t.Fatal(err)
	}
	if meta.ResultPath != filepath.Join(bot.Dir(physical, "after"), "agents", cold.ID, "results", "7.txt") {
		t.Errorf("cold result path=%s", meta.ResultPath)
	}
	ext, err := ReadDescriptor(filepath.Join(f.agents(), outside.ID))
	if err != nil || ext.CWD != external {
		t.Errorf("external descriptor changed: %+v %v", ext, err)
	}
	task, err := records.Get(coding.ID)
	if err != nil || task.WorkDir != expected || task.Repo != filepath.Join(work.BotDir(physical, "after"), "repo") {
		t.Errorf("code physical paths=%+v %v", task, err)
	}
	unchanged, err := records.Get(relative.ID)
	if err != nil || unchanged.WorkDir != external || unchanged.Repo != "./repo" {
		t.Errorf("external/relative paths changed: %+v %v", unchanged, err)
	}
	release.Do(func() { close(gate.release) })
	moveTerminal(t, local)
	final, err := ReadDescriptor(filepath.Join(f.agents(), req.ID))
	if err != nil || final.CWD != expected {
		t.Errorf("final descriptor=%+v %v", final, err)
	}
	if _, err := os.Stat(work.BotDir(physical, "before")); !os.IsNotExist(err) {
		t.Errorf("old workspace returned: %v", err)
	}
}
