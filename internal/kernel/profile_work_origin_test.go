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

func TestProfileCachedUseBotKeepsWorkspaceAndScopedProcessDirectory(t *testing.T) {
	k, _, _ := freshBot(t)
	original := k.Bot
	var err error
	k.WorkDir, k.SharedDir, err = work.Open(original.Root, original.Name())
	if err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(k.WorkDir, "project")
	if err := os.Mkdir(project, 0700); err != nil {
		t.Fatal(err)
	}
	k.Links = MountLinks(original, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager := mcp.NewManager(ctx, mcp.Options{})
	defer manager.Close()
	resources, err := NewBotResources(ctx, original, manager)
	if err != nil {
		t.Fatal(err)
	}
	defer resources.Close()
	k.Resources = resources
	if err := k.StartOperations(); err != nil {
		t.Fatal(err)
	}
	defer k.CloseOperations()
	k.attachShell()
	scope := "sa_scope"
	k.noteScope(scope, project)
	var child tools.Tool
	for _, tool := range k.scopeTools(scope) {
		if tool.Name() == "sh" {
			child = tool
			break
		}
	}
	if child == nil {
		t.Fatal("no scoped interpreter")
	}
	next := "origin-moved"
	if err := resources.RenameTo(next); err != nil {
		t.Fatal(err)
	}
	fresh, err := bot.Load(original.Root, next)
	if err != nil {
		t.Fatal(err)
	}
	k.UseBot(fresh) // the actual cached host refresh between web turns
	expected := work.BotDir(original.Root, next)
	if got, notice := k.shellStartScope(""); got != expected || notice != "" {
		t.Fatalf("new-turn cwd=%q notice=%q want=%q", got, notice, expected)
	}
	var main tools.Tool
	for _, tool := range k.baseTools().shells {
		if tool.Name() == "sh" {
			main = tool
			break
		}
	}
	if main == nil {
		t.Fatal("no main interpreter")
	}
	raw, _ := json.Marshal(map[string]string{"description": "Check cached source strict JSON after refreshed binding", "command": `printf '{"same_name":true}\n'`})
	output, err := main.Run(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output, "unavailable") || strings.Contains(output, "outside the workspace") || !strings.HasSuffix(output, `{"same_name":true}`) {
		t.Fatalf("unexpected shell prefix: %s", output)
	}
	raw, _ = json.Marshal(map[string]string{"description": "Keep the original scoped process directory after refresh", "command": `: > after-proof.txt; pwd -P`})
	output, err = child.Run(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output, "error:") || strings.Contains(output, "unavailable") || strings.Contains(output, "outside the workspace") {
		t.Fatal(output)
	}
	if _, err := os.Stat(filepath.Join(expected, "project", "after-proof.txt")); err != nil {
		t.Fatalf("scoped Start lost the original project: %v; %s", err, output)
	}
	if _, err := os.Stat(bot.Dir(original.Root, original.Name())); !os.IsNotExist(err) {
		t.Fatalf("former directory revived: %v", err)
	}
}
