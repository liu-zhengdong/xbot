//go:build smoke

package smoke

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"xbot/internal/experience"
)

func TestCLIExperienceRuntimeOutputArtifactCurrent(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires unprivileged Unix permissions for the actual artifact write failure")
	}
	cliSkillIsolation(t)
	model := newCLIExperienceModel(t)
	srv := Start(t, &Stub{srv: model.server})
	const original, current = "ExperienceOutputSource", "ExperienceOutputMoved"
	const summary, focusPrefix, focusTail = "Recorded the explicit public output projection decision.", "EXPERIENCE_FOCUS_FULL_BEGIN_", "_RAW_FOCUS_TAIL_REMAINS_ONLY_IN_DATABASE"
	srv.CreateBot(original, "Record and search actual source experiences without clipping the rendered semantic result.")
	primaryRaw, err := os.ReadFile(filepath.Join(srv.Root, "primary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var primary struct{ Name string }
	if err := json.Unmarshal(primaryRaw, &primary); err != nil || primary.Name == "" || primary.Name == original {
		t.Fatal("experience output fixture must use an actual API-created nonprimary source")
	}
	run := func(source, cue, command string) string {
		t.Helper()
		// These cues are pairwise non-containing; base selects by Contains on
		// the actual latest owner input rather than a synthetic source context.
		model.base.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "Observe complete experience output through the original current source CLI", "command": command}))
		cliSkillSend(t, srv, source, cue)
		results := model.base.results(cue)
		if len(results) != 1 || results[0] == "" || len(commandOutput(results[0])) > 32<<10 {
			t.Fatal("actual source experience operation did not return one bounded native Shell result")
		}
		return results[0]
	}
	if result := run(original, "CLI_EXP_OUTPUT_WARM", `printf 'experience-output-source-ready\n'`); !strings.HasPrefix(result, "exit 0\n") {
		t.Fatal("actual source did not finish its original warm history/card/memory assembly")
	}
	focus := focusPrefix + strings.Repeat("\x01", 7000) + focusTail
	focusPath := filepath.Join(srv.Root, "work", original, "full-focus.txt")
	if err := os.WriteFile(focusPath, []byte(focus), 0600); err != nil {
		t.Fatal(err)
	}
	var recorded cliExperienceRecordReply
	cliBotDecode(t, run(original, "CLI_EXP_OUTPUT_RECORD", `env -u XBOT_CALL_ID "$XBOT_BIN" experience record --self --summary `+shellQuote(summary)+` --focus-file full-focus.txt --json`), &recorded)
	if recorded.ID <= 0 || recorded.Recorded == nil || !*recorded.Recorded || recorded.From <= 0 || recorded.To < recorded.From || recorded.Error != "" {
		t.Fatal("actual no-CallID record lost its actual ID/range/confirmed save")
	}
	entry, found, err := experience.At(filepath.Join(srv.Root, "bots", original)).Get(recorded.ID)
	if err != nil || !found || entry.Focus != focus || entry.Summary != summary || entry.From != recorded.From || entry.To != recorded.To || entry.CreatedAt.IsZero() {
		t.Fatal("actual recorded focus was independently capped or the physical entry lost its original fields")
	}
	var moved cliFullProfile
	cliBotDecode(t, run(original, "CLI_EXP_OUTPUT_RENAME", `"$XBOT_BIN" profile set --name `+current+` --timezone UTC --self --json`), &moved)
	if !moved.Applied || !moved.Moved || moved.Name != current || moved.PreviousName != original || moved.Timezone != "UTC" || moved.Scope != "runtime" || moved.Error != "" {
		t.Fatal("actual source profile setup did not establish its current name/workspace and display timezone")
	}
	// Original SearchExperiences prints this exact entry prefix before focus.
	// Because focus itself exceeds6KiB, original presentHistory clips inside it:
	// summary/segment/detail suffix and raw focus tail cannot be recovered by an
	// output artifact. Expected bytes are derived from the actual stored row.
	prefix := fmt.Sprintf("#%d %s seq %d-%d focus: ", entry.ID, entry.CreatedAt.In(time.UTC).Format("2006-01-02 15:04"), entry.From, entry.To)
	const truncation = "\n[truncated]"
	originalText := prefix + entry.Focus
	expected := originalText[:(6<<10)-len(truncation)] + truncation
	canonical, err := json.Marshal(map[string]string{"text": expected})
	if err != nil || len(expected) != 6<<10 || len(canonical)+1 <= 32<<10 || strings.Contains(expected, focusTail) {
		t.Fatal("rendered text fixture must retain original6KiB semantic clipping while its complete JSON exceeds32KiB")
	}
	strictJSON := func(raw []byte, dst any) {
		t.Helper()
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(dst); err != nil {
			t.Fatal("experience result is not a complete typed Reply JSON")
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			t.Fatal("experience Reply JSON has trailing or clipped output")
		}
	}
	currentWork := filepath.Join(srv.Root, "work", current)
	artifact := func(file, access string, advertised int64) {
		t.Helper()
		rel := filepath.Clean(filepath.FromSlash(file))
		if file == "" || filepath.IsAbs(rel) || filepath.Dir(rel) != "results" || access != "local" || advertised <= 32<<10 || advertised > 20<<20 {
			t.Fatal("large search lacks its complete bounded local results reference")
		}
		path := filepath.Join(currentWork, rel)
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() != advertised {
			t.Fatal("actual current-workspace search artifact lost complete advertised bytes or0600")
		}
		raw, err := os.ReadFile(path)
		if err != nil || int64(len(raw)) != advertised || bytes.Contains(raw, []byte(srv.Token)) {
			t.Fatal("actual search artifact byte facts or authentication redaction failed")
		}
		var full cliExperienceSearchReply
		strictJSON(raw, &full)
		if full.Text != expected || full.File != "" || full.Access != "" || full.Bytes != 0 || full.Error != "" {
			t.Fatal("artifact is not the full original rendered Text Reply JSON or replaced semantic clipping with raw records")
		}
	}
	search := `env -u XBOT_CALL_ID XBOT_BOT=` + original + ` "$XBOT_BIN" experience search --self`
	var reference cliExperienceSearchReply
	jsonResult := run(current, "CLI_EXP_OUTPUT_JSON", search+` --json`)
	if !strings.HasPrefix(jsonResult, "exit 0\n") {
		t.Fatal("actual current-source JSON search failed")
	}
	strictJSON([]byte(commandOutput(jsonResult)), &reference)
	if reference.Text != "" || reference.Error != "" {
		t.Fatal("large search JSON retained partial inline text or hid an output failure")
	}
	artifact(reference.File, reference.Access, reference.Bytes)
	humanResult := run(current, "CLI_EXP_OUTPUT_HUMAN", search)
	if !strings.HasPrefix(humanResult, "exit 0\n") {
		t.Fatal("actual current-source human search failed")
	}
	human := commandOutput(humanResult)
	var humanFile string
	for _, token := range strings.Fields(human) {
		token = strings.Trim(token, "`\"'(),")
		if strings.HasPrefix(token, "results/") {
			if humanFile != "" {
				t.Fatal("human search must identify one full semantic result artifact")
			}
			humanFile = token
		}
	}
	var humanBytes int64
	_, byteText, hasBytes := strings.Cut(human, "bytes: ")
	if humanFile == "" || !strings.Contains(human, "access: local") || !hasBytes {
		t.Fatal("human search omitted its usable full-result path/access/bytes reference")
	}
	if _, err := fmt.Sscanf(byteText, "%d", &humanBytes); err != nil {
		t.Fatal("human search byte reference is not usable")
	}
	artifact(humanFile, "local", humanBytes)
	resultsDir := filepath.Join(currentWork, "results")
	resultInfo, err := os.Stat(resultsDir)
	if err != nil || !resultInfo.IsDir() {
		t.Fatal("actual result writer did not establish its real results directory")
	}
	beforeFiles, err := os.ReadDir(resultsDir)
	if err != nil {
		t.Fatal(err)
	}
	restore := fmt.Sprintf("chmod %04o ", resultInfo.Mode().Perm()) + shellQuote(resultsDir)
	command := "trap " + shellQuote(restore) + " 0\nchmod 0500 " + shellQuote(resultsDir) + " || exit $?\n" +
		search + " --json\nexperience_status=$?\n" + restore + " || exit $?\ntrap - 0\nexit \"$experience_status\""
	failed := run(current, "CLI_EXP_OUTPUT_WRITE_FAILURE", command)
	exit, _, ok := strings.Cut(failed, "\n")
	if !ok || !exitLine.MatchString(exit) || exit == "exit 0" {
		t.Fatal("actual result-file permission failure did not return nonzero")
	}
	var refused cliExperienceSearchReply
	if err := json.NewDecoder(strings.NewReader(commandOutput(failed))).Decode(&refused); err != nil || refused.Error == "" || refused.File != "" || refused.Bytes != 0 || refused.Text != "" {
		t.Fatal("actual artifact write failure advertised a fake file or partial/full inline result")
	}
	afterInfo, err := os.Stat(resultsDir)
	if err != nil || afterInfo.Mode().Perm() != resultInfo.Mode().Perm() {
		t.Fatal("actual source Shell failed to restore the result-directory mode before returning")
	}
	afterFiles, err := os.ReadDir(resultsDir)
	if err != nil || len(afterFiles) != len(beforeFiles) {
		t.Fatal("failed actual result write left a phantom result artifact")
	}
	for _, old := range []string{filepath.Join(srv.Root, "bots", original), filepath.Join(srv.Root, "work", original)} {
		if _, err := os.Stat(old); !os.IsNotExist(err) {
			t.Fatal("experience artifact reads/writes revived or retained the old source path after rename")
		}
	}
}
