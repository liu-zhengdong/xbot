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
	for _, id := range []string{"filesystem", "fetch", "localhttp"} {
		if !seen[id] {
			t.Fatalf("missing %s in %v", id, seen)
		}
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
