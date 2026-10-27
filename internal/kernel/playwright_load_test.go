package kernel

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"

	"xbot/internal/link"
)

func TestPlaywrightLoadToolsAndFailure(t *testing.T) {
	for _, id := range []string{"playwright", "mcp:playwright", "Playwright"} {
		t.Run(id, func(t *testing.T) {
			b := testBot(t, link.NameTerminal)
			if err := link.EnableConnector(b.GlobalMCPPath(), "playwright"); err != nil {
				t.Fatal(err)
			}
			k := &Kernel{Bot: b}
			k.newMCP = func(s link.MCPServer) link.Link {
				s.Dial = fakeMCP(t, "browser_snapshot")
				m := link.NewMCP(s)
				t.Cleanup(func() { _ = m.Close() })
				return m
			}
			before, err := k.mountedTools()
			if err != nil {
				t.Fatal(err)
			}
			for _, tool := range before {
				if strings.HasPrefix(tool.Name(), "mcp_playwright_") {
					t.Fatal("schema disclosed before load")
				}
			}
			out, err := (linksTool{k}).load(id)
			if err != nil || !strings.Contains(out, "mcp_playwright_browser_snapshot") {
				t.Fatalf("%q %v", out, err)
			}
			after, err := k.mountedTools()
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, tool := range after {
				if tool.Name() == "mcp_playwright_browser_snapshot" {
					found = true
					if _, err := tool.Run(context.Background(), []byte(`{}`)); err != nil {
						t.Fatal(err)
					}
				}
			}
			if !found {
				t.Fatal("loaded tool absent from next model call")
			}
			k.clearLoaded()
			after, _ = k.mountedTools()
			for _, tool := range after {
				if strings.HasPrefix(tool.Name(), "mcp_playwright_") {
					t.Fatal("schema persisted into next turn")
				}
			}
		})
	}
	t.Run("startup failure", func(t *testing.T) {
		b := testBot(t, link.NameTerminal)
		if err := link.AddMCP(b.GlobalMCPPath(), link.MCPServer{Name: "playwright", Command: "/does-not-exist/playwright-test"}); err != nil {
			t.Fatal(err)
		}
		k := &Kernel{Bot: b}
		out, err := (linksTool{k}).load("playwright")
		if err == nil || !strings.Contains(err.Error(), "start") || !strings.Contains(err.Error(), "playwright-test") {
			t.Fatalf("failure lost: %q %v", out, err)
		}
		if k.isLoaded("mcp:playwright") {
			t.Fatal("failed load marked as loaded")
		}
	})
}

func TestCuratedMCPConnectorsLoad(t *testing.T) {
	apps, err := link.StoreApps()
	if err != nil {
		t.Fatal(err)
	}
	for _, app := range apps {
		if app.Kind != link.AppServer {
			continue
		}
		t.Run(app.ID, func(t *testing.T) {
			b := testBot(t, link.NameTerminal)
			if err := link.EnableConnector(b.GlobalMCPPath(), app.ID); err != nil {
				t.Fatal(err)
			}
			k := &Kernel{Bot: b}
			k.newMCP = func(s link.MCPServer) link.Link {
				s.Dial = fakeMCP(t, "fixture_action")
				m := link.NewMCP(s)
				t.Cleanup(func() { _ = m.Close() })
				return m
			}
			for _, alias := range []string{app.Title, app.ID, "mcp:" + app.ID} {
				out, err := (linksTool{k}).load(alias)
				if err != nil || !strings.Contains(out, "mcp_"+app.ID+"_fixture_action") {
					t.Fatalf("%s: %q %v", alias, out, err)
				}
			}
		})
	}
}

func TestConnectorLoadLatency(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	if err := link.EnableConnector(b.GlobalMCPPath(), "playwright"); err != nil {
		t.Fatal(err)
	}
	k := &Kernel{Bot: b}
	k.newMCP = func(s link.MCPServer) link.Link {
		s.Dial = fakeMCP(t, "fixture_action")
		m := link.NewMCP(s)
		t.Cleanup(func() { _ = m.Close() })
		return m
	}
	start := time.Now()
	if _, err := (linksTool{k}).load("Playwright"); err != nil {
		t.Fatal(err)
	}
	cold := time.Since(start)
	var samples []time.Duration
	for range 100 {
		start = time.Now()
		if _, err := (linksTool{k}).load("playwright"); err != nil {
			t.Fatal(err)
		}
		samples = append(samples, time.Since(start))
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	t.Logf("scripted cold load=%s; cached links load p95=%s (100 samples)", cold, samples[94])
}
