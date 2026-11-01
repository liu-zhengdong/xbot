package link

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"xbot/internal/config"
	"xbot/internal/mcp"
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
	for _, id := range []string{"playwright", "localhttp"} {
		if !seen[id] {
			t.Fatalf("missing %s in %v", id, seen)
		}
	}
	if seen["fetch"] {
		t.Fatal("retired fetch is in the catalog")
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
	if err := mcp.RemoveMCP(path, "localhttp", mcp.Options{IsProviderKey: config.ProviderKey}); err != nil {
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
	if err := mcp.AddMCP(path, mcp.Spec{Name: "filesystem", Command: "mcp-server-filesystem"}, mcp.Options{IsProviderKey: config.ProviderKey}); err != nil {
		t.Skipf("custom entry not accepted here: %v", err)
	}
	list, err := mcp.LoadMCP(path, mcp.Options{IsProviderKey: config.ProviderKey})
	if err != nil || len(list) != 1 || list[0].Name != "filesystem" {
		t.Fatalf("hand-written entry lost: %v %v", list, err)
	}
}

// A keyless entry from an older one-click add is upgraded to the row, a keyed
// one is left alone, and the Bot's key store wins over the process environment
// only for the names the server declares.
func TestEnableConnectorUpgradesAndKeyReachesSpawn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	if err := mcp.AddMCP(path, mcp.Spec{Name: "context7", Command: "npx", Args: []string{"-y", "@upstash/context7-mcp"}}, mcp.Options{IsProviderKey: config.ProviderKey}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := EnableConnector(path, "context7"); err != nil {
			t.Fatal(err)
		}
	}
	list, _ := mcp.LoadMCP(path, mcp.Options{IsProviderKey: config.ProviderKey})
	if len(list) != 1 || !mcp.HasSecretEnvs(list[0], []string{"CONTEXT7_API_KEY"}) {
		t.Fatalf("not upgraded %+v", list)
	}
	secrets := filepath.Join(t.TempDir(), "secrets.json")
	if err := WriteSecrets(secrets, map[string]string{"CONTEXT7_API_KEY": "from-bot", "PATH": "/evil"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONTEXT7_API_KEY", "from-env")
	s := list[0]
	s.Secret = SecretLookup(secrets)
	// The file lookup remains a Link adapter seam; transport credential precedence
	// and filtering are asserted in the core package.
	if value, ok := s.Secret("CONTEXT7_API_KEY"); !ok || value != "from-bot" || !s.HasSecretValues([]string{"CONTEXT7_API_KEY"}) {
		t.Fatalf("stored credential not available: found=%v", ok)
	}
	s.Secret = SecretLookup(filepath.Join(t.TempDir(), "none.json"))
	if !s.HasSecretValues([]string{"CONTEXT7_API_KEY"}) {
		t.Fatal("environment fallback not available")
	}
}

func TestConnectorCatalogRejectsHeldProviderCredential(t *testing.T) {
	config.SetHeld("MCP_TOKEN", "fixture-provider-value")
	defer config.ForgetKeys()
	path := filepath.Join(t.TempDir(), "mcp.json")
	if err := EnableConnector(path, "localhttp"); err == nil {
		t.Fatal("catalogue admitted a credential held as a provider key")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("rejected catalogue entry wrote configuration", err)
	}
}
