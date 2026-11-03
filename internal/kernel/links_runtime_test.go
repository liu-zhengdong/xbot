package kernel

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/config"
	"xbot/internal/link"
	"xbot/internal/mcp"
	"xbot/internal/tools"
)

// linksRuntimeFixture starts a real routed source listener on a running
// kernel whose Bot already grants the store connector under test.
func linksRuntimeFixture(t *testing.T, grants ...string) (*Kernel, string) {
	t.Helper()
	k, endpoint := experienceRuntimeFixture(t, true)
	for _, grant := range grants {
		if err := bot.GrantGlobal(k.Bot.Root, grant); err != nil {
			t.Fatal(err)
		}
	}
	return k, endpoint
}

// runLinksSource opens a real prepared Shell call on the live kernel and runs
// the supplied work inside its frozen source context; CallLinks* uses the
// original call ID the job machinery bound for it.
func runLinksSource(t *testing.T, k *Kernel, run func(context.Context, json.RawMessage) (string, error)) {
	t.Helper()
	runRoutineSource(t, k, nil, `{"command":"\"$XBOT_BIN\" links search --self"}`, run)
}

func TestLinksRuntimeSearchReachesOriginalCatalog(t *testing.T) {
	k, endpoint := linksRuntimeFixture(t)
	var reply cliops.LinksReply
	var callErr error
	runLinksSource(t, k, func(ctx context.Context, _ json.RawMessage) (string, error) {
		reply, callErr = cliops.CallLinksSearch(context.Background(), endpoint, k.Bot.Root, tools.CallID(ctx), cliops.LinksSearch{Query: "telegram"})
		return "completed", nil
	})
	if callErr != nil || reply.Error != "" || !strings.Contains(reply.Text, "telegram") || !strings.Contains(reply.Text, "available") {
		t.Fatalf("catalog search reply=%+v err=%v", reply, callErr)
	}
}

func TestLinksRuntimeSearchEmptyQueryListsCatalog(t *testing.T) {
	k, endpoint := linksRuntimeFixture(t)
	var reply cliops.LinksReply
	var callErr error
	runLinksSource(t, k, func(ctx context.Context, _ json.RawMessage) (string, error) {
		reply, callErr = cliops.CallLinksSearch(context.Background(), endpoint, k.Bot.Root, tools.CallID(ctx), cliops.LinksSearch{})
		return "completed", nil
	})
	if callErr != nil || reply.Error != "" {
		t.Fatalf("empty query reply=%+v err=%v", reply, callErr)
	}
	for _, id := range []string{"telegram", "browser"} {
		if !strings.Contains(reply.Text, id) {
			t.Fatalf("empty query catalog missing %s: %q", id, reply.Text)
		}
	}
}

func TestLinksRuntimeLoadMarksLoadedAndListsTools(t *testing.T) {
	k, endpoint := linksRuntimeFixture(t, link.NameTelegram)
	if k.isLoaded(link.NameTelegram) {
		t.Fatal("telegram already loaded before the call")
	}
	var reply cliops.LinksReply
	var callErr error
	var frozen *turn
	runLinksSource(t, k, func(ctx context.Context, _ json.RawMessage) (string, error) {
		frozen, _ = ctx.Value(turnKey{}).(*turn)
		reply, callErr = cliops.CallLinksLoad(context.Background(), endpoint, k.Bot.Root, tools.CallID(ctx), cliops.LinksLoad{ID: "telegram"})
		return "completed", nil
	})
	if callErr != nil || reply.Error != "" || !strings.HasPrefix(reply.Text, "loaded telegram:") || !strings.Contains(reply.Text, "telegram_send") {
		t.Fatalf("load reply=%+v err=%v", reply, callErr)
	}
	if frozen == nil || !k.isLoaded(link.NameTelegram) {
		t.Fatalf("load did not mark the running kernel loaded=%v frozen=%v", k.isLoaded(link.NameTelegram), frozen != nil)
	}
}

