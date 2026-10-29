package kernel

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

func TestLinksConfirmInstallAndRemove(t *testing.T) {
	// The tool only builds the card summary; the kernel review path decides.
	var tool linksTool
	need, summary, err := tool.Confirm([]byte(`{"action":"install","id":"webhook"}`))
	if err != nil || need || !strings.HasPrefix(summary, "install webhook") {
		t.Fatalf("need %v summary %q err %v", need, summary, err)
	}
	need, summary, err = tool.Confirm([]byte(`{"action":"remove","id":"webhook"}`))
	if err != nil || need || !strings.HasPrefix(summary, "remove webhook") {
		t.Fatalf("remove need %v summary %q err %v", need, summary, err)
	}
	for _, action := range []string{"search", "load"} {
		need, summary, err = tool.Confirm([]byte(`{"action":"` + action + `","id":"webhook"}`))
		if err != nil || need {
			t.Fatalf("%s need %v summary %q err %v", action, need, summary, err)
		}
	}
}

func TestLinksInstallRequiresApproval(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	p := &scripted{resps: []provider.Message{
		linksCall("install", "browser"),
		{Role: provider.RoleAssistant, Content: provider.TextContent("stopped")},
	}}
	var gotTool, gotSummary string
	k := &Kernel{Bot: b, Provider: p, MaxSteps: 4, Approve: func(_ context.Context, tool, summary string) (bool, error) {
		gotTool, gotSummary = tool, summary
		return false, nil
	}}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "install browser", time.Unix(1, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	if gotTool != "links" || !strings.Contains(gotSummary, "install browser") {
		t.Fatalf("card %s %s", gotTool, gotSummary)
	}
	if b.Grants(link.NameBrowser) {
		t.Fatal("granted without approval")
	}
	text := transcriptText(t, b)
	if !strings.Contains(text, "denied by user") {
		t.Fatal(text)
	}
}

func TestLinksInstallAllowGrants(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	p := &scripted{resps: []provider.Message{
		linksCall("install", "browser"),
		{Role: provider.RoleAssistant, Content: provider.TextContent("done")},
	}}
	k := &Kernel{Bot: b, Provider: p, MaxSteps: 4, Approve: func(context.Context, string, string) (bool, error) {
		return true, nil
	}}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "install browser", time.Unix(1, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	if !b.Grants(link.NameBrowser) {
		t.Fatal("not granted")
	}
	if !strings.Contains(transcriptText(t, b), "installed browser") {
		t.Fatal(transcriptText(t, b))
	}
}

func TestLinksInstallHidesSecrets(t *testing.T) {
	const secret = "super-secret-value"
	b := testBot(t, link.NameTerminal)
	p := &scripted{resps: []provider.Message{
		linksCall("install", "telegram"),
		{Role: provider.RoleAssistant, Content: provider.TextContent("nope")},
	}}
	k := &Kernel{Bot: b, Provider: p, MaxSteps: 4}
	k.Approve = func(context.Context, string, string) (bool, error) {
		k.StageSecrets("telegram", map[string]string{"TELEGRAM_BOT_TOKEN": secret})
		return true, nil
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "install telegram", time.Unix(1, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	if !b.Grants(link.NameTelegram) {
		t.Fatal("telegram was not granted")
	}
	text := transcriptText(t, b)
	if strings.Contains(text, secret) {
		t.Fatal("secret reached the transcript")
	}
	if !strings.Contains(text, "installed telegram") {
		t.Fatal(text)
	}
	raw, err := os.ReadFile(b.SecretPath())
	if err != nil || !strings.Contains(string(raw), secret) {
		t.Fatalf("secret file: %v %s", err, raw)
	}
	info, err := os.Stat(b.SecretPath())
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("mode %v %v", info, err)
	}
}

func TestLinksInstallWaitsForSecretFill(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	p := &scripted{resps: []provider.Message{
		linksCall("install", "telegram"),
		{Role: provider.RoleAssistant, Content: provider.TextContent("nope")},
	}}
	k := &Kernel{Bot: b, Provider: p, MaxSteps: 4, Approve: func(context.Context, string, string) (bool, error) {
		return true, nil
	}}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "install telegram", time.Unix(1, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	if b.Grants("telegram") {
		t.Fatal("granted")
	}
	text := transcriptText(t, b)
	if !strings.Contains(text, "owner did not fill") || !strings.Contains(text, "TELEGRAM_BOT_TOKEN") {
		t.Fatal(text)
	}
	if _, err := os.Stat(b.SecretPath()); err == nil {
		t.Fatal("secrets file created")
	}
}

func TestLinksRemoveRequiresApproval(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	if err := b.GrantLink(link.NameBrowser); err != nil {
		t.Fatal(err)
	}
	p := &scripted{resps: []provider.Message{
		linksCall("remove", "browser"),
		{Role: provider.RoleAssistant, Content: provider.TextContent("gone")},
	}}
	asked := false
	k := &Kernel{Bot: b, Provider: p, MaxSteps: 4, Approve: func(context.Context, string, string) (bool, error) {
		asked = true
		return true, nil
	}}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "remove browser", time.Unix(1, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	if !asked {
		t.Fatal("remove did not ask for approval")
	}
	if b.Grants(link.NameBrowser) {
		t.Fatal("still granted")
	}
	if !strings.Contains(transcriptText(t, b), "removed browser") {
		t.Fatal(transcriptText(t, b))
	}
}

func TestInstalledPromptGrowsOneLine(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	k := &Kernel{Bot: b, Links: []link.Link{link.Terminal{}, link.Skills{}}}
	base := systemOf(t, k)
	if strings.Contains(base, "## 连接器") {
		t.Fatal(base)
	}
	if err := b.GrantLink(link.NameBrowser); err != nil {
		t.Fatal(err)
	}
	one := systemOf(t, k)
	lines := installedBody(one)
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "浏览器:") {
		t.Fatalf("%q", lines)
	}
	if err := b.GrantLink(link.NameTelegram); err != nil {
		t.Fatal(err)
	}
	two := systemOf(t, k)
	lines = installedBody(two)
	if len(lines) != 2 || !strings.Contains(strings.Join(lines, "\n"), "Telegram:") || !strings.Contains(strings.Join(lines, "\n"), "浏览器:") {
		t.Fatalf("%q", lines)
	}
	if strings.Count(two, "\n") != strings.Count(one, "\n")+1 {
		t.Fatalf("newline growth one %d two %d", strings.Count(one, "\n"), strings.Count(two, "\n"))
	}
	if err := b.GrantLink(link.NameSkills); err != nil {
		t.Fatal(err)
	}
	three := systemOf(t, k)
	if strings.Count(three, "\n") != strings.Count(two, "\n") {
		t.Fatalf("newline growth two %d three %d", strings.Count(two, "\n"), strings.Count(three, "\n"))
	}
	if strings.Contains(three, "Read the body of a shared skill") || strings.Contains(three, "Skill slug") || strings.Contains(three, `"type": "object"`) || strings.Contains(three, "inputSchema") {
		t.Fatal(three)
	}
}

func TestLinksLoadDoesNotPersist(t *testing.T) {
	b := testBot(t, link.NameTerminal, link.NameSkills)
	p := &scripted{resps: []provider.Message{
		linksCall("load", "skills"),
		{Role: provider.RoleAssistant, Content: provider.TextContent("loaded")},
	}}
	k := &Kernel{
		Bot: b, Provider: p, MaxSteps: 4,
		Links: []link.Link{link.Terminal{}, link.Skills{}},
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "load skills", time.Unix(1, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	if len(p.reqs) < 2 {
		t.Fatalf("calls %d", len(p.reqs))
	}
	// Loading never declares a schema; the tools stay behind the door.
	for _, req := range p.reqs {
		if hasDef(req.Tools, "read_skill") || hasDef(req.Tools, "write_skill") {
			t.Fatal("skill schema declared in tools[]")
		}
	}
	if !k.isLoaded(link.NameSkills) || !hasToolName(mustMounted(t, k), "write_skill") {
		t.Fatal("write_skill not loaded for this turn")
	}
	if e, ok := k.toolRegistry().find("read_skill"); !ok || !strings.Contains(e.tool.Description(), "Pass id, the skill id") {
		t.Fatal("read_skill not registered")
	}
	if strings.Contains(p.reqs[0].Messages[0].Text(), "Read the body of a shared skill") || strings.Contains(p.reqs[1].Messages[0].Text(), "Skill slug") {
		t.Fatal("schema leaked into the system prompt")
	}
	p2 := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: provider.TextContent("next")}}}
	k.Provider = p2
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "again", time.Unix(2, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	if len(p2.reqs) == 0 || k.isLoaded(link.NameSkills) || !hasToolName(mustMounted(t, k), "write_skill") {
		t.Fatal("native write_skill missing or link load persisted")
	}
}

func TestLinksSearchMatchesEnglish(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	k := &Kernel{Bot: b}
	tool := linksTool{k}
	for _, tc := range []struct{ query, id string }{
		{"feishu", "lark"},
	} {
		got := tool.search(tc.query)
		if !strings.Contains(got, tc.id) {
			t.Fatalf("%s -> %q", tc.query, got)
		}
	}
}

func TestBrowserLoadListsTools(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	k := &Kernel{Bot: b, Links: []link.Link{link.Terminal{}}}
	out, err := (linksTool{k}).install("browser")
	if err != nil || out != "installed browser" {
		t.Fatalf("install %q %v", out, err)
	}
	list, err := k.modelTools()
	if err != nil {
		t.Fatal(err)
	}
	if !hasToolName(list, "browser_open") || hasToolName(declared(list), "browser_open") {
		t.Fatal("browser tools must be registered behind the door, never declared")
	}
	out, err = (linksTool{k}).load("browser")
	if err != nil || !strings.Contains(out, "browser_open") || !strings.Contains(out, "browser_snapshot") || !strings.Contains(out, "browser_stop") {
		t.Fatalf("load %q %v", out, err)
	}
}

// The desktop is an organ: on from boot, not installable, and load only lists it.
func TestDesktopLoadListsTools(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	k := &Kernel{Bot: b, Links: []link.Link{link.Terminal{}}}
	if _, err := (linksTool{k}).install("desktop"); err == nil {
		t.Fatal("an organ installed like a Link")
	}
	list, err := k.modelTools()
	if err != nil {
		t.Fatal(err)
	}
	if !hasToolName(list, "desktop_click") || hasToolName(declared(list), "desktop_click") {
		t.Fatal("desktop tools must be registered from boot, never declared")
	}
	out, err := (linksTool{k}).load("desktop")
	if err != nil || !strings.Contains(out, "desktop_screenshot") || !strings.Contains(out, "desktop_click") {
		t.Fatalf("load %q %v", out, err)
	}
}

func mustMounted(t *testing.T, k *Kernel) []tools.Tool {
	t.Helper()
	list, err := k.mountedTools()
	if err != nil {
		t.Fatal(err)
	}
	return list
}

// TestOldNodeGrantIsIgnored pins the retired node link out of wiring: a bot
// config that still grants "node" must get no node tools and no error, and
// loading it must fail rather than mount anything.
func TestOldNodeGrantIsIgnored(t *testing.T) {
	b := testBot(t, link.NameTerminal, "node")
	k := &Kernel{Bot: b, Links: []link.Link{link.Terminal{}}}
	k.snapshotLinks()
	for _, l := range k.Links {
		if l.Name() == "node" {
			t.Fatal("old node grant still mounted")
		}
	}
	if _, err := (linksTool{k}).load("node"); err == nil {
		t.Fatal("load node succeeded")
	}
	list, err := k.modelTools()
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range list {
		if tool.Name() == "node_list" || tool.Name() == "node_call" {
			t.Fatalf("%s visible for an old node grant", tool.Name())
		}
	}
}

func TestLinksInstallMountsToolsImmediately(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	k := &Kernel{Bot: b, Links: []link.Link{link.Terminal{}}}
	k.StageSecrets("telegram", map[string]string{"TELEGRAM_BOT_TOKEN": "tok"})
	out, err := (linksTool{k}).install("telegram")
	if err != nil || out != "installed telegram" {
		t.Fatalf("install %q %v", out, err)
	}
	out, err = (linksTool{k}).load("telegram")
	if err != nil || !strings.Contains(out, "telegram_send") || strings.Contains(out, "no tools") {
		t.Fatalf("load %q %v", out, err)
	}
	for _, name := range []string{"read_skill", "write_skill", "code_start"} {
		if !registered(k, name) {
			t.Fatalf("native tool %s missing", name)
		}
	}
	if _, err := (linksTool{k}).remove("telegram"); err != nil {
		t.Fatal(err)
	}
	if _, err := (linksTool{k}).load("telegram"); err == nil {
		t.Fatal("removed telegram still loaded")
	}
}

func linksCall(action, id string) provider.Message {
	args := `{"action":"` + action + `","id":"` + id + `"}`
	return provider.Message{Role: provider.RoleAssistant, Content: provider.ToolContent([]provider.ToolCall{{
		ID: "c1", Type: "function", Function: provider.FunctionCall{Name: "links", Arguments: args},
	}})}
}

func systemOf(t *testing.T, k *Kernel) string {
	t.Helper()
	msgs, _, err := k.assemble(bot.State{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) == 0 || msgs[0].Role != provider.RoleSystem {
		t.Fatalf("system %+v", msgs)
	}
	var parts []string
	for _, msg := range msgs {
		if msg.Role != provider.RoleSystem {
			break
		}
		parts = append(parts, msg.Text())
	}
	return strings.Join(parts, "\n\n")
}

func installedBody(text string) []string {
	const head = "## 连接器\n"
	i := strings.Index(text, head)
	if i < 0 {
		return nil
	}
	rest := text[i+len(head):]
	if j := strings.Index(rest, "\n## "); j >= 0 {
		rest = rest[:j]
	}
	rest = strings.TrimRight(rest, "\n")
	if rest == "" {
		return nil
	}
	return strings.Split(rest, "\n")
}

func transcriptText(t *testing.T, b *bot.Bot) string {
	t.Helper()
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	var bld strings.Builder
	for _, e := range entries {
		bld.WriteString(e.Message.Text())
		bld.WriteByte('\n')
	}
	return bld.String()
}

func hasDef(defs []provider.ToolDef, name string) bool {
	for _, d := range defs {
		if d.Name == name {
			return true
		}
	}
	return false
}
