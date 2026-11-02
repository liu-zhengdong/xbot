//go:build smoke

package smoke

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
	"xbot/internal/bot"
)

func TestCLIExperienceRuntimeSourceConsumers(t *testing.T) {
	cliSkillIsolation(t)
	const source = "ExperienceChainSource"
	const summary = "Confirmed quasar deployment rollback and the public acceptance decision."
	const focus = "  quasar-deploy  "
	body := "EXPERIENCE_FULL_TOOL_BEGIN\n" + strings.Repeat("原始工具输出必须保留全文。", 280) + "\nEXPERIENCE_FULL_TOOL_END"
	model := newCLIExperienceModel(t)
	srv := Start(t, &Stub{srv: model.server})
	srv.CreateBot(source, "Record and recover the actual public deployment decision through the original source.")
	primaryRaw, err := os.ReadFile(filepath.Join(srv.Root, "primary.json"))
	var primary struct{ Name string }
	if err != nil || json.Unmarshal(primaryRaw, &primary) != nil || primary.Name == "" || primary.Name == source {
		t.Fatal("experience fixture must use an actual nonprimary source")
	}
	workspace := filepath.Join(srv.Root, "work", source)
	dbPath := filepath.Join(srv.Root, "bots", source, "experience.sqlite")
	const empty = "EXP_CHAIN_EMPTY_X00"
	model.base.add(empty, mcpRuntimeTool("sh", map[string]string{"description": "Read a missing experience database through the actual source", "command": `env -u XBOT_CALL_ID "$XBOT_BIN" experience search --self --json`}))
	cliSkillSend(t, srv, source, empty)
	var missing cliExperienceSearchReply
	cliBotDecode(t, model.base.results(empty)[0], &missing)
	if missing.Text != "no matching experiences" || missing.Error != "" || missing.File != "" {
		t.Fatal("missing source experience search changed original empty semantics")
	}
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatal("missing source search created a database")
	}
	// Original source Shell above establishes its lazy workspace. Bot creation
	// alone does not create this native job input directory.
	if err := os.WriteFile(filepath.Join(workspace, "experience-tool-body.txt"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	const first, latest = "EXP_CHAIN_PREPARED_A11", "EXP_CHAIN_CURRENT_B22"
	command := `printf '%s\n' "$XBOT_CALL_ID" > experience-original-call.txt` + "\n" + `printf 'ORIGINAL_SOURCE_JOB_READY\n' > experience-source-ready` + "\n" + `while [ ! -f experience-record-now ]; do sleep 0.025; done` + "\n" +
		`env -u XBOT_CALL_ID XBOT_BOT=` + primary.Name + ` "$XBOT_BIN" experience record --self --summary ` + shellQuote(summary) + ` --focus ` + shellQuote(focus) + ` --json > experience-record-reply.json` + "\n" + `experience_status=$?` + "\n" + `printf '%s\n' "$experience_status" > experience-record-exit` + "\n" + `cat experience-record-reply.json` + "\n" + `exit "$experience_status"`
	model.base.add(first, mcpRuntimeTool("sh", map[string]string{"description": "Keep original source Shell until a genuinely later owner message exists, then record without a CallID gate", "command": command}))
	original := srv.BeginSend(source, first)
	WaitFor(t, 10*time.Second, "actual original source Shell job readiness", func() (bool, string) {
		_, err := os.Stat(filepath.Join(workspace, "experience-source-ready"))
		return err == nil, "original Native Shell has not reached its physical ready marker"
	})
	// This is a second genuine authenticated owner ingress, not an invented
	// source context or appended fake identity row. Its raw tool result is real.
	model.base.add(latest, mcpRuntimeTool("sh", map[string]string{"description": "Produce the complete original tool output in the genuinely newer owner turn", "command": `cat experience-tool-body.txt`}))
	cliSkillSend(t, srv, source, latest)
	actual, err := bot.Load(srv.Root, source)
	if err != nil {
		t.Fatal(err)
	}
	firstSeq, latestSeq, bodySeq := 0, 0, 0
	if err := actual.Walk(1, 0, func(seq int, e bot.Entry) error {
		if e.Message.Role == "user" && !e.IsInternal() && !e.IsForkMark() {
			if strings.Contains(e.Message.Text(), first) {
				firstSeq = seq
			}
			if strings.Contains(e.Message.Text(), latest) {
				latestSeq = seq
			}
		}
		if e.Message.Role == "tool" {
			// Original transcript spills tool results past4KiB. Its stored preview
			// cannot identify a marker at the end of the actual complete blob.
			full, err := actual.HydrateLimit(e, 64<<10)
			if err != nil {
				return err
			}
			if strings.Contains(full.Message.Text(), "EXPERIENCE_FULL_TOOL_END") {
				bodySeq = seq
				if e.Blob == "" {
					t.Fatal("large real tool result did not exercise original transcript blob storage")
				}
			}
		}
		return nil
	}); err != nil || firstSeq <= 0 || latestSeq <= firstSeq || bodySeq <= latestSeq {
		t.Fatalf("actual later owner turn/full tool body was not distinct: first=%d latest=%d body=%d err=%v", firstSeq, latestSeq, bodySeq, err)
	}
	tailBefore, err := actual.Count()
	if err != nil {
		t.Fatal(err)
	}
	beforeRecord := time.Now()
	if err := os.WriteFile(filepath.Join(workspace, "experience-record-now"), []byte("RECORD_OPERATION_CURRENT_RANGE\n"), 0600); err != nil {
		t.Fatal(err)
	}
	WaitFor(t, 10*time.Second, "actual original source record receipt", func() (bool, string) {
		_, err := os.Stat(filepath.Join(workspace, "experience-record-exit"))
		return err == nil, "original source record CLI has not returned"
	})
	original.Wait()
	exit, err := os.ReadFile(filepath.Join(workspace, "experience-record-exit"))
	if err != nil || strings.TrimSpace(string(exit)) != "0" {
		t.Fatal("original source record without CallID did not succeed")
	}
	receiptRaw, err := os.ReadFile(filepath.Join(workspace, "experience-record-reply.json"))
	if err != nil || len(receiptRaw) > 32<<10 {
		t.Fatal("source record lost its bounded actual CLI receipt")
	}
	var reply cliExperienceRecordReply
	var fields map[string]json.RawMessage
	if json.Unmarshal(receiptRaw, &reply) != nil || json.Unmarshal(receiptRaw, &fields) != nil || fields["recorded"] == nil || reply.Recorded == nil || !*reply.Recorded || reply.ID <= 0 || reply.From != latestSeq || reply.From == firstSeq || reply.To != tailBefore || reply.Error != "" {
		t.Fatal("record did not confirm original operation-time current range independently of the prepared turn")
	}
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var id int64
	var created, storedSummary, storedFocus, segment string
	var from, to int
	if err := db.QueryRow(`SELECT id,created_at,summary,focus,from_seq,to_seq,segment FROM experiences WHERE id=?`, reply.ID).Scan(&id, &created, &storedSummary, &storedFocus, &from, &to, &segment); err != nil {
		t.Fatal(err)
	}
	at, err := time.Parse(time.RFC3339Nano, created)
	if err != nil || id != reply.ID || storedSummary != summary || storedFocus != strings.TrimSpace(focus) || from != reply.From || to != reply.To || at.Before(beforeRecord) || at.After(time.Now()) || !strings.HasPrefix(segment, "user: "+latest) || !strings.Contains(segment, "assistant: ") || !strings.Contains(segment, "[tools: sh]") || strings.Contains(segment, "EXPERIENCE_FULL_TOOL") {
		t.Fatal("actual committed SQLite columns lost original clock/range/trim/digest semantics")
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM experiences`).Scan(&count); err != nil || count != 1 {
		t.Fatal("one source record did not produce exactly one actual INSERT row")
	}
	status, panelRaw := cliFullAPI(t, srv, http.MethodGet, "/api/bots/"+source, nil)
	var panel struct {
		Memory struct {
			Experiences struct {
				Items []struct {
					ID                      int64
					CreatedAt               time.Time `json:"created_at"`
					Summary, Focus, Segment string
				}
			}
		}
	}
	if status != http.StatusOK || json.Unmarshal(panelRaw, &panel) != nil || len(panel.Memory.Experiences.Items) != 1 {
		t.Fatal("existing Web memory panel lost the recorded source experience")
	}
	web := panel.Memory.Experiences.Items[0]
	if web.ID != id || !web.CreatedAt.Equal(at) || web.Summary != storedSummary || web.Focus != storedFocus || web.Segment != segment {
		t.Fatal("original Web experience fields or app-compatible segment projection diverged from committed SQLite")
	}
	const searchCue = "EXP_CHAIN_ENV_SEARCH_C33"
	model.base.add(searchCue, mcpRuntimeTool("sh", map[string]string{"description": "List newest source experiences through original search semantics", "command": `"$XBOT_BIN" experience search --self --query '  ' --limit 0 --json`}), mcpRuntimeTool("sh", map[string]string{"description": "Retrieve the actual source decision through original recent-pool BM25", "command": `"$XBOT_BIN" experience search --self --query quasar --limit 8 --json`}))
	cliSkillSend(t, srv, source, searchCue)
	for _, result := range model.base.results(searchCue) {
		var found cliExperienceSearchReply
		cliBotDecode(t, result, &found)
		if found.Error != "" || found.File != "" || !strings.Contains(found.Text, "#"+strconv.FormatInt(id, 10)+" ") || !strings.Contains(found.Text, summary) || !strings.Contains(found.Text, segment) || strings.Contains(found.Text, "EXPERIENCE_FULL_TOOL") || !strings.Contains(found.Text, "transcript detail") {
			t.Fatal("source newest/BM25 search did not preserve original text digest without tool results")
		}
	}
	if len(model.base.results(searchCue)) != 2 {
		t.Fatal("actual source did not run both latest and BM25 searches")
	}
	model.mu.Lock()
	captured := append([][]byte(nil), model.raw...)
	model.mu.Unlock()
	envSeen := false
	for _, raw := range captured {
		var req stubRequest
		if json.Unmarshal(raw, &req) != nil || !strings.Contains(lastUser(req), searchCue) {
			continue
		}
		for _, message := range req.Messages {
			if strings.HasPrefix(message.Content, "<environment_") && strings.Contains(message.Content, `"experience"`) && strings.Contains(message.Content, `"count":1`) && strings.Contains(message.Content, "#"+strconv.FormatInt(id, 10)+" "+summary) {
				envSeen = true
			}
		}
	}
	if !envSeen {
		t.Fatal("next natural provider environment did not publish original experience count/latest ID summary")
	}
	const detailCue = "EXP_CHAIN_DETAIL_D44"
	model.base.add(detailCue,
		mcpRuntimeTool("sh", map[string]string{"description": "Read the original complete tool result through its raw transcript sequence", "command": `"$XBOT_BIN" transcript detail --self --seq ` + strconv.Itoa(bodySeq) + ` --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Read the same complete tool result through its recorded experience offset", "command": `"$XBOT_BIN" transcript detail --self --seq ` + strconv.Itoa(bodySeq-from) + ` --experience ` + strconv.FormatInt(id, 10) + ` --json`}),
	)
	cliSkillSend(t, srv, source, detailCue)
	details := model.base.results(detailCue)
	if len(details) != 2 {
		t.Fatal("preserved detail did not answer raw sequence and experience offset")
	}
	for _, result := range details {
		var reply cliTranscriptReadReply
		cliBotDecode(t, result, &reply)
		if reply.Error != "" || reply.File != "" || !strings.Contains(reply.Text, fmt.Sprintf("# seq %d tool", bodySeq)) || !strings.Contains(reply.Text, body) {
			t.Fatal("live detail did not recover complete original tool body through both stable transcript anchors")
		}
	}
}