func TestLinksRuntimeLoadByNameResolvesAlias(t *testing.T) {
	k, endpoint := linksRuntimeFixture(t, link.NameBrowser)
	var reply cliops.LinksReply
	var callErr error
	runLinksSource(t, k, func(ctx context.Context, _ json.RawMessage) (string, error) {
		reply, callErr = cliops.CallLinksLoad(context.Background(), endpoint, k.Bot.Root, tools.CallID(ctx), cliops.LinksLoad{ID: "浏览器"})
		return "completed", nil
	})
	if callErr != nil || reply.Error != "" || !strings.Contains(reply.Text, "browser_open") || !k.isLoaded(link.NameBrowser) {
		t.Fatalf("alias load reply=%+v loaded=%v err=%v", reply, k.isLoaded(link.NameBrowser), callErr)
	}
}

func TestLinksRuntimeLoadMCPPointsAtCLI(t *testing.T) {
	k, endpoint := linksRuntimeFixture(t)
	if err := link.EnableConnector(k.Bot.GlobalMCPPath(), "playwright"); err != nil {
		t.Fatal(err)
	}
	var reply cliops.LinksReply
	var callErr error
	runLinksSource(t, k, func(ctx context.Context, _ json.RawMessage) (string, error) {
		reply, callErr = cliops.CallLinksLoad(context.Background(), endpoint, k.Bot.Root, tools.CallID(ctx), cliops.LinksLoad{ID: "mcp:playwright"})
		return "completed", nil
	})
	if callErr == nil || !strings.Contains(reply.Error, "mcp tools") || !strings.Contains(reply.Error, "$XBOT_BIN") {
		t.Fatalf("mcp load must point at the CLI reply=%+v err=%v", reply, callErr)
	}
}

func TestLinksRuntimeLoadNotInstalledAndUnknown(t *testing.T) {
	k, endpoint := linksRuntimeFixture(t)
	for _, tc := range []struct{ id, want string }{
		{"telegram", "not installed"},
		{"nonexistent-thing", "not found"},
		{"", "id is required"},
	} {
		t.Run(tc.id, func(t *testing.T) {
			var reply cliops.LinksReply
			var callErr error
			runLinksSource(t, k, func(ctx context.Context, _ json.RawMessage) (string, error) {
				reply, callErr = cliops.CallLinksLoad(context.Background(), endpoint, k.Bot.Root, tools.CallID(ctx), cliops.LinksLoad{ID: tc.id})
				return "completed", nil
			})
			if callErr == nil || reply.Text != "" || !strings.Contains(reply.Error, tc.want) {
				t.Fatalf("id=%q reply=%+v want=%q err=%v", tc.id, reply, tc.want, callErr)
			}
		})
	}
}

func TestLinksRuntimeScrubsSecretValuesFromErrors(t *testing.T) {
	secret := "telegram-secret-token-value"
	k, endpoint := linksRuntimeFixture(t)
	if err := link.WriteSecrets(k.Bot.SecretPath(), map[string]string{"TELEGRAM_BOT_TOKEN": secret}); err != nil {
		t.Fatal(err)
	}
	k.StageSecrets("telegram", map[string]string{"TELEGRAM_BOT_TOKEN": secret})
	var reply cliops.LinksReply
	var callErr error
	runLinksSource(t, k, func(ctx context.Context, _ json.RawMessage) (string, error) {
		reply, callErr = cliops.CallLinksLoad(context.Background(), endpoint, k.Bot.Root, tools.CallID(ctx), cliops.LinksLoad{ID: "mcp:secret-probe-" + secret})
		return "completed", nil
	})
	if callErr == nil {
		t.Fatal("mcp load must fail")
	}
	if strings.Contains(reply.Error, secret) {
		t.Fatalf("secret reached reply: %q", reply.Error)
	}
}

