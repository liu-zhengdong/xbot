//go:build smoke

package smoke

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/link"
	"xbot/internal/vault"
)

// Independent wire view of the original links reply; production LinksReply
// keeps text/error only. No implementation DTO is used to manufacture it.
type cliLinksReply struct {
	Text  string `json:"text,omitempty"`
	Error string `json:"error,omitempty"`
}

func cliLinksRun(t *testing.T, model *mcpRuntimeModel, srv *Server, source, cue, command string) string {
	t.Helper()
	model.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "Use the actual original source Native Shell and its prepared call context", "command": command}))
	cliSkillSend(t, srv, source, cue)
	// The tool result reaches the fixture on the next provider request; wait for
	// the one actual Native Shell result rather than sampling a possibly-empty
	// slot the SSE done event can precede by a race.
	var got string
	WaitFor(t, 10*time.Second, "actual links Native Shell result", func() (bool, string) {
		results := model.results(cue)
		if len(results) == 1 && results[0] != "" {
			got = results[0]
			return true, ""
		}
		return false, fmt.Sprintf("results=%.300q", results)
	})
	return got
}

func cliLinksRunOK(t *testing.T, model *mcpRuntimeModel, srv *Server, source, cue, command string) string {
	t.Helper()
	result := cliLinksRun(t, model, srv, source, cue, command)
	if !strings.HasPrefix(result, "exit 0\n") {
		t.Fatalf("source links CLI did not succeed: %.900q", result)
	}
	return result
}

