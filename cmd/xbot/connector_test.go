package main

import (
	"os"
	"strings"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/link"
)

func TestConnectorCLI(t *testing.T) {
	root := t.TempDir()
	b, err := bot.Create(root, "ada", "persona", []string{link.NameTerminal})
	if err != nil {
		t.Fatal(err)
	}
	if err := cmdConnector(root, []string{"list"}); err != nil {
		t.Fatal(err)
	}
	if err := cmdConnector(root, []string{"add", "ada", "localhttp"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(b.MCPPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "localhttp") || !strings.Contains(string(raw), "MCP_TOKEN") || strings.Contains(string(raw), "sk-") {
		t.Fatalf("file %s", raw)
	}
	if err := cmdConnector(root, []string{"remove", "ada", "localhttp"}); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(b.MCPPath())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "localhttp") {
		t.Fatalf("still there %s", raw)
	}
	if err := cmdMCP(root, []string{"add", "ada", "remote", "--url", "http://127.0.0.1:9/mcp", "--secret-env", "MCP_TOKEN"}); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(b.MCPPath())
	if err != nil || !strings.Contains(string(raw), "http://127.0.0.1:9/mcp") || strings.Contains(string(raw), "\"command\"") {
		t.Fatalf("url add %s %v", raw, err)
	}
}
