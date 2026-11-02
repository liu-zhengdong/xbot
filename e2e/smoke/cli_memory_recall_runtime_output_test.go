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
	"unicode/utf8"
)

func TestCLIMemoryRecallRuntimeOutputCurrent(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires unprivileged Unix permissions for the actual result write failure")
	}
	cliSkillIsolation(t)
	model := newCLIMemoryRecallModel(t)
	srv := Start(t, &Stub{srv: model.server})
	const original, current = "RecallOutputSource", "RecallOutputMoved"
	const query = "quasarrecallneedle"
	srv.CreateBot(original, "Find the complete visible-memory projection through this actual current source.")
	primaryRaw, err := os.ReadFile(filepath.Join(srv.Root, "primary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var primary struct{ Name string }
	if err := json.Unmarshal(primaryRaw, &primary); err != nil || primary.Name == "" || primary.Name == original {
		t.Fatal("recall output fixture must use a real API-created nonprimary source")
	}
	run := func(name, cue, command string) string {
		t.Helper()
		model.base.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "Read the complete recall projection through the actual original live source Shell Call-ID", "command": command}))
		cliSkillSend(t, srv, name, cue)
		results := model.base.results(cue)
		if len(results) != 1 || results[0] == "" || len(commandOutput(results[0])) > 32<<10 {
			t.Fatal("recall did not produce one complete bounded actual source Shell result")
		}
		return results[0]
	}
	if result := run(original, "CLI_RECALL_OUTPUT_WARM", `printf 'recall-current-output-source-ready\n'`); !strings.HasPrefix(result, "exit 0\n") {
		t.Fatal("real source warm history did not establish its native workspace before fixture inputs")
	}
	canonicalRoot, err := filepath.EvalSymlinks(srv.Root)
	if err != nil {
		t.Fatal(err)
	}
	// Each real pathname component remains below255 bytes, while escaped JSON
	// expands its control/HTML characters. The query occurs only in file bodies.
	component := strings.Repeat("<&\x01", 75) + "路"
	rel := filepath.Join("log", component, component, component, "ranked-notes.md")
	memoryDir := filepath.Join(canonicalRoot, "bots", original, "memory")
	fixturePath := filepath.Join(memoryDir, rel)
	if len(fixturePath) >= 1024 {
		t.Fatal("private fixture exceeds the actual Darwin pathname budget before business execution")
	}
	for _, part := range strings.Split(fixturePath, string(filepath.Separator)) {
		if len(part) > 255 {
			t.Fatal("private fixture exceeds the filesystem component limit before business execution")
		}
	}
	if err := os.MkdirAll(filepath.Dir(fixturePath), 0700); err != nil {
		t.Fatal(err)
	}
	var fixture strings.Builder
	fixture.WriteString("# Canonical dated recall output fixture\n")
	var expectedLines []string
	for i := 0; i < 21; i++ {
		line := fmt.Sprintf("- 2026-11-01 %s %s%s <&> BODY_OUTSIDE_200_RUNES_%02d", query, strings.Repeat("汉", 180), strings.Repeat("\x01", 20), i)
		if utf8.RuneCountInString(line) <= 200 {
			t.Fatal("body fixture must exercise original200-rune truncation")
		}
		fixture.WriteString(line)
		fixture.WriteByte('\n')
		if i < 20 {
			body := string([]rune(line)[:200]) + "…"
			if len(body) <= 200 || !utf8.ValidString(body) {
				t.Fatal("expected hit must distinguish200 Unicode runes from a200-byte clip")
			}
			// All21 lines have identical query frequency and token lengths, so
			// original BM25 ties preserve their file-line order. Header is line1.
			expectedLines = append(expectedLines, fmt.Sprintf("- bot:%s:%d: %s", filepath.ToSlash(rel), i+2, body))
		}
	}
	if err := os.WriteFile(fixturePath, []byte(fixture.String()), 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(fixturePath)
	if err != nil || !info.Mode().IsRegular() || info.Size() != int64(fixture.Len()) {
		t.Fatal("actual deep UTF-8/control pathname fixture did not exist completely before recall")
	}
	expected := strings.Join(expectedLines, "\n") + "\n(1 more; narrow the query or use since=)"
	canonical, err := json.Marshal(map[string]string{"text": expected})
	if err != nil || len(canonical)+1 <= 32<<10 || len(canonical)+1 > 20<<20 || !utf8.ValidString(expected) {
		t.Fatal("full original20-hit text fixture must force a bounded complete JSON artifact")
	}
	var moved cliFullProfile
	cliBotDecode(t, run(original, "CLI_RECALL_OUTPUT_RENAME", `"$XBOT_BIN" profile set --name `+current+` --self --json`), &moved)
	if !moved.Applied || !moved.Moved || moved.Name != current || moved.PreviousName != original || moved.Scope != "runtime" || moved.Error != "" {
		t.Fatal("actual profile rename did not establish the current source/workspace before recall")
	}
	currentWork := filepath.Join(srv.Root, "work", current)
	currentFixture := filepath.Join(canonicalRoot, "bots", current, "memory", rel)
	if info, err := os.Stat(currentFixture); err != nil || !info.Mode().IsRegular() {
		t.Fatal("actual source rename lost the complete deep memory fixture")
	}
	strictJSON := func(raw []byte, dst any) {
		t.Helper()
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(dst); err != nil {
			t.Fatal("recall result is not one complete typed Reply JSON")
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			t.Fatal("recall result JSON was clipped or contains trailing records")
		}
	}
	artifact := func(file, access string, advertised int64) {
		t.Helper()
		resultRel := filepath.Clean(filepath.FromSlash(file))
		if file == "" || filepath.IsAbs(resultRel) || filepath.Dir(resultRel) != "results" || access != "local" || advertised <= 32<<10 || advertised > 20<<20 {
			t.Fatal("recall omitted its complete bounded local result-file reference")
		}
		path := filepath.Join(currentWork, resultRel)
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() != advertised {
			t.Fatal("current source artifact path lost actual complete bytes or0600")
		}
		raw, err := os.ReadFile(path)
		if err != nil || int64(len(raw)) != advertised || bytes.Contains(raw, []byte(srv.Token)) {
			t.Fatal("recall artifact byte facts or authentication redaction failed")
		}
		var full cliMemoryRecallReply
		strictJSON(raw, &full)
		if full.Text != expected || full.File != "" || full.Access != "" || full.Bytes != 0 || full.Error != "" {
			t.Fatal("artifact lost original full20-hit projection/200-rune bodies/remaining-count or falsely marked this small scan incomplete")
		}
	}
	// Keep each actual Shell-generated live Call-ID. XBOT_BOT remains diagnostic:
	// a stale name must not choose the filesystem or manufacture another kernel.
	search := `XBOT_BOT=` + original + ` "$XBOT_BIN" memory-recall --self --scope bot --since 2026-11-01 --query ` + query
	jsonResult := run(current, "CLI_RECALL_OUTPUT_JSON", search+` --json`)
	if !strings.HasPrefix(jsonResult, "exit 0\n") {
		t.Fatal("actual current-source JSON recall failed")
	}
	var reference cliMemoryRecallReply
	strictJSON([]byte(commandOutput(jsonResult)), &reference)
	if reference.Text != "" || reference.Error != "" {
		t.Fatal("large recall JSON carried clipped inline hits instead of a full artifact reference")
	}
	artifact(reference.File, reference.Access, reference.Bytes)
	humanResult := run(current, "CLI_RECALL_OUTPUT_HUMAN", search)
	if !strings.HasPrefix(humanResult, "exit 0\n") {
		t.Fatal("actual current-source human recall failed")
	}
	human := commandOutput(humanResult)
	var humanFile string
	for _, token := range strings.Fields(human) {
		token = strings.Trim(token, "`\"'(),")
		if strings.HasPrefix(token, "results/") {
			if humanFile != "" {
				t.Fatal("human recall must identify one complete projection artifact")
			}
			humanFile = token
		}
	}
	var humanBytes int64
	_, byteText, hasBytes := strings.Cut(human, "bytes: ")
	if humanFile == "" || !strings.Contains(human, "access: local") || !hasBytes {
		t.Fatal("human recall lacks usable full-result path/access/bytes metadata")
	}
	if _, err := fmt.Sscanf(byteText, "%d", &humanBytes); err != nil {
		t.Fatal("human recall did not print usable artifact byte facts")
	}
	artifact(humanFile, "local", humanBytes)
	resultsDir := filepath.Join(currentWork, "results")
	resultInfo, err := os.Stat(resultsDir)
	if err != nil || !resultInfo.IsDir() {
		t.Fatal("actual recall writer did not establish its real results directory")
	}
	beforeFiles, err := os.ReadDir(resultsDir)
	if err != nil {
		t.Fatal(err)
	}
	restore := fmt.Sprintf("chmod %04o ", resultInfo.Mode().Perm()) + shellQuote(resultsDir)
	command := "trap " + shellQuote(restore) + " 0\nchmod 0500 " + shellQuote(resultsDir) + " || exit $?\n" +
		search + " --json\nrecall_status=$?\n" + restore + " || exit $?\ntrap - 0\nexit \"$recall_status\""
	failed := run(current, "CLI_RECALL_OUTPUT_WRITE_FAILURE", command)
	exit, _, ok := strings.Cut(failed, "\n")
	if !ok || !exitLine.MatchString(exit) || exit == "exit 0" {
		t.Fatal("real recall artifact permission failure did not return nonzero")
	}
	var refused cliMemoryRecallReply
	if err := json.NewDecoder(strings.NewReader(commandOutput(failed))).Decode(&refused); err != nil || refused.Error == "" || refused.File != "" || refused.Bytes != 0 || refused.Text != "" {
		t.Fatal("actual recall artifact write failure claimed a fake file or partial/full inline hit projection")
	}
	afterInfo, err := os.Stat(resultsDir)
	if err != nil || afterInfo.Mode().Perm() != resultInfo.Mode().Perm() {
		t.Fatal("native Shell failed to restore exact artifact-directory permissions before returning")
	}
	afterFiles, err := os.ReadDir(resultsDir)
	if err != nil || len(afterFiles) != len(beforeFiles) {
		t.Fatal("actual failed artifact write left a phantom result file")
	}
	for _, old := range []string{filepath.Join(srv.Root, "bots", original), filepath.Join(srv.Root, "work", original)} {
		if _, err := os.Stat(old); !os.IsNotExist(err) {
			t.Fatal("recall or artifact consumers revived or retained old source bot/work paths")
		}
	}
}
