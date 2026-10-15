package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/link"
)

func TestMCPCLIAndMount(t *testing.T) {
	t.Setenv("API_TOKEN", "super-secret-value")
	root := t.TempDir()
	b, err := bot.Create(root, "ada", "persona", []string{link.NameTerminal})
	if err != nil {
		t.Fatal(err)
	}
	if err := cmdLink(root, []string{"install", "ada", "mcp"}); err == nil || !strings.Contains(err.Error(), "not available yet") {
		t.Fatalf("catalog install %v", err)
	}
	dir := t.TempDir()
	sentinel := filepath.Join(dir, "ran")
	script := filepath.Join(dir, "mcp-fake")
	body := "#!/bin/sh\necho ran > " + shellQuote(sentinel) + "\n"
	if err := os.WriteFile(script, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	if err := cmdMCP(root, []string{"add", "ada", "docs", "--command", script, "--arg", "--stdio", "--secret-env", "API_TOKEN"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(b.MCPPath())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "super-secret-value") || !strings.Contains(string(raw), "API_TOKEN") || !strings.Contains(string(raw), script) {
		t.Fatalf("file %s", raw)
	}
	fi, err := os.Stat(b.MCPPath())
	if err != nil || fi.Mode().Perm() != 0600 {
		t.Fatalf("mode %v %v", fi, err)
	}
	if err := cmdMCP(root, []string{"add", "ada", "docs", "--command", script}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("dup %v", err)
	}
	if err := cmdMCP(root, []string{"list", "ada"}); err != nil {
		t.Fatal(err)
	}
	links, err := mount(b, runnerSpec{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, l := range links {
		if l.Name() == "mcp:docs" {
			found = true
		}
	}
	if !found {
		t.Fatal("mcp link was not mounted")
	}
	if _, err := os.Stat(sentinel); err == nil {
		t.Fatal("mount started the server")
	}
	if err := cmdMCP(root, []string{"remove", "ada", "docs"}); err != nil {
		t.Fatal(err)
	}
	links, err = mount(b, runnerSpec{})
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range links {
		if l.Name() == "mcp:docs" {
			t.Fatal("removed server still mounted")
		}
	}
	bad, err := bot.Create(root, "bob", "persona", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bad.MCPPath(), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := mount(bad, runnerSpec{}); err == nil {
		t.Fatal("broken mcp.json mounted")
	}
}

func shellQuote(path string) string {
	return "'" + strings.ReplaceAll(path, "'", `'\''`) + "'"
}
