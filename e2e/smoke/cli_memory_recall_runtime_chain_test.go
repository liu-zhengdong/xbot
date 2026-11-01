//go:build smoke

package smoke

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"xbot/internal/bot"
)

func TestCLIMemoryRecallRuntimeSourceConsumers(t *testing.T) {
	cliSkillIsolation(t)
	model := newCLIMemoryRecallModel(t)
	srv := Start(t, &Stub{srv: model.server})
	const source = "MemoryRecallChainSource"
	srv.CreateBot(source, "Search only the original visible current source memory and read the actual matching files in full.")
	actual, err := bot.Load(srv.Root, source)
	if err != nil {
		t.Fatal(err)
	}
	cfg := actual.Config()
	if len(cfg.Owners) != 1 || cfg.Owners[0] == "" {
		t.Fatal("actual authenticated API source must have one existing owner")
	}
	owner := cfg.Owners[0]
	var primary struct{ Name string }
	raw, err := os.ReadFile(filepath.Join(srv.Root, "primary.json"))
	if err != nil || json.Unmarshal(raw, &primary) != nil || primary.Name == "" || primary.Name == source {
		t.Fatal("recall chain source must be actually nonprimary")
	}
	const otherPerson = "RecallOtherPerson"
	if otherPerson == owner {
		t.Fatal("other person fixture must differ from the actual current owner")
	}
	const equalLine = "violet launch code violet-orbit-73 visible in this particular scope"
	const botDetail = "BOT_FULL_DETAIL_BEGIN "
	const personDetail = "PERSON_FULL_DETAIL_BEGIN "
	botBody := "# Public deployment reference\n" + equalLine + "\n" + botDetail + strings.Repeat("the original complete free file stays readable; ", 30) + " BOT_FULL_DETAIL_END\n"
	personBody := "# Current owner reference\n" + equalLine + "\n" + personDetail + strings.Repeat("the original visible owner file stays readable; ", 30) + " PERSON_FULL_DETAIL_END\n"
	botDir := filepath.Join(srv.Root, "bots", source, "memory")
	personDir := filepath.Join(srv.Root, "people", owner, "memory")
	fixture := map[string]string{
		filepath.Join(botDir, "memory.md"):                                    "# memory\n## 索引\n（暂无）\n## 条目\n- (2026-10-18) violet BOT_DATED_CURRENT\n",
		filepath.Join(botDir, "projects", "deployment.txt"):                   botBody,
		filepath.Join(botDir, "violet-only-in-path.txt"):                      "This unrelated body has no requested keyword.\n",
		filepath.Join(personDir, "memory.md"):                                 "# memory\n## 索引\n（暂无）\n## 条目\n- (2026-10-18) violet PERSON_DATED_CURRENT\n",
		filepath.Join(personDir, "projects", "preferences.txt"):               personBody,
		filepath.Join(srv.Root, "people", otherPerson, "memory", "memory.md"): "# memory\n## 索引\n（暂无）\n## 条目\n- (2026-10-18) violet OTHER_PERSON_MUST_NOT_BE_VISIBLE\n",
	}
	for path, body := range fixture {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	const cue = "MR_CHAIN_SCOPES_A13"
	command := `test -n "$XBOT_CALL_ID" && printf '%s' violet | XBOT_BOT=` + primary.Name + ` "$XBOT_BIN" memory-recall --self --query-file - --json`
	model.base.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "Search the original prepared source with its real CallID, empty scope union and stdin query", "command": command}), mcpRuntimeTool("sh", map[string]string{"description": "Limit the original visible files to the current Bot scope", "command": `"$XBOT_BIN" memory-recall --self --query violet --scope bot --json`}), mcpRuntimeTool("sh", map[string]string{"description": "Read only the actual owner selected by the original prepared source", "command": `"$XBOT_BIN" memory-recall --self --query violet --scope person --json`}))
	cliSkillSend(t, srv, source, cue)
	results := model.base.results(cue)
	if len(results) != 3 {
		t.Fatal("actual source did not run all three original scope choices")
	}
	var union, botOnly, personOnly cliMemoryRecallReply
	cliBotDecode(t, results[0], &union)
	cliBotDecode(t, results[1], &botOnly)
	cliBotDecode(t, results[2], &personOnly)
	for _, reply := range []cliMemoryRecallReply{union, botOnly, personOnly} {
		if reply.Error != "" || reply.File != "" || reply.Text == "" || strings.Contains(reply.Text, "OTHER_PERSON_MUST_NOT_BE_VISIBLE") || strings.Contains(reply.Text, "violet-only-in-path.txt") {
			t.Fatal("source recall lost original semantic text, exposed another person, or matched a path rather than body")
		}
	}
	const botHit = "- bot:projects/deployment.txt:2: " + equalLine
	const personHit = "- person:projects/preferences.txt:2: " + equalLine
	if strings.Count(union.Text, "- bot:") != 2 || strings.Count(union.Text, "- person:") != 2 || !strings.Contains(union.Text, "- bot:memory.md:5: - (2026-10-18) violet BOT_DATED_CURRENT") || !strings.Contains(union.Text, "- person:memory.md:5: - (2026-10-18) violet PERSON_DATED_CURRENT") || !strings.Contains(union.Text, botHit) || !strings.Contains(union.Text, personHit) || strings.Index(union.Text, botHit) > strings.Index(union.Text, personHit) {
		t.Fatal("empty scope did not preserve Bot-first equal-score union, original dated/free path:line or actual current owner visibility")
	}
	if strings.Count(botOnly.Text, "- bot:") != 2 || strings.Contains(botOnly.Text, "- person:") || !strings.Contains(botOnly.Text, botHit) || strings.Count(personOnly.Text, "- person:") != 2 || strings.Contains(personOnly.Text, "- bot:") || !strings.Contains(personOnly.Text, personHit) {
		t.Fatal("explicit bot/person scopes changed the original visible directories")
	}
	// Consume paths from the real Text, rather than giving read_file an
	// unrelated fixture path and calling that a path:line consumer chain.
	hitPath := func(scope, suffix string) string {
		t.Helper()
		for _, line := range strings.Split(union.Text, "\n") {
			if rest, ok := strings.CutPrefix(line, "- "+scope+":"); ok {
				parts := strings.SplitN(rest, ":", 3)
				if len(parts) == 3 && parts[1] == "2" && strings.HasSuffix(parts[0], suffix) {
					return parts[0]
				}
			}
		}
		t.Fatal("original matching Text lacked its usable free-file relative path")
		return ""
	}
	botRelative := hitPath("bot", "deployment.txt")
	personRelative := hitPath("person", "preferences.txt")
	const readCue = "MR_CHAIN_FULL_FILES_B27"
	model.base.add(readCue, mcpRuntimeTool("read_file", map[string]string{"path": filepath.Join(botDir, filepath.FromSlash(botRelative))}), mcpRuntimeTool("read_file", map[string]string{"path": filepath.Join(personDir, filepath.FromSlash(personRelative))}))
	cliSkillSend(t, srv, source, readCue)
	readResults := model.base.results(readCue)
	if len(readResults) != 2 || !strings.Contains(readResults[0], equalLine) || !strings.Contains(readResults[0], botDetail+strings.Repeat("the original complete free file stays readable; ", 30)+" BOT_FULL_DETAIL_END") || !strings.Contains(readResults[1], equalLine) || !strings.Contains(readResults[1], personDetail+strings.Repeat("the original visible owner file stays readable; ", 30)+" PERSON_FULL_DETAIL_END") {
		t.Fatal("native read_file did not recover both complete actual matching files from their returned scope/path/line")
	}
	for path, body := range fixture {
		after, err := os.ReadFile(path)
		if err != nil || string(after) != body {
			t.Fatal("readonly source search or file consumer changed an original memory body")
		}
	}
}
