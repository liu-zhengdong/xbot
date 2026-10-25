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
	if err := link.EnableConnector(b.GlobalMCPPath(), "fetch"); err != nil {
		t.Fatal(err)
	}
	if err := link.AddMCP(b.GlobalMCPPath(), link.MCPServer{Name: "mine", Command: "/bin/echo"}); err != nil {
		t.Fatal(err)
	}
	text := systemOf(t, k)
	lines := installedBody(text)
	if len(lines) != 3 {
		t.Fatalf("%q", lines)
	}
	want := []string{"电脑操作:", "网页抓取:", "mcp:mine:"}
	for i, prefix := range want {
		if !strings.HasPrefix(lines[i], prefix) {
			t.Fatalf("line %d %q, want prefix %q", i, lines[i], prefix)
		}
	}
	if !strings.HasPrefix(text[strings.Index(text, "## 连接器"):], "## 连接器\n电脑操作:") {
		t.Fatal("heading must be followed directly by name:description lines")
	}
	for _, bad := range []string{"inputSchema", `"type": "object"`, "- 电脑操作"} {
		if strings.Contains(text, bad) {
			t.Fatalf("%q leaked into the prompt", bad)
		}
	}
}

func TestConnectorsLoadAcceptsShownName(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	if err := link.EnableConnector(b.GlobalMCPPath(), "fetch"); err != nil {
		t.Fatal(err)
	}
	k := &Kernel{Bot: b, Links: []link.Link{link.Terminal{}}}
	if _, err := (linksTool{k: k}).load("网页抓取"); err != nil {
		t.Fatalf("load by shown name: %v", err)
	}
	if _, err := (linksTool{k: k}).load("Gmail"); err == nil {
		t.Fatal("a coming-soon app must not load")
	}
}
