package kernel

import (
	"context"
	"os"
	"strings"
	"testing"

	"xbot/internal/cliops"
	"xbot/internal/config"
	"xbot/internal/link"
	"xbot/internal/mcp"
	"xbot/internal/tools"
)

// A store server that names a key (Context7) installs through the chat card
// like Telegram: the card lists the key, nothing is written until it is
// filled, an old keyless entry is upgraded, the shared backend holds the key,
// and remove takes the entry and the key away.
func TestLinksInstallStoreConnectorWithKey(t *testing.T) {
	const secret = "ctx7sk-test-value-123"
	b := testBot(t, link.NameTerminal)
	t.Setenv("CONTEXT7_API_KEY", "")
	k := &Kernel{Bot: b, Links: []link.Link{link.Terminal{}}}
	tool := linksTool{k}
	for _, id := range []string{"context7", "mcp:context7", "Context7"} {
		if summary := cardSummary("install", id); !strings.HasPrefix(summary, "install context7\n") || !strings.Contains(summary, "\nsecrets: CONTEXT7_API_KEY") {
			t.Fatalf("%s card %q", id, summary)
		}
	}
	// No key filled: nothing written, nothing claimed.
	out, err := tool.install("context7")
	if err != nil || !strings.Contains(out, "did not fill CONTEXT7_API_KEY") || strings.Contains(out, "installed") {
		t.Fatalf("missing key %q %v", out, err)
	}
	if _, err := os.Stat(b.GlobalMCPPath()); err == nil {
		t.Fatal("entry written without its key")
	}
	if out := tool.search("context7"); !strings.Contains(out, "context7\tavailable") || !strings.Contains(out, "secrets: CONTEXT7_API_KEY") {
		t.Fatalf("search before %q", out)
	}
	// An entry left by the old keyless add is upgraded, not skipped.
	if err := mcp.AddMCP(b.GlobalMCPPath(), mcp.Spec{Name: "context7", Command: "npx", Args: []string{"-y", "@upstash/context7-mcp"}}, mcp.Options{IsProviderKey: config.ProviderKey}); err != nil {
		t.Fatal(err)
	}
	if out := tool.search("context7"); !strings.Contains(out, "context7\tavailable") {
		t.Fatalf("keyless entry reads installed %q", out)
	}
	// Declaring the name without a stored value is not installed either.
	if err := link.EnableConnector(b.GlobalMCPPath(), "context7"); err != nil {
		t.Fatal(err)
	}
	if out := tool.search("context7"); !strings.Contains(out, "context7\tavailable") {
		t.Fatalf("name without a value reads installed %q", out)
	}
	if err := mcp.RemoveMCP(b.GlobalMCPPath(), "context7", mcp.Options{IsProviderKey: config.ProviderKey}); err != nil {
		t.Fatal(err)
	}
	if err := mcp.AddMCP(b.GlobalMCPPath(), mcp.Spec{Name: "context7", Command: "npx", Args: []string{"-y", "@upstash/context7-mcp"}}, mcp.Options{IsProviderKey: config.ProviderKey}); err != nil {
		t.Fatal(err)
	}
	k.StageSecrets("context7", map[string]string{"CONTEXT7_API_KEY": secret})
	out, err = tool.install("mcp:context7")
	if err != nil || out != "installed context7" {
		t.Fatalf("install %q %v", out, err)
	}
	global, _ := mcp.LoadMCP(b.GlobalMCPPath(), mcp.Options{IsProviderKey: config.ProviderKey})
	if len(global) != 1 || !mcp.HasSecretEnvs(global[0], []string{"CONTEXT7_API_KEY"}) {
		t.Fatalf("global %+v", global)
	}
	raw, _ := os.ReadFile(b.GlobalMCPPath())
	if strings.Contains(string(raw), secret) {
		t.Fatal("key value in mcp.json")
	}
	if got, ok := link.GlobalSecretLookup(b.Root)("CONTEXT7_API_KEY"); !ok || got != secret {
		t.Fatal("global MCP credential missing")
	}
	if out := tool.search("context7"); !strings.Contains(out, "context7\tinstalled") || strings.Contains(out, secret) {
		t.Fatalf("search after %q", out)
	}
	// Installing again without a fresh key changes nothing.
	if out, _ := tool.install("context7"); strings.Contains(out, "installed") {
		t.Fatalf("second install %q", out)
	}
	if global, _ := mcp.LoadMCP(b.GlobalMCPPath(), mcp.Options{IsProviderKey: config.ProviderKey}); len(global) != 1 {
		t.Fatalf("second install touched the entry %+v", global)
	}
	for i := 0; i < 2; i++ {
		if out, err := tool.remove("Context7"); err != nil || out != "removed context7" {
			t.Fatalf("remove %q %v", out, err)
		}
	}
	if global, _ := mcp.LoadMCP(b.GlobalMCPPath(), mcp.Options{IsProviderKey: config.ProviderKey}); len(global) != 0 {
		t.Fatalf("left %+v", global)
	}
	if _, ok := link.GlobalSecretLookup(b.Root)("CONTEXT7_API_KEY"); ok {
		t.Fatal("shared key left behind")
	}
}