// linksSearchRuntime runs the original linksTool.search end to end: the
// output still passes through scrubSecrets with live secretValues, so a staged
// secret that occurs inside a real MCP row never reaches the caller.
func TestLinksRuntimeSearchScrubsStagedSecretValues(t *testing.T) {
	secret := "z9hiddenstore"
	k, endpoint := linksRuntimeFixture(t)
	if err := mcp.AddMCP(k.Bot.GlobalMCPPath(), mcp.Spec{Name: secret, Command: "/bin/echo"}, mcp.Options{IsProviderKey: config.ProviderKey}); err != nil {
		t.Fatal(err)
	}
	if k.Resources != nil {
		if err := k.Resources.MCP.Reconcile(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	k.StageSecrets("probe", map[string]string{"SOME_TOKEN": secret})
	var reply cliops.LinksReply
	var callErr error
	runLinksSource(t, k, func(ctx context.Context, _ json.RawMessage) (string, error) {
		reply, callErr = cliops.CallLinksSearch(context.Background(), endpoint, k.Bot.Root, tools.CallID(ctx), cliops.LinksSearch{Query: "mcp:"})
		return "completed", nil
	})
	if callErr != nil || strings.Contains(reply.Text, secret) {
		t.Fatalf("staged secret reached catalog output=%q err=%v", reply.Text, callErr)
	}
	if !strings.Contains(reply.Text, "mcp:[secret]") {
		t.Fatalf("scrubbed mcp row missing from catalog output=%q", reply.Text)
	}
}

func TestLinksRuntimeRejectsMissingSourceCall(t *testing.T) {
	k, _ := linksRuntimeFixture(t)
	for _, tc := range []struct {
		name string
		run  func(context.Context) (cliops.LinksReply, error)
	}{
		{"search", func(ctx context.Context) (cliops.LinksReply, error) {
			return k.linksSearchRuntime(ctx, cliops.LinksSearch{})
		}},
		{"load", func(ctx context.Context) (cliops.LinksReply, error) {
			return k.linksLoadRuntime(ctx, cliops.LinksLoad{ID: "telegram"})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reply, err := tc.run(context.Background())
			if err == nil || reply.Text != "" || !strings.Contains(reply.Error, "source approval context unavailable") {
				t.Fatalf("bare context accepted reply=%+v err=%v", reply, err)
			}
		})
	}
}

// linksProbeLink is a real grant Link whose Tools() runs inside the original
// loadContext, proving the runtime path consults this kernel's live links.
type linksProbeLink struct {
	loaded []string
}

func (l linksProbeLink) Name() string { return link.NameTelegram }
func (l linksProbeLink) Tools() []tools.Tool {
	out := make([]tools.Tool, 0, len(l.loaded))
	for _, name := range l.loaded {
		out = append(out, probeNamedTool(name))
	}
	return out
}

type probeNamedTool string

func (t probeNamedTool) Name() string        { return string(t) }
func (t probeNamedTool) Description() string { return "probe" }
func (t probeNamedTool) Parameters() any     { return map[string]any{"type": "object"} }
func (t probeNamedTool) Confirm(json.RawMessage) (bool, string, error) {
	return false, "", nil
}
func (t probeNamedTool) Run(context.Context, json.RawMessage) (string, error) { return "", nil }

// The runtime load marks the grant loaded on this running kernel and the
// rebuilt mounted set then declares what was deferred before the call.
func TestLinksRuntimeLoadMountsDeferredGrantTools(t *testing.T) {
	k, endpoint := linksRuntimeFixture(t, link.NameTelegram)
	k.linksMu.Lock()
	k.Links = append(k.Links, linksProbeLink{loaded: []string{"probe_telegram_tool"}})
	k.linksMu.Unlock()
	mounted, err := k.mountedTools()
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range mounted {
		if tool != nil && tool.Name() == "probe_telegram_tool" {
			t.Fatal("deferred grant tool mounted before the runtime load")
		}
	}
	var reply cliops.LinksReply
	var callErr error
	runLinksSource(t, k, func(ctx context.Context, _ json.RawMessage) (string, error) {
		reply, callErr = cliops.CallLinksLoad(context.Background(), endpoint, k.Bot.Root, tools.CallID(ctx), cliops.LinksLoad{ID: "telegram"})
		return "completed", nil
	})
	if callErr != nil || !strings.Contains(reply.Text, "probe_telegram_tool") {
		t.Fatalf("probe load reply=%+v err=%v", reply, callErr)
	}
	if !k.isLoaded(link.NameTelegram) {
		t.Fatal("load did not mark the grant loaded")
	}
	mounted, err = k.mountedTools()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tool := range mounted {
		if tool != nil && tool.Name() == "probe_telegram_tool" {
			found = true
		}
	}
	if !found {
		t.Fatal("grant tool still deferred after the runtime load")
	}
}
