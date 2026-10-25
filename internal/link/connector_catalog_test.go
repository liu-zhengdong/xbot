package link

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConnectorCatalogEnable(t *testing.T) {
	list, err := Connectors()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, c := range list {
		seen[c.ID] = true
		if strings.Contains(c.Summary, "sk-") {
			t.Fatalf("secret value in catalog: %+v", c)
		}
	}
	for _, id := range []string{"fetch", "localhttp"} {
		if !seen[id] {
			t.Fatalf("missing %s in %v", id, seen)
		}
	}
	if seen["filesystem"] {
		t.Fatal("filesystem duplicates the built-in file tools and has no catalog entry")
	}
	path := filepath.Join(t.TempDir(), "mcp.json")
	if err := EnableConnector(path, "localhttp"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, "http://127.0.0.1:18779/mcp") || !strings.Contains(text, "MCP_TOKEN") {
		t.Fatal(text)
	}
	if strings.Contains(text, "gh-secret") || strings.Contains(text, "sk-") {
		t.Fatal(text)
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0600 {
		t.Fatalf("mode %v %v", fi, err)
	}
	if err := EnableConnector(path, "nope"); err == nil {
		t.Fatal("unknown connector installed")
	}
	if err := RemoveMCP(path, "localhttp"); err != nil {
		t.Fatal(err)
	}
}

// filesystem is gone from the catalog: no one-step install, and a hand-written entry in mcp.json still loads.
func TestFilesystemConnectorIsGone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	if err := EnableConnector(path, "filesystem"); err == nil {
		t.Fatal("EnableConnector must reject filesystem")
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("a rejected connector wrote mcp.json")
	}
	if err := AddMCP(path, MCPServer{Name: "filesystem", Command: "mcp-server-filesystem"}); err != nil {
		t.Skipf("custom entry not accepted here: %v", err)
	}
	list, err := LoadMCP(path)
	if err != nil || len(list) != 1 || list[0].Name != "filesystem" {
		t.Fatalf("hand-written entry lost: %v %v", list, err)
	}
}
