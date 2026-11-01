//go:build smoke

package smoke

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestCLIExperienceRuntimeReadOnlyAndUnconfirmed(t *testing.T) {
	cliSkillIsolation(t)
	model := newCLIExperienceModel(t)
	srv := Start(t, &Stub{srv: model.server})
	const source = "ExperienceGateSource"
	srv.CreateBot(source, "Actual nonprimary source for experience readonly and unconfirmed-write acceptance.")
	primaryPath := filepath.Join(srv.Root, "primary.json")
	primaryRaw, err := os.ReadFile(primaryPath)
	if err != nil {
		t.Fatal(err)
	}
	var primary struct{ Name string }
	if err := json.Unmarshal(primaryRaw, &primary); err != nil || primary.Name == "" || primary.Name == source {
		t.Fatal("experience gate must use an actual API nonprimary source")
	}
	botDir := filepath.Join(srv.Root, "bots", source)
	dbPath := filepath.Join(botDir, "experience.sqlite")
	run := func(cue, command string) string {
		t.Helper()
		model.base.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "Exercise the original source experience CLI", "command": command}))
		cliSkillSend(t, srv, source, cue)
		results := model.base.results(cue)
		if len(results) != 1 || len(commandOutput(results[0])) > 32<<10 {
			t.Fatal("experience source did not return one bounded native Shell result")
		}
		return results[0]
	}
	var empty cliExperienceSearchReply
	cliBotDecode(t, run("EXP_EMPTY_A17", `"$XBOT_BIN" experience search --self --json`), &empty)
	if empty.Text != "no matching experiences" || empty.File != "" || empty.Error != "" {
		t.Fatal("missing-DB search did not preserve the original empty result")
	}
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatal("missing-DB search created an experience database")
	}
	workspace := filepath.Join(srv.Root, "work", source)
	if err := os.WriteFile(filepath.Join(workspace, "--help"), []byte("RESEARCH_FILE_MUST_NOT_RECORD"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "experience-native-read-proof.txt"), []byte("EXPERIENCE_NATIVE_FILE_TOOL_PRESERVED"), 0600); err != nil {
		t.Fatal(err)
	}
	type storedRow struct {
		ID                                 int64
		CreatedAt, Summary, Focus, Segment string
		From, To                           sql.NullInt64
	}
	readRows := func() []storedRow {
		t.Helper()
		db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		rows, err := db.Query(`SELECT id,created_at,summary,focus,from_seq,to_seq,segment FROM experiences ORDER BY id`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []storedRow
		for rows.Next() {
			var row storedRow
			if err := rows.Scan(&row.ID, &row.CreatedAt, &row.Summary, &row.Focus, &row.From, &row.To, &row.Segment); err != nil {
				t.Fatal(err)
			}
			out = append(out, row)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return out
	}
	checkRecord := func(result, summary string) cliExperienceRecordReply {
		t.Helper()
		var reply cliExperienceRecordReply
		cliBotDecode(t, result, &reply)
		if reply.Recorded == nil || !*reply.Recorded || reply.ID <= 0 || reply.Error != "" {
			t.Fatal("successful record omitted the required true confirmation and actual ID")
		}
		found := false
		for _, row := range readRows() {
			if row.ID == reply.ID {
				found = row.Summary == summary && row.From.Int64 == int64(reply.From) && row.To.Int64 == int64(reply.To)
			}
		}
		if !found {
			t.Fatal("actual SQLite row did not match the acknowledged summary/ID/range")
		}
		return reply
	}
	const mainCue, research, dev = "EXP_MAIN_B28", "EXP_RESEARCH_C39", "EXP_DEV_D40"
	const mainSummary, devSummary = "EXPERIENCE_MAIN_ALLOWED", "EXPERIENCE_DEV_ALLOWED"
	model.base.add(mainCue,
		mcpRuntimeTool("sh", map[string]string{"description": "Record one short experience through the real nonprimary source", "command": `"$XBOT_BIN" experience record --self --summary ` + mainSummary + ` --json`}),
		mcpRuntimeTool("tools", map[string]string{"action": "describe", "id": "detail"}),
	)
	cliSkillSend(t, srv, source, mainCue)
	mainResults := model.base.results(mainCue)
	mainReply := checkRecord(mainResults[0], mainSummary)
	if strings.HasPrefix(mainResults[1], "error:") || !strings.Contains(mainResults[1], "detail") {
		t.Fatal("the preserved Context Tools gate lost live detail")
	}
	fileSnapshot := func() map[string][]byte {
		t.Helper()
		files := map[string][]byte{}
		for _, path := range []string{primaryPath, filepath.Join(botDir, "config.json")} {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			files[path] = raw
		}
		root := filepath.Join(workspace, "results")
		if _, err := os.Stat(root); os.IsNotExist(err) {
			files[root] = nil
			return files
		} else if err != nil {
			t.Fatal(err)
		}
		files[root] = []byte("directory exists")
		if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			raw, err := os.ReadFile(path)
			if err == nil {
				files[path] = raw
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return files
	}
	// Compare logical SQLite rows, not WAL/schema bytes: original reads of an
	// existing DB may perform their documented SQLite initialization IO.
	beforeRows, beforeFiles := readRows(), fileSnapshot()
	injection := filepath.Join(workspace, "experience-operator-must-not-run")
	model.base.add(research,
		mcpRuntimeTool("sh", map[string]string{"description": "Read experience CLI help in research", "command": `"$XBOT_BIN" experience --help`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Search actual experiences through the research source", "command": `"$XBOT_BIN" experience search --self --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Reject ordinary record before the interpreter", "command": `"$XBOT_BIN" experience record --self --summary RESEARCH_MUST_NOT_RECORD --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Reject literal help summary as a write", "command": `"$XBOT_BIN" experience record --self --summary --help --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Reject literal help summary filename as a write", "command": `"$XBOT_BIN" experience record --self --summary-file --help --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Reject record pipelines before the interpreter", "command": `printf RESEARCH_PIPE_MUST_NOT_RECORD | "$XBOT_BIN" experience record --self --summary-file - --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Reject shell operators attached to experience help", "command": `"$XBOT_BIN" experience --help; printf injected > ` + shellQuote(injection)}),
	)
	const researchDispatch = "EXP_DISPATCH_RESEARCH_E51"
	model.base.add(researchDispatch, mcpRuntimeTool("dispatch", map[string]string{"type": "research", "task": research}))
	cliSkillSend(t, srv, source, researchDispatch)
	WaitFor(t, 12*time.Second, "actual experience research steps", func() (bool, string) {
		results := model.base.results(research)
		return results[len(results)-1] != "", "research steps not completed"
	})
	r := model.base.results(research)
	if !strings.HasPrefix(r[0], "exit 0\n") || strings.TrimSpace(commandOutput(r[0])) == "" {
		t.Fatal("research did not allow actual experience help")
	}
	var searched cliExperienceSearchReply
	cliBotDecode(t, r[1], &searched)
	if !strings.Contains(searched.Text, mainSummary) || searched.File != "" || searched.Error != "" {
		t.Fatal("research search did not preserve the original existing-DB text result")
	}
	for _, rejected := range r[2:] {
		line, _, _ := strings.Cut(rejected, "\n")
		if exitLine.MatchString(line) || strings.TrimSpace(rejected) == "" || len(rejected) > 32<<10 {
			t.Fatal("research record, literal-help write or operator reached the interpreter")
		}
	}
	if !reflect.DeepEqual(beforeRows, readRows()) || !reflect.DeepEqual(beforeFiles, fileSnapshot()) {
		t.Fatal("rejected research writes changed actual experience data/config/Primary/artifacts")
	}
	if _, err := os.Stat(injection); !os.IsNotExist(err) {
		t.Fatal("rejected pseudo-help executed its shell operator")
	}

	model.base.add(dev,
		mcpRuntimeTool("sh", map[string]string{"description": "Record from the actual dev source without a Call-ID prerequisite", "command": `env -u XBOT_CALL_ID "$XBOT_BIN" experience record --self --summary ` + devSummary + ` --json`}),
		mcpRuntimeTool("read_file", map[string]string{"path": "experience-native-read-proof.txt"}),
	)
	const devDispatch = "EXP_DISPATCH_DEV_F62"
	model.base.add(devDispatch, mcpRuntimeTool("dispatch", map[string]string{"type": "dev", "task": dev}))
	cliSkillSend(t, srv, source, devDispatch)
	WaitFor(t, 12*time.Second, "actual experience dev steps", func() (bool, string) {
		results := model.base.results(dev)
		return results[len(results)-1] != "", "dev steps not completed"
	})
	d := model.base.results(dev)
	devReply := checkRecord(d[0], devSummary)
	if devReply.ID == mainReply.ID || len(readRows()) != 2 || !strings.Contains(d[1], "EXPERIENCE_NATIVE_FILE_TOOL_PRESERVED") {
		t.Fatal("dev did not persist a second actual row or preserve native file reading")
	}

	// Fold the public experience environment field from actual provider wire.
	// No new environment source, receipt note or domain context is constructed.
	experienceValue := func(request stubRequest) json.RawMessage {
		var value json.RawMessage
		for _, message := range request.Messages {
			rest := message.Content
			for {
				_, after, found := strings.Cut(rest, "<environment_update>\n")
				if !found {
					break
				}
				body, tail, closed := strings.Cut(after, "\n</environment_update>")
				if !closed {
					break
				}
				rest = tail
				var update struct{ Set map[string]json.RawMessage }
				if json.Unmarshal([]byte(body), &update) == nil && update.Set["experience"] != nil {
					value = update.Set["experience"]
				}
			}
		}
		return value
	}
	if os.Geteuid() == 0 {
		t.Log("Not run: unreadable-database permission fault requires a non-root process")
	} else {
		const publishCue, faultCue = "EXP_PUBLISH_G73", "EXP_FAULT_H84"
		if !strings.HasPrefix(run(publishCue, `printf 'experience-value-published\n'`), "exit 0\n") {
			t.Fatal("actual source failed to publish the latest experience environment")
		}
		rowsBeforeFault := readRows()
		info, err := os.Stat(dbPath)
		if err != nil {
			t.Fatal(err)
		}
		oldMode := info.Mode().Perm()
		defer func() {
			if err := os.Chmod(dbPath, oldMode); err != nil {
				t.Errorf("restore actual DB mode: %v", err)
			}
		}()
		// Leave the real DB unreadable through the natural provider request after
		// the tool error, so experienceEnv must retain its already-published value.
		fault := run(faultCue, `chmod 000 `+shellQuote(dbPath)+` && "$XBOT_BIN" experience record --self --summary FS_UNCONFIRMED_ATTEMPT --from 1 --to 1 --json`)
		line, _, ok := strings.Cut(fault, "\n")
		if !ok || !exitLine.MatchString(line) || line == "exit 0" {
			t.Fatal("actual unreadable DB did not cause a nonzero record error")
		}
		var first json.RawMessage
		if err := json.NewDecoder(strings.NewReader(commandOutput(fault))).Decode(&first); err != nil {
			t.Fatal("record error did not preserve its first typed JSON receipt")
		}
		var fields map[string]json.RawMessage
		var failed cliExperienceRecordReply
		if json.Unmarshal(first, &fields) != nil || json.Unmarshal(first, &failed) != nil || !bytes.Equal(bytes.TrimSpace(fields["recorded"]), []byte("null")) || failed.Recorded != nil || failed.Error == "" || failed.From != 1 || failed.To != 1 {
			t.Fatal("record error omitted required null confirmation or lost its actual explicit range")
		}
		model.mu.Lock()
		captured := append([][]byte(nil), model.raw...)
		model.mu.Unlock()
		seenAfterFault := false
		for _, raw := range captured {
			var request stubRequest
			if json.Unmarshal(raw, &request) != nil || !strings.Contains(lastUser(request), faultCue) {
				continue
			}
			var wire struct {
				Messages []struct {
					Role       string
					ToolCallID string `json:"tool_call_id"`
				}
			}
			if json.Unmarshal(raw, &wire) != nil {
				t.Fatal("actual fault provider request was not JSON")
			}
			for _, message := range wire.Messages {
				if message.Role == "tool" && message.ToolCallID == faultCue+"_0" {
					seenAfterFault = true
					var value struct {
						Count  int
						Latest string
					}
					if json.Unmarshal(experienceValue(request), &value) != nil || value.Count != 2 || value.Latest != fmt.Sprintf("#%d %s", devReply.ID, devSummary) {
						t.Fatal("natural environment read error discarded the original published experience value")
					}
				}
			}
		}
		if !seenAfterFault {
			t.Fatal("record error never reached the natural next actual provider request")
		}
		if err := os.Chmod(dbPath, oldMode); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(rowsBeforeFault, readRows()) {
			t.Fatal("this concrete open-permission failure changed the previously observed SQLite rows")
		}
	}
	model.base.mu.Lock()
	mainSeen := model.base.scripts[mainCue].callers[callerForeground]
	researchSeen := model.base.scripts[research].callers[callerSubagent]
	devSeen := model.base.scripts[dev].callers[callerSubagent]
	model.base.mu.Unlock()
	if !mainSeen || !researchSeen || !devSeen {
		t.Fatal("retirement checks did not reach fresh actual main and continuable requests")
	}
	model.mu.Lock()
	retired := false
	for _, raw := range model.raw {
		var request stubRequest
		if json.Unmarshal(raw, &request) == nil {
			caller, _ := classify(request)
			if caller != callerCompaction && caller != callerExtract && (bytes.Contains(raw, []byte("experience_record")) || bytes.Contains(raw, []byte("experience_search"))) {
				retired = true
			}
		}
	}
	model.mu.Unlock()
	if retired {
		t.Fatal("fresh provider requests still expose a retired experience schema, index or instruction")
	}
}
