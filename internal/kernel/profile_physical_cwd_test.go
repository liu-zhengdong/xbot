package kernel

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/mcp"
	"xbot/internal/tools"
	"xbot/internal/work"
)

func TestProfileAliasedRootKeepsPhysicalLiveProject(t *testing.T) {
	k, _, _ := freshBot(t)
	original := k.Bot
	alias := filepath.Join(t.TempDir(), "root-alias")
	if err := os.Symlink(original.Root, alias); err != nil {
		t.Fatal(err)
	}
	aliased, err := bot.Load(alias, original.Name())
	if err != nil {
		t.Fatal(err)
	}
	k.Bot = aliased
	k.WorkDir, k.SharedDir, err = work.Open(alias, original.Name())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager := mcp.NewManager(ctx, mcp.Options{})
	defer manager.Close()
	resources, err := NewBotResources(ctx, aliased, manager)
	if err != nil {
		t.Fatal(err)
	}
	defer resources.Close()
	k.Resources = resources
	if err := k.StartOperations(); err != nil {
		t.Fatal(err)
	}
	defer k.CloseOperations()
	scope := "sa_physical"
	k.noteScope(scope, k.WorkDir)
	var shell tools.Tool
	for _, tool := range k.scopeTools(scope) {
		if tool.Name() == "sh" {
			shell = tool
			break
		}
	}
	if shell == nil {
		t.Fatal("no scoped interpreter")
	}
	raw, _ := json.Marshal(map[string]string{"description": "Keep a real project before rename", "command": "mkdir full-project; cd full-project; printf before > before.txt; pwd -P"})
	before, err := shell.Run(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	physicalRoot, err := filepath.EvalSymlinks(original.Root)
	if err != nil {
		t.Fatal(err)
	}
	oldProject := filepath.Join(work.BotDir(physicalRoot, original.Name()), "full-project")
	if !strings.Contains(before, oldProject) {
		t.Fatalf("fixture did not store physical cwd: %s", before)
	}
	next := "physical-moved"
	if err := resources.RenameTo(next); err != nil {
		t.Fatal(err)
	}
	fresh, err := bot.Load(alias, next)
	if err != nil {
		t.Fatal(err)
	}
	k.UseBot(fresh)
	raw, _ = json.Marshal(map[string]string{"description": "Continue in the same project after rename", "command": "cat before.txt; printf after > after.txt; pwd -P"})
	after, err := shell.Run(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	newProject := filepath.Join(work.BotDir(physicalRoot, next), "full-project")
	if strings.Contains(after, "unavailable") || strings.Contains(after, "outside the workspace") || !strings.Contains(after, newProject) {
		t.Fatalf("physical project lost: %s", after)
	}
	data, err := os.ReadFile(filepath.Join(newProject, "after.txt"))
	if err != nil || string(data) != "after" {
		t.Fatalf("project proof %q %v; %s", data, err, after)
	}
	if _, err := os.Stat(work.BotDir(alias, original.Name())); !os.IsNotExist(err) {
		t.Fatalf("old workspace revived: %v", err)
	}
}