func cliLinksDecode(t *testing.T, result string) cliLinksReply {
	t.Helper()
	var reply cliLinksReply
	decoder := json.NewDecoder(strings.NewReader(commandOutput(result)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&reply); err != nil {
		t.Fatalf("source links reply is not one strict typed JSON object: %v; %.900q", err, result)
	}
	return reply
}

// First actual source chain: the original nonprimary source's real Native Shell
// drives the same search/load helpers through the running kernel's runtime CLI.
func TestCLILinksRuntimeSourceConsumers(t *testing.T) {
	cliSkillIsolation(t)
	model := newMCPRuntimeModel(t)
	srv := Start(t, &Stub{srv: model.server})
	const source = "LinksChainSource"
	srv.CreateBot(source, "Search and load links through the actual source runtime CLI.")
	var primary struct{ Name string }
	primaryRaw, err := os.ReadFile(filepath.Join(srv.Root, "primary.json"))
	if err != nil || json.Unmarshal(primaryRaw, &primary) != nil || primary.Name == "" || primary.Name == source {
		t.Fatal("links source must be actually nonprimary")
	}
	// One real global mcp.json row makes search produce an actual mcp: installed
	// line; no MCP process starts because search only reads specs. A context7
	// spec with its declared secret env plus the actual vault value below marks
	// that store connector row installed through HasSecretValues.
	mcpBytes, _ := json.Marshal(map[string]any{"servers": []any{
		map[string]string{"name": "links-chain-mcp", "url": "http://127.0.0.1:9/unreachable"},
		map[string]any{"name": "context7", "command": "npx", "args": []string{"-y", "@upstash/context7-mcp"}, "secret_envs": []string{"CONTEXT7_API_KEY"}},
	}})
	if err := os.WriteFile(filepath.Join(srv.Root, "mcp.json"), mcpBytes, 0600); err != nil {
		t.Fatal(err)
	}
	// The browser store grant is genuinely installed, which keeps its catalog
	// row installed and lets the lazy link's tools appear after load.
	if err := os.WriteFile(filepath.Join(srv.Root, "apps.json"), []byte(`{"links":["browser"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	// An actual product-vault secret; the catalog line names the key while the
	// value itself must never reach the scrubbed TSV.
	backend, err := vault.OpenGlobal(srv.Root)
	if err != nil {
		t.Fatal(err)
	}
	const secret = "LINKS_CHAIN_SECRET_VALUE_9f27b3"
	if err := backend.Set("CONTEXT7_API_KEY", secret, nil); err != nil {
		t.Fatal(err)
	}
	catalogIDs := map[string]bool{}
	for _, e := range link.Catalog() {
		catalogIDs[e.ID] = true
	}
	connectors, err := link.Connectors()
	if err != nil || len(connectors) == 0 {
		t.Fatal("actual embedded connector catalog is missing")
	}
	// search only lists connectors.json rows that a store card installs
	// (storeConnector): uncategorized rows stay out of the TSV, same as the
	// original helper.
	connectorIDs := map[string]bool{}
	for _, c := range connectors {
		if c.Category == "" {
			if app, ok := link.LookupApp(c.ID); ok && app.Kind == link.AppServer {
				t.Fatalf("uncategorized connector %q unexpectedly has a store card", c.ID)
			}
			continue
		}
		connectorIDs[c.ID] = true
	}
	// Full-catalog search through the original prepared source context.
	search := cliLinksDecode(t, cliLinksRunOK(t, model, srv, source, "LINKS_CAT_SCAN", `"$XBOT_BIN" links search --self --json`))
	if search.Error != "" || strings.TrimSpace(search.Text) == "" || search.Text == "no links" {
		t.Fatalf("source search did not return the original catalog text: %+v", search)
	}
	seen := map[string]string{}
	for _, line := range strings.Split(search.Text, "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) < 3 {
			t.Fatalf("search line lost the original TSV shape: %q", line)
		}
		if _, dup := seen[fields[0]]; dup {
			t.Fatalf("duplicate catalog row id %q", fields[0])
		}
		switch fields[1] {
		case "listed", "available", "installed":
		default:
			t.Fatalf("search row %q lost its three-state marker", fields[0])
		}
		seen[fields[0]] = fields[1]
	}
	for id := range catalogIDs {
		if seen[id] == "" {
			t.Fatalf("catalog row %q missing from the actual search TSV", id)
		}
	}
	for id := range connectorIDs {
		if seen[id] == "" {
			t.Fatalf("connectors.json row %q missing from the actual search TSV", id)
		}
	}
	if seen["browser"] != "installed" {
		t.Fatalf("granted browser row lost installed state: %v", seen["browser"])
	}
	if seen["context7"] != "installed" {
		t.Fatalf("secret-complete store connector lost installed state: %v", seen["context7"])
	}
	if seen["mcp:links-chain-mcp"] != "installed" {
		t.Fatalf("actual mcp.json row missing its installed line: %v", seen)
	}
	if len(seen) != len(catalogIDs)+len(connectorIDs)+1 {
		t.Fatalf("search row set drifted from catalog+connectors+installed-mcp: %v", seen)
	}
	if !strings.Contains(search.Text, "secrets: CONTEXT7_API_KEY") {
		t.Fatal("installed connector row lost its original secrets names suffix")
	}
	if strings.Contains(search.Text, secret) {
		t.Fatal("scrubSecrets leaked the actual vault value into the search text")
	}
	// Filtered search keeps only the actual matching row.
	filtered := cliLinksDecode(t, cliLinksRunOK(t, model, srv, source, "LINKS_CAT_FILTER", `"$XBOT_BIN" links search --self --query telegram --json`))
	if filtered.Error != "" || len(strings.Split(strings.TrimSpace(filtered.Text), "\n")) != 1 || !strings.HasPrefix(filtered.Text, "telegram\tavailable\t") || !strings.Contains(filtered.Text, "secrets: TELEGRAM_BOT_TOKEN") {
		t.Fatalf("query search did not return exactly the original telegram row: %+v", filtered)
	}
	// browser is an installed store link: load lists its real tool names on the
	// running kernel (toolsForLoaded over live snapshotLinks) and clears its
	// deferred marker (markLoaded) so the running kernel treats it as loaded.
	browser := cliLinksDecode(t, cliLinksRunOK(t, model, srv, source, "LINKS_APP_LOAD", `"$XBOT_BIN" links load --self browser --json`))
	if browser.Error != "" || !strings.HasPrefix(browser.Text, "loaded browser: ") || !strings.Contains(browser.Text, "browser_open") || !strings.Contains(browser.Text, "browser_snapshot") {
		t.Fatalf("link load did not keep the original loaded acknowledgement: %+v", browser)
	}
	// Loading the same id again stays on the same running kernel: the second
	// call is the idempotent original loadedNames path, not a fresh lookup.
	again := cliLinksDecode(t, cliLinksRunOK(t, model, srv, source, "LINKS_REP_LOAD", `"$XBOT_BIN" links load --self browser --json`))
	if again.Error != "" || again.Text != browser.Text {
		t.Fatalf("repeat load did not return the same original acknowledgement: %+v", again)
	}
	// An organ load goes through the original loadedNames(o.Grant) path and
	// still lists the organ's own tools on the running kernel.
	tools := cliLinksDecode(t, cliLinksRunOK(t, model, srv, source, "LINKS_ORG_LOAD", `"$XBOT_BIN" links load --self tools --json`))
	if tools.Error != "" || !strings.HasPrefix(tools.Text, "loaded tools: ") || !strings.Contains(tools.Text, "sh") || !strings.Contains(tools.Text, "read_file") {
		t.Fatalf("organ load lost its original loaded acknowledgement: %+v", tools)
	}
	// mcp: ids keep the original pointing error at the mcp CLI; nothing mounts.
	mcpLoad := cliLinksRun(t, model, srv, source, "LINKS_MCP_STOP", `"$XBOT_BIN" links load --self mcp:links-chain-mcp --json`)
	if strings.HasPrefix(mcpLoad, "exit 0\n") {
		t.Fatalf("mcp: load must keep its pointing error, not a success: %.900q", mcpLoad)
	}
	mcpReply := cliLinksDecode(t, mcpLoad)
	if !strings.Contains(mcpReply.Error, `"$XBOT_BIN" mcp tools "links-chain-mcp" --self --json`) {
		t.Fatalf("mcp: load lost its original mcp tools pointing error: %+v", mcpReply)
	}
	// A store connector name resolves to its mcp: form and takes the same path.
	storeLoad := cliLinksRun(t, model, srv, source, "LINKS_STORE_STOP", `"$XBOT_BIN" links load --self playwright --json`)
	if strings.HasPrefix(storeLoad, "exit 0\n") {
		t.Fatalf("store connector load must keep the mcp pointing error: %.900q", storeLoad)
	}
	storeReply := cliLinksDecode(t, storeLoad)
	if !strings.Contains(storeReply.Error, `"$XBOT_BIN" mcp tools "playwright" --self --json`) {
		t.Fatalf("store connector load lost the mcp pointing error: %+v", storeReply)
	}
}