// The card fills the key through the runtime install call; the reply text
// the shell (and any transcript of it) sees never carries the value.
func TestLinksInstallStoreConnectorHidesKey(t *testing.T) {
	const secret = "ctx7sk-turn-secret-456"
	k, endpoint := linksRuntimeFixture(t)
	var reply cliops.LinksReply
	var callErr error
	runLinksApprove(t, k, func(_ string, summary string) (bool, error) {
		if !strings.Contains(summary, "secrets: CONTEXT7_API_KEY") {
			t.Errorf("card %q", summary)
		}
		k.StageSecrets("context7", map[string]string{"CONTEXT7_API_KEY": secret})
		return true, nil
	}, "install", "context7", func(ctx context.Context) {
		reply, callErr = cliops.CallLinksInstall(context.Background(), endpoint, k.Bot.Root, tools.CallID(ctx), cliops.LinksInstall{ID: "context7"})
	})
	if callErr != nil || reply.Text != "installed context7" || strings.Contains(reply.Text, secret) || strings.Contains(reply.Error, secret) {
		t.Fatalf("connector install reply=%+v err=%v", reply, callErr)
	}
	if global, _ := mcp.LoadMCP(k.Bot.GlobalMCPPath(), mcp.Options{IsProviderKey: config.ProviderKey}); len(global) != 1 {
		t.Fatalf("connector entry %+v", global)
	}
	if got, ok := link.GlobalSecretLookup(k.Bot.Root)("CONTEXT7_API_KEY"); !ok || got != secret {
		t.Fatal("global connector credential missing")
	}
	if len(k.takeSecrets("context7")) != 0 {
		t.Fatal("staged secrets left after install")
	}
}

// A blank value in this Bot's store masks a non-blank environment value at
// start, so search must not call it installed.
func TestLinksSearchBlankStoreMasksEnvironment(t *testing.T) {
	t.Setenv("CONTEXT7_API_KEY", "ctx7sk-env")
	b := testBot(t, link.NameTerminal)
	tool := linksTool{&Kernel{Bot: b, Links: []link.Link{link.Terminal{}}}}
	if err := link.EnableConnector(b.GlobalMCPPath(), "context7"); err != nil {
		t.Fatal(err)
	}
	if out := tool.search("context7"); !strings.Contains(out, "context7\tinstalled") {
		t.Fatalf("env key %q", out)
	}
	if err := link.WriteSecrets(b.SecretPath(), map[string]string{"CONTEXT7_API_KEY": "   "}); err != nil {
		t.Fatal(err)
	}
	if out := tool.search("context7"); !strings.Contains(out, "context7\tavailable") {
		t.Fatalf("blank store plus env %q", out)
	}
}
