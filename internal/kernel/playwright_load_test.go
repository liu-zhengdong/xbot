package kernel

import (
	"strings"
	"testing"

	"xbot/internal/inbox"
	"xbot/internal/link"
)

// Curated aliases remain useful for configuration, but never mount model tools.
func TestCuratedMCPConnectorsUseCLI(t *testing.T) {
	apps, err := link.StoreApps()
	if err != nil {
		t.Fatal(err)
	}
	for _, app := range apps {
		if app.Kind != link.AppServer {
			continue
		}
		t.Run(app.ID, func(t *testing.T) {
			b := testBot(t, inbox.NameTerminal)
			if err := link.EnableConnector(b.GlobalMCPPath(), app.ID); err != nil {
				t.Fatal(err)
			}
			k := &Kernel{Bot: b}
			for _, alias := range []string{app.Title, app.ID, "mcp:" + app.ID} {
				out, err := (linksTool{k}).load(alias)
				if err == nil || !strings.Contains(err.Error(), "mcp tools") || !strings.Contains(err.Error(), app.ID) {
					t.Fatalf("%s: %q %v", alias, out, err)
				}
			}
			for _, l := range k.snapshotLinks() {
				if strings.HasPrefix(l.Name(), "mcp:") {
					t.Fatalf("hidden MCP adapter mounted: %s", l.Name())
				}
			}
			if k.isLoaded("mcp:" + app.ID) {
				t.Fatal("retired load marked the MCP group loaded")
			}
		})
	}
}
