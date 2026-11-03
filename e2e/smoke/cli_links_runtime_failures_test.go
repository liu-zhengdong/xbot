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
)

// Actual failure semantics through the original source Native Shell. Each case
// keeps one genuine prepared call context; no bare fake, public USER append or
// fabricated Kernel ctx is used.
func TestCLILinksRuntimeFailureSemantics(t *testing.T) {
	cliSkillIsolation(t)
	model := newMCPRuntimeModel(t)
	srv := Start(t, &Stub{srv: model.server})
	const source = "LinksFailureSource"
	srv.CreateBot(source, "Preserve actual links runtime failure facts under the original prepared source call.")
	var primary struct{ Name string }
	primaryRaw, err := os.ReadFile(filepath.Join(srv.Root, "primary.json"))
	if err != nil || json.Unmarshal(primaryRaw, &primary) != nil || primary.Name == "" || primary.Name == source {
		t.Fatal("links failure source must be actually nonprimary")
	}
	run := func(cue, command string) string {
		t.Helper()
		model.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "Preserve actual links failure facts under the original prepared source call", "command": command}))
		cliSkillSend(t, srv, source, cue)
		var got string
		WaitFor(t, 10*time.Second, "actual links failure Shell result", func() (bool, string) {
			results := model.results(cue)
			if len(results) == 1 && results[0] != "" {
				got = results[0]
				return true, ""
			}
			return false, fmt.Sprintf("results=%.300q", results)
		})
		return got
	}
	decode := func(t *testing.T, result string) cliLinksReply {
		t.Helper()
		var reply cliLinksReply
		decoder := json.NewDecoder(strings.NewReader(commandOutput(result)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&reply); err != nil {
			t.Fatalf("failure must still return the strict typed reply: %v; %.900q", err, result)
		}
		return reply
	}
	// The real Native Shell job id (sh_*) is the actual XBOT_CALL_ID, the same
	// evidence compact keeps. An empty value is rejected client-side before any
	// reply exists: the original compact_runtime.go:73 message goes to stderr.
	missing := run("LINKS_FAIL_CALL_ID_MISSING", `env XBOT_CALL_ID= "$XBOT_BIN" links search --self --json`)
	if strings.HasPrefix(missing, "exit 0\n") {
		t.Fatalf("missing XBOT_CALL_ID silently succeeded: %.900q", missing)
	}
	if !strings.Contains(commandOutput(missing), "links: requires XBOT_CALL_ID from an active XBot shell") {
		t.Fatalf("missing XBOT_CALL_ID lost its original rejection: %.900q", missing)
	}
	// A foreign/bogus call id reaches the runtime but cannot resolve a live
	// prepared source call: the original unavailable fact is returned.
	foreign := run("LINKS_FAIL_CALL_ID_FOREIGN", `env XBOT_CALL_ID=bogus-not-a-shell-job "$XBOT_BIN" links search --self --json`)
	if strings.HasPrefix(foreign, "exit 0\n") {
		t.Fatalf("foreign XBOT_CALL_ID silently succeeded: %.900q", foreign)
	}
	foreignReply := decode(t, foreign)
	if !strings.Contains(foreignReply.Error, "source approval context unavailable") {
		t.Fatalf("foreign XBOT_CALL_ID lost the actual unavailable error: %+v", foreignReply)
	}
	// Bare noun, unknown subcommand and missing --self keep their parser rejections.
	for _, c := range []struct {
		name, command, want string
	}{
		{"bare", `"$XBOT_BIN" links`, "links: expected search or load"},
		{"unknown_subcommand", `"$XBOT_BIN" links peek --self --json`, `links: unexpected subcommand "peek"`},
		{"search_missing_self", `"$XBOT_BIN" links search --json`, "links: requires --self from an active XBot shell"},
		{"load_missing_self", `"$XBOT_BIN" links load browser --json`, "links: requires --self from an active XBot shell"},
		{"load_missing_id", `"$XBOT_BIN" links load --self --json`, "links: load requires exactly one id or name"},
	} {
		result := run("LINKS_FAIL_PARSE_"+strings.ToUpper(c.name), c.command)
		if strings.HasPrefix(result, "exit 0\n") {
			t.Fatalf("%s was not rejected: %.900q", c.name, result)
		}
		if !strings.Contains(commandOutput(result), c.want) {
			t.Fatalf("%s lost its original rejection %q: %.900q", c.name, c.want, result)
		}
	}
	// Loading an id that exists nowhere keeps the original not-found error.
	notFound := run("LINKS_FAIL_LOAD_NOT_FOUND", `"$XBOT_BIN" links load --self no-such-link-anywhere --json`)
	if strings.HasPrefix(notFound, "exit 0\n") {
		t.Fatalf("unknown load id silently succeeded: %.900q", notFound)
	}
	notFoundReply := decode(t, notFound)
	if !strings.Contains(notFoundReply.Error, "links: not found") {
		t.Fatalf("unknown load id lost its original error: %+v", notFoundReply)
	}
	// The model-side door keeps its pointing errors: search and load are CLI
	// only, so the tool returns the actual pointing text, not a reimplemented result.
	for _, c := range []struct {
		name string
		args map[string]string
		want string
	}{
		{"model_search", map[string]string{"action": "search"}, `"$XBOT_BIN" links search --self`},
		{"model_load", map[string]string{"action": "load", "id": "browser"}, `"$XBOT_BIN" links load --self <id|name>`},
	} {
		model.add("LINKS_FAIL_"+strings.ToUpper(c.name), mcpRuntimeTool("links", c.args))
		cliSkillSend(t, srv, source, "LINKS_FAIL_"+strings.ToUpper(c.name))
		var got []string
		WaitFor(t, 10*time.Second, "actual links tool result", func() (bool, string) {
			results := model.results("LINKS_FAIL_" + strings.ToUpper(c.name))
			if len(results) == 1 && results[0] != "" {
				got = results
				return true, ""
			}
			return false, fmt.Sprintf("results=%.300q", results)
		})
		if !strings.Contains(got[0], c.want) {
			t.Fatalf("model-side links %s lost its CLI pointing error: %.900q", c.name, got)
		}
	}
}
