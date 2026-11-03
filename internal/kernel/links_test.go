package kernel

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"xbot/internal/agent"
	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/inbox"
	"xbot/internal/link"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

func TestLinksCardSummaryInstallAndRemove(t *testing.T) {
	// cardSummary keeps the retired Confirm's card text byte for byte; the
	// runtime handler decides that install and remove need the owner.
	if summary := cardSummary("install", "webhook"); !strings.HasPrefix(summary, "install webhook") {
		t.Fatalf("install summary %q", summary)
	}
	if summary := cardSummary("remove", "webhook"); !strings.HasPrefix(summary, "remove webhook") {
		t.Fatalf("remove summary %q", summary)
	}
	for _, action := range []string{"search", "load"} {
		if summary := cardSummary(action, "webhook"); summary != "" {
			t.Fatalf("%s summary %q", action, summary)
		}
	}
}

// Denial on the runtime owner card leaves the grant untouched and surfaces
// "denied by user" in the reply, never running the helper.
func TestLinksInstallRequiresApproval(t *testing.T) {
	k, endpoint := linksRuntimeFixture(t)
	var reply cliops.LinksReply
	var callErr error
	runLinksApprove(t, k, func(name, summary string) (bool, error) {
		if name != "sh" || !strings.Contains(summary, "install browser") {
			t.Errorf("card %s %q", name, summary)
		}
		return false, nil
	}, "install", "browser", func(ctx context.Context) {
		reply, callErr = cliops.CallLinksInstall(context.Background(), endpoint, k.Bot.Root, tools.CallID(ctx), cliops.LinksInstall{ID: "browser"})
	})
	if callErr == nil || !strings.Contains(reply.Error, "denied by user") {
		t.Fatalf("denied install reply=%+v err=%v", reply, callErr)
	}
	if k.Bot.Grants(link.NameBrowser) {
		t.Fatal("granted without approval")
	}
}

func TestLinksInstallAllowGrants(t *testing.T) {
	k, endpoint := linksRuntimeFixture(t)
	var reply cliops.LinksReply
	var callErr error
	runLinksApprove(t, k, func(string, string) (bool, error) { return true, nil }, "install", "browser", func(ctx context.Context) {
		reply, callErr = cliops.CallLinksInstall(context.Background(), endpoint, k.Bot.Root, tools.CallID(ctx), cliops.LinksInstall{ID: "browser"})
	})
	if callErr != nil || reply.Text != "installed browser" {
		t.Fatalf("approved install reply=%+v err=%v", reply, callErr)
	}
	if !k.Bot.Grants(link.NameBrowser) {
		t.Fatal("not granted")
	}
}

func TestLinksInstallHidesSecrets(t *testing.T) {
	const secret = "super-secret-value"
	k, endpoint := linksRuntimeFixture(t)
	var reply cliops.LinksReply
	var callErr error
	runLinksApprove(t, k, func(_ string, summary string) (bool, error) {
		if !strings.Contains(summary, "secrets: TELEGRAM_BOT_TOKEN") {
			t.Errorf("card %q", summary)
		}
		k.StageSecrets("telegram", map[string]string{"TELEGRAM_BOT_TOKEN": secret})
		return true, nil
	}, "install", "telegram", func(ctx context.Context) {
		reply, callErr = cliops.CallLinksInstall(context.Background(), endpoint, k.Bot.Root, tools.CallID(ctx), cliops.LinksInstall{ID: "telegram"})
	})
	if callErr != nil || reply.Text != "installed telegram" || strings.Contains(reply.Text, secret) || strings.Contains(reply.Error, secret) {
		t.Fatalf("install reply=%+v err=%v", reply, callErr)
	}
	b := k.Bot
	if !b.Grants(link.NameTelegram) {
		t.Fatal("telegram was not granted")
	}
	raw, err := os.ReadFile(b.VaultDir() + "/secrets.enc")
	if err != nil || strings.Contains(string(raw), secret) {
		t.Fatal("secret missing from encrypted global vault or stored in plaintext")
	}
	if got, ok := link.GlobalSecretLookup(b.Root)("telegram.token"); !ok || got != secret {
		t.Fatal("shared token missing")
	}
	info, err := os.Stat(b.VaultDir() + "/secrets.enc")
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("mode %v %v", info, err)
	}
	if len(k.takeSecrets("telegram")) != 0 {
		t.Fatal("staged secrets left after install")
	}
}

