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
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"xbot/internal/bot"
	"xbot/internal/experience"
	"xbot/internal/provider"
)

func TestCLITranscriptRuntimeDetailCurrent(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires unprivileged Unix permissions for the actual result write failure")
	}
	cliSkillIsolation(t)
	model := newCLITranscriptModel(t)
	srv := Start(t, &Stub{srv: model.server})
	const original, current = "TranscriptDetailSource", "TranscriptDetailMoved"
	srv.CreateBot(original, "Read actual recorded detail through stable raw sequence and experience anchors.")
	primaryRaw, err := os.ReadFile(filepath.Join(srv.Root, "primary.json"))
	var primary struct{ Name string }
	if err != nil || json.Unmarshal(primaryRaw, &primary) != nil || primary.Name == "" || primary.Name == original {
		t.Fatal("detail fixture must use an actual API-created nonprimary source")
	}
	run := func(name, cue, command string) string {
		t.Helper()
		model.base.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "Consume the original current-source detail through its native CLI", "command": command}))
		cliSkillSend(t, srv, name, cue)
		results := model.base.results(cue)
		if len(results) != 1 || results[0] == "" || len(commandOutput(results[0])) > 32<<10 {
			t.Fatal("detail did not return one complete bounded native Shell result")
		}
		return results[0]
	}
	if result := run(original, "CTX_DETAIL_WARM_A17", `printf 'detail-source-workspace-ready\n'`); !strings.HasPrefix(result, "exit 0\n") {
		t.Fatal("actual source warm history did not establish its workspace before inputs")
	}
	const nativeHead, nativeTail = "DETAIL_REAL_NATIVE_BEGIN\n", "\nDETAIL_REAL_NATIVE_END\n"
	padding := (10 << 10) - len(nativeHead) - len(nativeTail)
	payload := nativeHead + strings.Repeat("汉", padding/3) + strings.Repeat("x", padding%3) + nativeTail
	if len(payload) != 10<<10 || !utf8.ValidString(payload) {
		t.Fatal("actual native detail payload must contain10KiB of complete UTF-8")
	}
	workspace := filepath.Join(srv.Root, "work", original)
	for name, body := range map[string]string{"detail-native-payload.txt": payload, "detail-native-count.txt": "0\n"} {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	const produce = "CTX_DETAIL_NATIVE_RECORD_B29"
	model.base.add(produce,
		mcpRuntimeTool("sh", map[string]string{"description": "Produce the real complete native tool body once", "command": "detail_count=$(cat detail-native-count.txt)\nprintf '%s\\n' \"$((detail_count + 1))\" > detail-native-count.txt\ncat detail-native-payload.txt"}),
		mcpRuntimeTool("sh", map[string]string{"description": "Anchor this same real owner turn including its original tool result", "command": `env -u XBOT_CALL_ID "$XBOT_BIN" experience record --self --summary 'Recorded the original complete native detail evidence.' --json`}),
	)
	cliSkillSend(t, srv, original, produce)
	produced := model.base.results(produce)
	if len(produced) != 2 || !strings.HasPrefix(produced[0], "exit 0\n") || produced[1] == "" {
		t.Fatal("actual native body and following same-owner experience record did not complete")
	}
	var recorded cliExperienceRecordReply
	cliBotDecode(t, produced[1], &recorded)
	if recorded.ID <= 0 || recorded.Recorded == nil || !*recorded.Recorded || recorded.Error != "" {
		t.Fatal("actual source experience record lost its confirmed anchor")
	}
	actual, err := bot.Load(srv.Root, original)
	if err != nil {
		t.Fatal(err)
	}
	ownerSeq, nativeSeq := 0, 0
	var native bot.Entry
	if err := actual.Walk(1, 0, func(seq int, entry bot.Entry) error {
		if entry.Message.Role == provider.RoleUser && !entry.IsInternal() && !entry.IsForkMark() && strings.Contains(entry.Message.Text(), produce) {
			ownerSeq = seq
		}
		if entry.Message.Role == provider.RoleTool && entry.Message.CallID() == produce+"_0" {
			full, err := actual.HydrateLimit(entry, 64<<10)
			if err != nil {
				return err
			}
			if nativeSeq != 0 || entry.Blob == "" || !strings.Contains(full.Message.Text(), strings.TrimSuffix(payload, "\n")) {
				t.Fatal("real native result was duplicated, not spilled, or lost its complete10KiB body")
			}
			nativeSeq, native = seq, full
		}
		return nil
	}); err != nil || ownerSeq <= 0 || nativeSeq <= ownerSeq {
		t.Fatal("actual transcript did not retain distinct owner and original paired tool-result sequences")
	}
	stored, found, err := experience.At(filepath.Join(srv.Root, "bots", original)).Get(recorded.ID)
	if err != nil || !found || stored.ID != recorded.ID || stored.From != recorded.From || stored.To != recorded.To || stored.From != ownerSeq || stored.From > nativeSeq || stored.To < nativeSeq {
		t.Fatal("actual default experience range does not cover the original owner's native tool body")
	}
	var moved cliFullProfile
	cliBotDecode(t, run(original, "CTX_DETAIL_RENAME_C31", `"$XBOT_BIN" profile set --name `+current+` --timezone UTC --self --json`), &moved)
	if !moved.Applied || !moved.Moved || moved.Name != current || moved.PreviousName != original || moved.Timezone != "UTC" || moved.Scope != "runtime" || moved.Error != "" {
		t.Fatal("actual profile rename did not establish the current source and detail display timezone")
	}
	workspace = filepath.Join(srv.Root, "work", current)
	expectedNative := strings.TrimRight(fmt.Sprintf("# seq %d tool %s\n%s", nativeSeq, native.Time.In(time.UTC).Format("2006-01-02 15:04"), native.Message.Text()), "\n")
	for _, anchor := range []struct{ cue, args string }{
		{"CTX_DETAIL_RAW_D43", " --seq " + strconv.Itoa(nativeSeq)},
		{"CTX_DETAIL_EXPERIENCE_E57", " --seq " + strconv.Itoa(nativeSeq-stored.From) + " --experience " + strconv.FormatInt(stored.ID, 10)},
	} {
		var reply cliTranscriptReadReply
		cliBotDecode(t, run(current, anchor.cue, `env -u XBOT_CALL_ID XBOT_BOT=`+original+` "$XBOT_BIN" transcript detail --self`+anchor.args+` --json`), &reply)
		if reply.Text != expectedNative || reply.Error != "" || reply.File != "" || !strings.Contains(reply.Text, strings.TrimSuffix(payload, "\n")) {
			t.Fatal("raw sequence or experience offset failed to hydrate the complete original native body after rename")
		}
	}
	// This larger entry is public persisted transcript DATA, not a claimed
	// native tool output: native Shell output has its own32KiB limit. No user
	// identity, source context or synthetic tool-call/result pair is fabricated.
	actual, err = bot.Load(srv.Root, current)
	if err != nil {
		t.Fatal(err)
	}
	const largeHead, largeTail = "DETAIL_PERSISTED_ASSISTANT_BEGIN\n", "\nDETAIL_RAW_80K_TAIL_MUST_NOT_BE_EXPORTED"
	largePadding := (80 << 10) - len(largeHead) - len(largeTail)
	largeBody := largeHead + strings.Repeat("<&\x01", largePadding/3) + strings.Repeat("y", largePadding%3) + largeTail
	at := time.Now().UTC()
	largeSeq, err := actual.AppendSeq(bot.Entry{Time: at, Message: provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent(largeBody)}})
	if err != nil || largeSeq <= nativeSeq || len(largeBody) != 80<<10 {
		t.Fatal("public transcript fixture did not persist its original80KiB assistant body")
	}
	// Detail first limits hydration, then includes its header before the WHOLE
	//64KiB presentHistory cap. This ASCII/control fixture has no split-rune edge.
	semantic := fmt.Sprintf("# seq %d assistant %s\n%s", largeSeq, at.In(time.UTC).Format("2006-01-02 15:04"), largeBody)
	const truncation = "\n[truncated]"
	expected := semantic[:(64<<10)-len(truncation)] + truncation
	canonical, err := json.Marshal(map[string]string{"text": expected})
	if err != nil || len(expected) != 64<<10 || !utf8.ValidString(expected) || len(canonical)+1 <= 32<<10 || len(canonical)+1 > 20<<20 || strings.Contains(expected, largeTail) {
		t.Fatal("large detail fixture did not distinguish complete64KiB semantic projection from uncapped raw data")
	}
	strictJSON := func(raw []byte, dst any) {
		t.Helper()
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(dst); err != nil {
			t.Fatal("detail result is not one complete typed Reply JSON")
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			t.Fatal("detail result JSON has trailing or clipped records")
		}
	}
	artifact := func(file, access string, advertised int64) {
		t.Helper()
		rel := filepath.Clean(filepath.FromSlash(file))
		if file == "" || filepath.IsAbs(rel) || filepath.Dir(rel) != "results" || access != "local" || advertised <= 32<<10 || advertised > 20<<20 {
			t.Fatal("large detail omitted its complete bounded local artifact reference")
		}
		path := filepath.Join(workspace, rel)
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() != advertised {
			t.Fatal("actual current-workspace detail artifact lost0600 or advertised complete bytes")
		}
		raw, err := os.ReadFile(path)
		if err != nil || int64(len(raw)) != advertised || bytes.Contains(raw, []byte(srv.Token)) {
			t.Fatal("detail artifact byte facts or authentication redaction failed")
		}
		var full cliTranscriptReadReply
		strictJSON(raw, &full)
		if full.Text != expected || full.Error != "" || full.File != "" || full.Access != "" || full.Bytes != 0 {
			t.Fatal("artifact changed original whole64KiB detail semantics or exported the uncapped raw body")
		}
	}
	detail := `env -u XBOT_CALL_ID XBOT_BOT=` + original + ` "$XBOT_BIN" transcript detail --self --seq ` + strconv.Itoa(largeSeq)
	jsonResult := run(current, "CTX_DETAIL_LARGE_JSON_F61", detail+` --json`)
	if !strings.HasPrefix(jsonResult, "exit 0\n") {
		t.Fatal("actual current-source large JSON detail failed")
	}
	var reference cliTranscriptReadReply
	strictJSON([]byte(commandOutput(jsonResult)), &reference)
	if reference.Text != "" || reference.Error != "" {
		t.Fatal("large detail returned partial inline text instead of a complete semantic artifact")
	}
	artifact(reference.File, reference.Access, reference.Bytes)
	humanResult := run(current, "CTX_DETAIL_LARGE_HUMAN_G73", detail)
	if !strings.HasPrefix(humanResult, "exit 0\n") {
		t.Fatal("actual current-source large human detail failed")
	}
	human := commandOutput(humanResult)
	var humanFile string
	for _, token := range strings.Fields(human) {
		token = strings.Trim(token, "`\"'(),")
		if strings.HasPrefix(token, "results/") {
			if humanFile != "" {
				t.Fatal("human detail must identify one complete artifact")
			}
			humanFile = token
		}
	}
	var humanBytes int64
	_, byteText, hasBytes := strings.Cut(human, "bytes: ")
	if humanFile == "" || !strings.Contains(human, "access: local") || !hasBytes {
		t.Fatal("human detail lacks usable full-result path/access/bytes metadata")
	}
	if _, err := fmt.Sscanf(byteText, "%d", &humanBytes); err != nil {
		t.Fatal("human detail artifact byte facts are not usable")
	}
	artifact(humanFile, "local", humanBytes)
	resultsDir := filepath.Join(workspace, "results")
	resultInfo, err := os.Stat(resultsDir)
	if err != nil || !resultInfo.IsDir() {
		t.Fatal("actual detail writer did not establish its result directory")
	}
	beforeFiles, err := os.ReadDir(resultsDir)
	if err != nil {
		t.Fatal(err)
	}
	restore := fmt.Sprintf("chmod %04o ", resultInfo.Mode().Perm()) + shellQuote(resultsDir)
	command := "trap " + shellQuote(restore) + " 0\nchmod 0500 " + shellQuote(resultsDir) + " || exit $?\n" + detail + " --json\ndetail_status=$?\n" + restore + " || exit $?\ntrap - 0\nexit \"$detail_status\""
	failed := run(current, "CTX_DETAIL_WRITE_FAILURE_H89", command)
	exit, _, ok := strings.Cut(failed, "\n")
	if !ok || !exitLine.MatchString(exit) || exit == "exit 0" {
		t.Fatal("actual detail artifact permission failure did not return nonzero")
	}
	var refused cliTranscriptReadReply
	if err := json.NewDecoder(strings.NewReader(commandOutput(failed))).Decode(&refused); err != nil || refused.Error == "" || refused.File != "" || refused.Bytes != 0 || refused.Text != "" {
		t.Fatal("detail write failure advertised a fake file or partial/full inline projection")
	}
	afterInfo, err := os.Stat(resultsDir)
	if err != nil || afterInfo.Mode().Perm() != resultInfo.Mode().Perm() {
		t.Fatal("actual source Shell did not restore exact result-directory mode before returning")
	}
	afterFiles, err := os.ReadDir(resultsDir)
	if err != nil || len(afterFiles) != len(beforeFiles) {
		t.Fatal("failed detail write left a phantom artifact")
	}
	count, err := os.ReadFile(filepath.Join(workspace, "detail-native-count.txt"))
	if err != nil || strings.TrimSpace(string(count)) != "1" {
		t.Fatal("detail consumers re-executed the original native body producer")
	}
	for _, old := range []string{filepath.Join(srv.Root, "bots", original), filepath.Join(srv.Root, "work", original)} {
		if _, err := os.Stat(old); !os.IsNotExist(err) {
			t.Fatal("detail/experience/blob/artifact reads revived or retained old source bot/work paths")
		}
	}
}
