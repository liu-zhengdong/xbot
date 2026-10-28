package kernel

import (
	"strings"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/link"
)

// The system prompt lists installed apps under one 连接器 heading, one 名称:描述 line each.
// Apps installed for the owner appear for every bot; no tool schema comes along.
func TestConnectorsBlockListsGlobalInstalls(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	k := &Kernel{Bot: b, Links: []link.Link{link.Terminal{}}}
	if strings.Contains(systemOf(t, k), "## 连接器") {
		t.Fatal("empty store must add no block")
	}
	if err := bot.GrantGlobal(b.Root, link.NameDesktop); err != nil {
		t.Fatal(err)
	}
	if err := link.EnableConnector(b.GlobalMCPPath(), "playwright"); err != nil {
		t.Fatal(err)
	}
	if err := link.AddMCP(b.GlobalMCPPath(), link.MCPServer{Name: "mine", Command: "/bin/echo"}); err != nil {
		t.Fatal(err)
	}
	text := systemOf(t, k)
	lines := installedBody(text)
	// An organ is native, not a connector, even with an old grant on file.
	if len(lines) != 2 {
		t.Fatalf("%q", lines)
	}
	want := []string{"mcp:mine:", "Playwright:"}
	for i, prefix := range want {
		if !strings.HasPrefix(lines[i], prefix) {
			t.Fatalf("line %d %q, want prefix %q", i, lines[i], prefix)
		}
	}
	if !strings.HasPrefix(text[strings.Index(text, "## 连接器"):], "## 连接器\nmcp:mine:") {
		t.Fatal("heading must be followed directly by name:description lines")
	}
	for _, bad := range []string{"inputSchema", `"type": "object"`, "电脑操作"} {
		if strings.Contains(text, bad) {
			t.Fatalf("%q leaked into the prompt", bad)
		}
	}
}

func TestConnectorsLoadAcceptsShownName(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	if err := link.EnableConnector(b.GlobalMCPPath(), "playwright"); err != nil {
		t.Fatal(err)
	}
	k := &Kernel{Bot: b, Links: []link.Link{link.Terminal{}}}
	k.newMCP = func(s link.MCPServer) link.Link {
		s.Dial = fakeMCP(t, "playwright")
		m := link.NewMCP(s)
		t.Cleanup(func() { _ = m.Close() })
		return m
	}
	if _, err := (linksTool{k: k}).load("Playwright"); err != nil {
		t.Fatalf("load by shown name: %v", err)
	}
	if _, err := (linksTool{k: k}).load("Gmail"); err == nil {
		t.Fatal("a coming-soon app must not load")
	}
}

func TestConnectorsBlockExcludesNonStoreLinks(t *testing.T) {
	b := testBot(t, link.NameTerminal, link.NameWebhook, link.NameWatch, link.NameSkills, link.NameCoding)
	k := &Kernel{Bot: b}
	text, err := k.installedSection()
	if err != nil {
		t.Fatal(err)
	}
	if text != "" {
		t.Fatalf("non-store links in connectors: %q", text)
	}
	if err := bot.GrantGlobal(b.Root, link.NameBrowser); err != nil {
		t.Fatal(err)
	}
	text, err = k.installedSection()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "浏览器:") {
		t.Fatal(text)
	}
	for _, bad := range []string{"webhook", "watch", "skills", "coding-agent"} {
		if strings.Contains(text, bad) {
			t.Fatal(text)
		}
	}
}

func TestNativeConnectorsLoadByNameAndID(t *testing.T) {
	for _, tc := range []struct{ id, grant, tool string }{
		{"电脑操作", link.NameDesktop, "desktop_screenshot"}, {"desktop", link.NameDesktop, "desktop_screenshot"},
		{"浏览器", link.NameBrowser, "browser_open"}, {"browser", link.NameBrowser, "browser_open"},
	} {
		t.Run(tc.id, func(t *testing.T) {
			b := testBot(t, link.NameTerminal)
			if err := bot.GrantGlobal(b.Root, tc.grant); err != nil {
				t.Fatal(err)
			}
			k := &Kernel{Bot: b}
			out, err := (linksTool{k}).load(tc.id)
			if err != nil || !strings.Contains(out, tc.tool) {
				t.Fatalf("%q %v", out, err)
			}
			mounted, err := k.mountedTools()
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, tool := range mounted {
				if tool.Name() == tc.tool {
					found = true
				}
			}
			if !found {
				t.Fatal("native tool absent after load")
			}
		})
	}
}