// An approved card that collects no key still does not grant: the staged
// secret stage is the only way a value reaches the helper.
func TestLinksInstallWaitsForSecretFill(t *testing.T) {
	k, endpoint := linksRuntimeFixture(t)
	var reply cliops.LinksReply
	var callErr error
	runLinksApprove(t, k, func(string, string) (bool, error) { return true, nil }, "install", "telegram", func(ctx context.Context) {
		reply, callErr = cliops.CallLinksInstall(context.Background(), endpoint, k.Bot.Root, tools.CallID(ctx), cliops.LinksInstall{ID: "telegram"})
	})
	if callErr != nil || !strings.Contains(reply.Text, "owner did not fill") || !strings.Contains(reply.Text, "TELEGRAM_BOT_TOKEN") {
		t.Fatalf("unfilled install reply=%+v err=%v", reply, callErr)
	}
	if k.Bot.Grants("telegram") {
		t.Fatal("granted")
	}
	if _, err := os.Stat(k.Bot.SecretPath()); err == nil {
		t.Fatal("secrets file created")
	}
}

func TestLinksRemoveRequiresApproval(t *testing.T) {
	k, endpoint := linksRuntimeFixture(t)
	if err := k.Bot.GrantLink(link.NameBrowser); err != nil {
		t.Fatal(err)
	}
	var reply cliops.LinksReply
	var callErr error
	asked := false
	runLinksApprove(t, k, func(name, summary string) (bool, error) {
		asked = true
		if name != "sh" || !strings.Contains(summary, "remove browser") {
			t.Errorf("card %s %q", name, summary)
		}
		return true, nil
	}, "remove", "browser", func(ctx context.Context) {
		reply, callErr = cliops.CallLinksRemove(context.Background(), endpoint, k.Bot.Root, tools.CallID(ctx), cliops.LinksRemove{ID: "browser"})
	})
	if !asked {
		t.Fatal("remove did not ask for approval")
	}
	if callErr != nil || reply.Text != "removed browser" {
		t.Fatalf("remove reply=%+v err=%v", reply, callErr)
	}
	if k.Bot.Grants(link.NameBrowser) {
		t.Fatal("still granted")
	}
}

func TestInstalledPromptGrowsOneLine(t *testing.T) {
	b := testBot(t, inbox.NameTerminal)
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

func TestLinksSearchMatchesEnglish(t *testing.T) {
	b := testBot(t, inbox.NameTerminal)
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
	b := testBot(t, inbox.NameTerminal)
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
	b := testBot(t, inbox.NameTerminal)
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
	b := testBot(t, inbox.NameTerminal, "node")
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
	b := testBot(t, inbox.NameTerminal)
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
	if registered(k, "recall") {
		t.Fatal("installing a connector restored retired recall")
	}
	for _, name := range []string{"read_file", "code_start"} {
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

// runLinksApprove opens the same real prepared Shell call runLinksSource
// does, but with an approver: the runtime install/remove handlers reach it as
// the original Shell approver and send the Confirm-shaped owner card.
func runLinksApprove(t *testing.T, k *Kernel, approve func(name, summary string) (bool, error), action, id string, run func(context.Context)) {
	t.Helper()
	runRoutineSource(t, k, agent.Approver(func(_ context.Context, name, summary string) (bool, error) {
		return approve(name, summary)
	}), `{"command":"\"$XBOT_BIN\" links `+action+` --self `+id+`"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
		run(ctx)
		return "completed", nil
	})
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
