package kernel

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"xbot/internal/link"
	"xbot/internal/provider"
)

// A store server that names a key (Context7) installs through the chat card
// like Telegram: the card lists the key, nothing is written until it is
// filled, an old keyless entry is upgraded, the running server gets the key
// from this Bot's store, and remove takes the entry and the key away.
func TestLinksInstallStoreConnectorWithKey(t *testing.T) {
	const secret = "ctx7sk-test-value-123"
	b := testBot(t, link.NameTerminal)
	var mu sync.Mutex
	var mounted []link.MCPServer
	k := &Kernel{Bot: b, Links: []link.Link{link.Terminal{}}}
	k.newMCP = func(s link.MCPServer) link.Link {
		mu.Lock()
		mounted = append(mounted, s)
		mu.Unlock()
		s.Dial = fakeMCP(t, "resolve")
		return link.NewMCP(s)
	}
	tool := linksTool{k}
	for _, id := range []string{"context7", "mcp:context7", "Context7"} {
		args, _ := json.Marshal(map[string]string{"action": "install", "id": id})
		_, summary, err := tool.Confirm(args)
		if err != nil || !strings.HasPrefix(summary, "install context7\n") || !strings.Contains(summary, "\nsecrets: CONTEXT7_API_KEY") {
			t.Fatalf("%s card %q %v", id, summary, err)
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
	if err := link.AddMCP(b.GlobalMCPPath(), link.MCPServer{Name: "context7", Command: "npx", Args: []string{"-y", "@upstash/context7-mcp"}}); err != nil {
		t.Fatal(err)
	}
	if out := tool.search("context7"); !strings.Contains(out, "context7\tavailable") {
		t.Fatalf("keyless entry reads installed %q", out)
	}
	k.StageSecrets("context7", map[string]string{"CONTEXT7_API_KEY": secret})
	out, err = tool.install("mcp:context7")
	if err != nil || out != "installed context7" {
		t.Fatalf("install %q %v", out, err)
	}
	global, _ := link.LoadMCP(b.GlobalMCPPath())
	if len(global) != 1 || !link.HasSecretEnvs(global[0], []string{"CONTEXT7_API_KEY"}) {
		t.Fatalf("global %+v", global)
	}
	raw, _ := os.ReadFile(b.GlobalMCPPath())
	if strings.Contains(string(raw), secret) {
		t.Fatal("key value in mcp.json")
	}
	if got, _ := link.ReadSecrets(b.SecretPath()); got["CONTEXT7_API_KEY"] != secret {
		t.Fatalf("secrets file %v", got)
	}
	if out := tool.search("context7"); !strings.Contains(out, "context7\tinstalled") || strings.Contains(out, secret) {
		t.Fatalf("search after %q", out)
	}
	if got, err := tool.load("Context7"); err != nil || !strings.Contains(got, "resolve") {
		t.Fatalf("load %q %v", got, err)
	}
	mu.Lock()
	last := mounted[len(mounted)-1]
	mu.Unlock()
	if last.Secret == nil {
		t.Fatal("mounted server has no key lookup")
	}
	if v, ok := last.Secret("CONTEXT7_API_KEY"); !ok || v != secret {
		t.Fatalf("lookup %q %v", v, ok)
	}
	// Installing again without a fresh key changes nothing.
	if out, _ := tool.install("context7"); strings.Contains(out, "installed") {
		t.Fatalf("second install %q", out)
	}
	if global, _ := link.LoadMCP(b.GlobalMCPPath()); len(global) != 1 {
		t.Fatalf("second install touched the entry %+v", global)
	}
	for i := 0; i < 2; i++ {
		if out, err := tool.remove("Context7"); err != nil || out != "removed context7" {
			t.Fatalf("remove %q %v", out, err)
		}
	}
	if global, _ := link.LoadMCP(b.GlobalMCPPath()); len(global) != 0 {
		t.Fatalf("left %+v", global)
	}
	if got, _ := link.ReadSecrets(b.SecretPath()); got["CONTEXT7_API_KEY"] != "" {
		t.Fatal("key left behind")
	}
}

// The whole turn: the card fills the key, the model sees only the outcome.
func TestLinksInstallStoreConnectorHidesKey(t *testing.T) {
	const secret = "ctx7sk-turn-secret-456"
	b := testBot(t, link.NameTerminal)
	p := &scripted{resps: []provider.Message{
		linksCall("install", "context7"),
		{Role: provider.RoleAssistant, Content: provider.TextContent("done")},
	}}
	k := &Kernel{Bot: b, Provider: p, MaxSteps: 4}
	k.newMCP = func(s link.MCPServer) link.Link { s.Dial = fakeMCP(t, "resolve"); return link.NewMCP(s) }
	k.Approve = func(_ context.Context, _ string, summary string) (bool, error) {
		if !strings.Contains(summary, "secrets: CONTEXT7_API_KEY") {
			t.Errorf("card %q", summary)
		}
		k.StageSecrets("context7", map[string]string{"CONTEXT7_API_KEY": secret})
		return true, nil
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "请帮我安装 Context7", time.Unix(1, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	text := transcriptText(t, b)
	if strings.Contains(text, secret) {
		t.Fatal("key reached the transcript")
	}
	if !strings.Contains(text, "installed context7") {
		t.Fatal(text)
	}
	for _, req := range p.reqs {
		raw, _ := json.Marshal(req.Messages)
		if strings.Contains(string(raw), secret) {
			t.Fatal("key reached the model")
		}
	}
	if len(p.reqs) < 2 {
		t.Fatalf("model saw %d requests", len(p.reqs))
	}
}
