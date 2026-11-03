//go:build smoke

package smoke

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"xbot/internal/vault"
)

// Business E2E for the links install/remove runtime CLI. The original
// nonprimary source Native Shell drives install/remove --self through the
// running kernel: each call emits the real owner confirmation card (the
// permissive default does not short-circuit it), the owner decision goes
// through POST /api/bots/{name}/approvals/{id}, card-staged secrets feed
// takeSecrets/storeSecrets, and the granted side effects land on disk. No
// fixture approver, synthetic kernel ctx, or model-side links tool is used.
func TestCLILinksRuntimeMutate(t *testing.T) {
	cliSkillIsolation(t)
	model := newMCPRuntimeModel(t)
	srv := Start(t, &Stub{srv: model.server})
	const source = "LinksMutateSource"
	srv.CreateBot(source, "Install and remove connectors through the actual source runtime CLI owner card.")
	var primary struct{ Name string }
	primaryRaw, err := os.ReadFile(filepath.Join(srv.Root, "primary.json"))
	if err != nil || json.Unmarshal(primaryRaw, &primary) != nil || primary.Name == "" || primary.Name == source {
		t.Fatal("links mutate source must be actually nonprimary")
	}
	if on := permissionsOn(t, srv); on {
		t.Fatal("the mutate test proves the permissive default still emits the owner card; permissions must be off")
	}
	const token = "TELEGRAM_BOT_TOKEN"
	const tokenValue = "links-mutate-secret-a8c41f7e"

	jobID := func(cue string) string {
		t.Helper()
		var id string
		for _, result := range model.results(cue) {
			if match := regexp.MustCompile(`\bbackground (sh_[a-f0-9]+)`).FindStringSubmatch(result); len(match) == 2 {
				if id != "" {
					t.Fatalf("cue %s produced two background jobs: %v", cue, model.results(cue))
				}
				id = match[1]
			}
		}
		if id == "" {
			t.Fatalf("links mutate %s did not background on its owner card: %v", cue, model.results(cue))
		}
		return id
	}
	// One pending approval card for this source, found in the actual
	// transcript. The card is an owner card on the nonprimary bot, rebound to
	// the owner's current branch by runtimeApprovalSource.
	cardID := func(cue, firstLine string) string {
		t.Helper()
		var id string
		WaitFor(t, 10*time.Second, "actual owner card for "+cue, func() (bool, string) {
			// Card rows are append-only: the pending row stays in the ledger
			// after a later row resolves the same id, so only a card whose
			// latest row is still pending counts.
			latest := map[string]string{}
			for _, row := range srv.Transcript(source) {
				if !strings.HasPrefix(strings.TrimSpace(row.Content), "<approval_card ") {
					continue
				}
				match := regexp.MustCompile(`<approval_card id="([a-f0-9]+)"`).FindStringSubmatch(row.Content)
				if len(match) != 2 {
					continue
				}
				if state := regexp.MustCompile(` state="([^"]+)"`).FindStringSubmatch(row.Content); len(state) == 2 {
					latest[match[1]] = state[1] + "\x00" + row.Content
				}
			}
			var cards []string
			for cid, state := range latest {
				head, _, _ := strings.Cut(state, "\x00")
				if head == "pending" && strings.Contains(state, firstLine) {
					cards = append(cards, cid)
				}
			}
			if len(cards) == 1 {
				id = cards[0]
				return true, ""
			}
			return false, fmt.Sprintf("cards=%v latest=%v", cards, latest)
		})
		return id
	}
	decide := func(id string, secrets map[string]string) {
		t.Helper()
		status, body := srv.do(http.MethodPost, "/api/bots/"+url.PathEscape(source)+"/approvals/"+id, map[string]any{"allow": true, "secrets": secrets})
		if status != http.StatusOK {
			t.Fatalf("owner decision on card %s: HTTP %d %s", id, status, body)
		}
	}
	jobEnd := func(id string) (string, int) {
		t.Helper()
		var job struct {
			Status   string `json:"status"`
			ExitCode int    `json:"exit_code"`
			Output   string `json:"output"`
		}
		WaitFor(t, 15*time.Second, "links mutate job "+id+" completion", func() (bool, string) {
			raw, err := os.ReadFile(filepath.Join(srv.Root, "bots", source, "shell-jobs", id+".json"))
			if err != nil {
				return false, "job record not yet visible"
			}
			if json.Unmarshal(raw, &job) != nil {
				return false, "job record not complete"
			}
			return job.Status != "running", job.Status
		})
		raw, err := os.ReadFile(job.Output)
		if err != nil {
			t.Fatalf("links mutate job %s output unreadable: %v", id, err)
		}
		out := string(raw)
		if strings.Contains(out, tokenValue) {
			t.Fatalf("links mutate job %s leaked the staged secret value into its output", id)
		}
		if job.Status != "done" {
			t.Fatalf("links mutate job %s did not finish: status=%s exit=%d out=%.900q", id, job.Status, job.ExitCode, out)
		}
		return out, job.ExitCode
	}
	jobDone := func(id string) string {
		t.Helper()
		out, code := jobEnd(id)
		if code != 0 {
			t.Fatalf("links mutate job %s exited %d: %.900q", id, code, out)
		}
		return out
	}
	run := func(cue, command string) (string, string) {
		t.Helper()
		model.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "Mutate links through the actual source Native Shell owner card", "command": command}))
		cliSkillSend(t, srv, source, cue)
		id := jobID(cue)
		return id, ""
	}
	vaultNames := func() []string {
		t.Helper()
		backend, err := vault.OpenGlobal(srv.Root)
		if err != nil {
			t.Fatal(err)
		}
		return backend.Names()
	}
	// GrantLink writes the source bot's own config links list, the same store
	// EffectiveLinks reads back.
	grants := func() []string {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(srv.Root, "bots", source, "config.json"))
		if err != nil {
			t.Fatal(err)
		}
		var cfg struct {
			Links []string `json:"links"`
		}
		if err := json.Unmarshal(raw, &cfg); err != nil {
			t.Fatal(err)
		}
		return cfg.Links
	}
	searchState := func(cue, id string) string {
		t.Helper()
		reply := cliLinksDecode(t, cliLinksRunOK(t, model, srv, source, cue, `"$XBOT_BIN" links search --self --query `+id+` --json`))
		if reply.Error != "" {
			t.Fatalf("post-mutate search failed: %+v", reply)
		}
		for _, line := range strings.Split(reply.Text, "\n") {
			fields := strings.Split(line, "\t")
			if fields[0] == id && len(fields) > 1 {
				return fields[1]
			}
		}
		t.Fatalf("post-mutate search lost the %s row: %q", id, reply.Text)
		return ""
	}

	// Missing CallID keeps the client-side rejection before any card exists.
	missing := cliLinksRun(t, model, srv, source, "LINKS_MUTATE_NO_CALL_ID", `env XBOT_CALL_ID= "$XBOT_BIN" links install --self telegram --json`)
	if strings.HasPrefix(missing, "exit 0\n") || !strings.Contains(commandOutput(missing), "links: requires XBOT_CALL_ID from an active XBot shell") {
		t.Fatalf("missing XBOT_CALL_ID lost its original rejection: %.900q", missing)
	}
	// The model-side links tool is retired: install/remove have no second door.
	for _, action := range []string{"install", "remove"} {
		cue := "LINKS_MUTATE_TOOL_" + strings.ToUpper(action)
		model.add(cue, mcpRuntimeTool("links", map[string]string{"action": action, "id": "browser"}))
		cliSkillSend(t, srv, source, cue)
		var got []string
		WaitFor(t, 10*time.Second, "retired links tool result", func() (bool, string) {
			results := model.results(cue)
			if len(results) == 1 && results[0] != "" {
				got = results
				return true, ""
			}
			return false, fmt.Sprintf("results=%.300q", results)
		})
		if !strings.Contains(got[0], `unknown tool "links"`) {
			t.Fatalf("model-side links %s did not stay retired: %.900q", action, got)
		}
	}

	// install browser: a catalog link without secrets still emits the owner
	// card under the permissive default, then GrantLink writes apps.json.
	installBrowser, _ := run("LINKS_MUTATE_INSTALL_BROWSER", `"$XBOT_BIN" links install --self browser --json`)
	card := cardID("LINKS_MUTATE_INSTALL_BROWSER", "install browser")
	decide(card, nil)
	out := jobDone(installBrowser)
	reply := cliLinksDecode(t, out)
	if reply.Error != "" || reply.Text != "installed browser" {
		t.Fatalf("links install browser lost its original acknowledgement: %+v", reply)
	}
	if !contains(grants(), "browser") {
		t.Fatalf("approved browser install did not write apps.json grants: %v", grants())
	}
	if got := searchState("LINKS_MUTATE_SEARCH_BROWSER", "browser"); got != "installed" {
		t.Fatalf("post-install search did not mark browser installed: %q", got)
	}

	// install telegram: the card summary carries the key names and the owner
	// fills the value on the card itself; takeSecrets/storeSecrets land it in
	// the product vault without the value reaching any output.
	installTg, _ := run("LINKS_MUTATE_INSTALL_TELEGRAM", `"$XBOT_BIN" links install --self telegram --json`)
	tgCard := cardID("LINKS_MUTATE_INSTALL_TELEGRAM", "install telegram")
	var tgRow string
	for _, row := range srv.Transcript(source) {
		if strings.Contains(row.Content, `id="`+tgCard+`"`) {
			tgRow = row.Content
		}
	}
	if !strings.Contains(tgRow, "secrets: "+token) {
		t.Fatalf("install card lost its original secrets line: %q", tgRow)
	}
	if strings.Contains(tgRow, tokenValue) {
		t.Fatal("install card already carried the secret value")
	}
	decide(tgCard, map[string]string{token: tokenValue})
	out = jobDone(installTg)
	reply = cliLinksDecode(t, out)
	if reply.Error != "" || reply.Text != "installed telegram" {
		t.Fatalf("links install telegram lost its original acknowledgement: %+v", reply)
	}
	// Connector tokens are namespaced in the vault under their kind, the same
	// store dropSecrets removes through RemoveProductSecrets.
	if !contains(vaultNames(), "telegram.token") {
		t.Fatalf("approved telegram install did not stage the token into the vault: %v", vaultNames())
	}
	if got := searchState("LINKS_MUTATE_SEARCH_TELEGRAM", "telegram"); got != "installed" {
		t.Fatalf("post-install search did not mark telegram installed: %q", got)
	}
	// The staged value is stored, never printed: search output and transcript
	// carry key names only.
	for _, row := range srv.Transcript(source) {
		if strings.Contains(row.Content, tokenValue) {
			t.Fatalf("transcript leaked the staged secret value: %q", row.Content)
		}
	}

	// remove telegram: a fresh owner card approves RevokeLink + dropSecrets.
	removeTg, _ := run("LINKS_MUTATE_REMOVE_TELEGRAM", `"$XBOT_BIN" links remove --self telegram --json`)
	rmCard := cardID("LINKS_MUTATE_REMOVE_TELEGRAM", "remove telegram")
	var rmRow string
	for _, row := range srv.Transcript(source) {
		if strings.Contains(row.Content, `id="`+rmCard+`"`) {
			rmRow = row.Content
		}
	}
	if strings.Contains(rmRow, "secrets:") {
		t.Fatalf("remove card must not carry a secrets line: %q", rmRow)
	}
	decide(rmCard, nil)
	out = jobDone(removeTg)
	reply = cliLinksDecode(t, out)
	if reply.Error != "" || reply.Text != "removed telegram" {
		t.Fatalf("links remove telegram lost its original acknowledgement: %+v", reply)
	}
	if contains(vaultNames(), "telegram.token") {
		t.Fatalf("approved telegram remove left the token in the vault: %v", vaultNames())
	}
	if contains(grants(), "telegram") {
		t.Fatalf("approved telegram remove left its grant: %v", grants())
	}
	if got := searchState("LINKS_MUTATE_VERIFY_GONE", "telegram"); got == "installed" {
		t.Fatalf("post-remove search still marks telegram installed: %q", got)
	}

	// remove is idempotent: a second owner card approves the same original
	// helper and the absent grant/secret is a no-op, not an error.
	removeTg2, _ := run("LINKS_MUTATE_REMOVE_AGAIN", `"$XBOT_BIN" links remove --self telegram --json`)
	rmCard2 := cardID("LINKS_MUTATE_REMOVE_AGAIN", "remove telegram")
	if rmCard2 == rmCard {
		t.Fatal("second remove did not raise a fresh owner card")
	}
	decide(rmCard2, nil)
	out = jobDone(removeTg2)
	reply = cliLinksDecode(t, out)
	if reply.Error != "" || reply.Text != "removed telegram" {
		t.Fatalf("second links remove telegram lost its idempotent acknowledgement: %+v", reply)
	}

	// remove of an id that exists nowhere still passes through the owner card
	// first (the card precedes the domain lookup, matching the retired tool),
	// then keeps the original not-found reply on the typed wire.
	missingJob, _ := run("LINKS_MUTATE_REMOVE_MISSING", `"$XBOT_BIN" links remove --self no-such-link-anywhere --json`)
	missingCard := cardID("LINKS_MUTATE_REMOVE_MISSING", "remove no-such-link-anywhere")
	decide(missingCard, nil)
	notFoundOut, notFoundCode := jobEnd(missingJob)
	if notFoundCode == 0 {
		t.Fatalf("unknown remove id silently succeeded: %.900q", notFoundOut)
	}
	notFoundReply := cliLinksDecode(t, notFoundOut)
	if !strings.Contains(notFoundReply.Error, "links: not found") {
		t.Fatalf("unknown remove id lost its original error: %+v", notFoundReply)
	}
}

func permissionsOn(t *testing.T, srv *Server) bool {
	t.Helper()
	status, body := srv.do(http.MethodGet, "/api/permissions", nil)
	if status != http.StatusOK {
		t.Fatalf("GET /api/permissions: %d %s", status, body)
	}
	var reply struct {
		Confirm string `json:"confirm"`
	}
	if err := json.Unmarshal(body, &reply); err != nil {
		t.Fatalf("permissions reply: %v %s", err, body)
	}
	return reply.Confirm == "table"
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}
