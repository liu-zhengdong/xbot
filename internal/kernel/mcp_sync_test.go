package kernel

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"strings"
	"sync"
	"testing"

	"xbot/internal/link"
)

// fakeMCP serves one tool named tool over a pipe and counts the dials.
func fakeMCP(t *testing.T, tool string) func(context.Context) (io.ReadWriteCloser, error) {
	t.Helper()
	return func(context.Context) (io.ReadWriteCloser, error) {
		client, server := net.Pipe()
		go func() {
			defer server.Close()
			sc := bufio.NewScanner(server)
			for sc.Scan() {
				var msg struct {
					ID     json.RawMessage `json:"id"`
					Method string          `json:"method"`
				}
				if json.Unmarshal(sc.Bytes(), &msg) != nil || len(msg.ID) == 0 {
					continue
				}
				result := `{}`
				switch msg.Method {
				case "initialize":
					result = `{"protocolVersion":"2024-11-05","capabilities":{}}`
				case "tools/list":
					result = `{"tools":[{"name":"` + tool + `","description":"d","inputSchema":{"type":"object"},"annotations":{"readOnlyHint":true}}]}`
				}
				_, _ = server.Write([]byte(`{"jsonrpc":"2.0","id":` + string(msg.ID) + `,"result":` + result + "}\n"))
			}
		}()
		t.Cleanup(func() { _ = client.Close() })
		return client, nil
	}
}

func mcpToolNames(t *testing.T, k *Kernel, id string) string {
	t.Helper()
	out, err := (linksTool{k: k}).load(id)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// A kernel built before the store installs a server must see it on its next
// turn, see it replaced when the definition changes, and lose it on removal.
func TestStoreMCPReachesLiveKernel(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	var mu sync.Mutex
	tool := "first"
	k := &Kernel{Bot: b, Links: []link.Link{link.Terminal{}}}
	k.newMCP = func(s link.MCPServer) link.Link {
		mu.Lock()
		name := tool
		mu.Unlock()
		s.Dial = fakeMCP(t, name)
		return link.NewMCP(s)
	}
	if _, err := k.mountedTools(); err != nil {
		t.Fatal(err)
	}
	if err := link.EnableConnector(b.GlobalMCPPath(), "fetch"); err != nil {
		t.Fatal(err)
	}
	if got := mcpToolNames(t, k, "mcp:fetch"); !strings.Contains(got, "first") {
		t.Fatalf("installed server has no tools on the live kernel: %q", got)
	}
	list, err := k.mountedTools()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tl := range list {
		if strings.Contains(tl.Name(), "first") {
			found = true
		}
	}
	if !found {
		t.Fatal("mounted tools lack the installed server's tool")
	}

	// Replace: the same name with another definition gets a fresh instance.
	mu.Lock()
	tool = "second"
	mu.Unlock()
	if err := link.RemoveMCP(b.GlobalMCPPath(), "fetch"); err != nil {
		t.Fatal(err)
	}
	if err := link.AddMCP(b.GlobalMCPPath(), link.MCPServer{Name: "fetch", URL: "https://example.com/mcp"}); err != nil {
		t.Fatal(err)
	}
	if got := mcpToolNames(t, k, "mcp:fetch"); !strings.Contains(got, "second") || strings.Contains(got, "first") {
		t.Fatalf("replaced server not remounted: %q", got)
	}

	// Remove: the instance leaves the kernel.
	if err := link.RemoveMCP(b.GlobalMCPPath(), "fetch"); err != nil {
		t.Fatal(err)
	}
	for _, l := range k.snapshotLinks() {
		if l.Name() == "mcp:fetch" {
			t.Fatal("removed server is still mounted")
		}
	}
}

func TestMCPSyncKeepsInstanceWhenUnchanged(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	if err := link.EnableConnector(b.GlobalMCPPath(), "fetch"); err != nil {
		t.Fatal(err)
	}
	k := &Kernel{Bot: b, Links: []link.Link{link.Terminal{}}}
	k.newMCP = func(s link.MCPServer) link.Link { s.Dial = fakeMCP(t, "t"); return link.NewMCP(s) }
	first := k.snapshotLinks()
	second := k.snapshotLinks()
	var a, c link.Link
	for _, l := range first {
		if l.Name() == "mcp:fetch" {
			a = l
		}
	}
	for _, l := range second {
		if l.Name() == "mcp:fetch" {
			c = l
		}
	}
	if a == nil || a != c {
		t.Fatal("an unchanged server must keep its running instance")
	}
}
