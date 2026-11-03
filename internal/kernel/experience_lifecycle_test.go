package kernel

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/clip"
	"xbot/internal/config"
	"xbot/internal/experience"
	"xbot/internal/inbox"
	"xbot/internal/provider"
	"xbot/internal/tools"
	"xbot/internal/work"
)

func TestExperienceRuntimeNaturalEnvironmentSQLiteAndNoExtraEvents(t *testing.T) {
	k, endpoint := experienceRuntimeFixture(t, true)
	experienceRuntimeAppend(t, k.Bot,
		bot.Entry{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("project question")}},
		bot.Entry{Message: toolCall("shell-original", "sh", `{"command":"inspect project"}`)},
		bot.Entry{Message: provider.Message{Role: provider.RoleTool, Content: provider.ToolResult("shell-original", "do not copy tool body")}},
		bot.Entry{Message: provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("project answer")}},
	)
	before, err := k.currentCount()
	if err != nil {
		t.Fatal(err)
	}
	wakes := 0
	SetWaker(k.Bot.Root, func(*bot.Bot) { wakes++ })
	defer SetWaker(k.Bot.Root, nil)
	summary := "decision " + strings.Repeat("s", 300)
	var recorded cliops.ExperienceRecordReply
	status, _ := experienceRuntimeHTTP(t, endpoint, k.Bot.Root, "record", "", cliops.ExperienceRecord{Summary: summary, Focus: "project"}, &recorded)
	if status != http.StatusOK || recorded.Recorded == nil || !*recorded.Recorded {
		t.Fatalf("record %+v status=%d", recorded, status)
	}
	db, err := sql.Open("sqlite", filepath.Join(bot.Dir(k.Bot.Root, k.Bot.Name()), experience.FileName))
	if err != nil {
		t.Fatal(err)
	}
	var id int64
	var created, gotSummary, focus, segment string
	var from, to int
	err = db.QueryRow(`SELECT id,created_at,summary,focus,from_seq,to_seq,segment FROM experiences`).Scan(&id, &created, &gotSummary, &focus, &from, &to, &segment)
	closeErr := db.Close()
	if err != nil || closeErr != nil || id != recorded.ID || created != k.now().Format(time.RFC3339Nano) || gotSummary != summary || focus != "project" || from != 1 || to != before || !strings.Contains(segment, "[tools: sh]") || strings.Contains(segment, "do not copy tool body") {
		t.Fatalf("real original SQLite columns id=%d time=%q summary=%q focus=%q range=%d/%d segment=%q err=%v/%v", id, created, gotSummary, focus, from, to, segment, err, closeErr)
	}
	// The existing Web consumer's Store.Page still receives every original
	// field and original segment grammar; no Web import cycle or new DTO copy.
	page, err := experience.At(bot.Dir(k.Bot.Root, k.Bot.Name())).Page(8, 0, "PROJECT")
	if err != nil || len(page) != 1 || page[0].ID != id || page[0].Summary != summary || page[0].Focus != focus || page[0].Segment != segment || page[0].From != from || page[0].To != to || !page[0].CreatedAt.Equal(k.now()) {
		t.Fatalf("existing Web Store.Page fields %+v %v", page, err)
	}
	var search cliops.ExperienceSearchReply
	status, _ = experienceRuntimeHTTP(t, endpoint, k.Bot.Root, "search", "", cliops.ExperienceSearch{}, &search)
	after, err := k.currentCount()
	if status != http.StatusOK || err != nil || after != before || wakes != 0 {
		t.Fatalf("runtime added transcript/events: status=%d seq=%d/%d wakes=%d err=%v", status, before, after, wakes, err)
	}
	if pending := k.Resources.box.Pending(); len(pending) != 0 {
		t.Fatalf("record/search created Inbox facts %+v", pending)
	}
	model := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, Content: provider.TextContent("noted")},
		{Role: provider.RoleAssistant, Content: provider.TextContent("still here")},
	}}
	k.Provider = model
	if err := k.Handle(context.Background(), inbox.TerminalEvent("owner", "next natural turn", k.now())); err != nil {
		t.Fatal(err)
	}
	if len(model.reqs) != 1 {
		t.Fatalf("natural next turn made %d model calls", len(model.reqs))
	}
	request := messagesText(model.reqs[0].Messages)
	short, _ := clip.Bytes(summary, experienceEnvSummary)
	published := false
	for _, message := range model.reqs[0].Messages {
		frame, ok := decodeEnvironment(message.Text())
		if !ok {
			continue
		}
		value, ok := frame.Set["experience"]
		if !ok {
			continue
		}
		var got struct {
			Count  int    `json:"count"`
			Latest string `json:"latest"`
		}
		if err := json.Unmarshal(value, &got); err != nil {
			t.Fatalf("decode natural experience environment: %v", err)
		}
		if got.Count != 1 || got.Latest != "#1 "+short {
			t.Fatalf("natural experience environment count=%d latest=%q want=%q", got.Count, got.Latest, "#1 "+short)
		}
		published = true
	}
	if !published || strings.Contains(request, summary) {
		t.Fatalf("natural environment summary/count: %s", request)
	}
	previous, err := k.sampleEnvironment(environment{})
	if err != nil {
		t.Fatal(err)
	}
	// The existing reader's error keeps the previously published experience
	// value. It must not create a reset, synthetic receipt or second event.
	path := filepath.Join(bot.Dir(k.Bot.Root, k.Bot.Name()), experience.FileName)
	if err := os.Rename(path, path+".saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	next, err := k.sampleEnvironment(previous)
	if err != nil || !bytes.Equal(previous.Fields["experience"], next.Fields["experience"]) {
		t.Fatalf("DB read failure reset original environment prev=%s next=%s err=%v", previous.Fields["experience"], next.Fields["experience"], err)
	}
	if err := k.Handle(context.Background(), inbox.TerminalEvent("owner", "natural next turn after DB error", k.now())); err != nil {
		t.Fatal(err)
	}
	if len(model.reqs) != 2 || wakes != 0 {
		t.Fatalf("natural reader-error path calls=%d wakes=%d", len(model.reqs), wakes)
	}
}

func TestExperienceRuntimeMissingDBSearchDoesNotCreateDatabase(t *testing.T) {
	k, endpoint := experienceRuntimeFixture(t, false)
	path := filepath.Join(bot.Dir(k.Bot.Root, k.Bot.Name()), experience.FileName)
	for _, query := range []string{"", "missing query"} {
		var reply cliops.ExperienceSearchReply
		status, raw := experienceRuntimeHTTP(t, endpoint, k.Bot.Root, "search", "fake-finished-id", cliops.ExperienceSearch{Query: query}, &reply)
		if status != http.StatusOK || reply.Text != "no matching experiences" || reply.File != "" {
			t.Fatalf("missing DB search %q: %s", query, raw)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("original exists guard created missing DB: %v", err)
		}
	}
	// Existing database opens retain their original CREATE TABLE/WAL behavior;
	// this assertion intentionally does not claim zero writes across the root.
}

func TestExperienceRuntimeSearchArtifactExactSemanticTextAndCurrentRename(t *testing.T) {
	for _, character := range []string{"\x01", "<"} {
		t.Run(map[string]string{"\x01": "control", "<": "HTML"}[character], func(t *testing.T) {
			k, endpoint := experienceRuntimeFixture(t, true)
			const heldName, heldSecret = "XBOT_EXPERIENCE_RUNTIME_TEST_KEY", "held-experience-secret-12345"
			oldHeld, _ := config.Held(heldName)
			config.SetHeld(heldName, heldSecret)
			t.Cleanup(func() { config.SetHeld(heldName, oldHeld) })
			k.Redact = func(text string) string { return strings.ReplaceAll(text, "SOURCE_SECRET", "[source]") }
			focus := "SOURCE_SECRET " + heldSecret + " " + strings.Repeat(character, 12000)
			if _, err := k.RecordExperience("SEMANTICALLY_CROPPED_SUMMARY", focus, 0, 0); err != nil {
				t.Fatal(err)
			}
			original, err := k.SearchExperiences("", 0)
			if err != nil {
				t.Fatal(err)
			}
			want := config.RedactHeld(original)
			full := cliops.ExperienceSearchReply{Text: want}
			wantJSON, err := json.Marshal(full)
			if err != nil {
				t.Fatal(err)
			}
			wantJSON = append(wantJSON, '\n')
			if len(want) > experienceSearchCap || len(wantJSON) <= tools.DefaultShellOutputBytes || len(cliops.FormatTextResult(full)) > tools.DefaultShellOutputBytes || strings.Contains(want, "SEMANTICALLY_CROPPED_SUMMARY") {
				t.Fatalf("fixture must exceed exact JSON budget after semantic cap: text=%d json=%d human=%d", len(want), len(wantJSON), len(cliops.FormatTextResult(full)))
			}
			// presentHistory invokes this legitimate dependency after SQLite IO
			// has released the route. Move the source at that exact boundary,
			// using an existing profile operation rather than a production hook.
			var renameOnce sync.Once
			var renameErr error
			k.Redact = func(text string) string {
				renameOnce.Do(func() {
					name := "renamed"
					_, renameErr = k.ApplyProfileChange(bot.ProfileChange{Name: &name})
				})
				return strings.ReplaceAll(text, "SOURCE_SECRET", "[source]")
			}
			var reply cliops.ExperienceSearchReply
			status, raw := experienceRuntimeHTTP(t, endpoint, k.Bot.Root, "search", "", cliops.ExperienceSearch{}, &reply)
			if renameErr != nil || status != http.StatusOK || reply.Text != "" || reply.File == "" || reply.Access != "local" || reply.Bytes != int64(len(wantJSON)) || len(cliops.FormatTextResult(reply)) > tools.DefaultShellOutputBytes {
				t.Fatalf("current artifact metadata %s renameErr=%v status=%d", raw, renameErr, status)
			}
			path := filepath.Join(work.BotDir(k.Bot.Root, "renamed"), filepath.FromSlash(reply.File))
			gotJSON, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(gotJSON, wantJSON) || strings.Contains(string(gotJSON), heldSecret) || strings.Contains(string(gotJSON), "SOURCE_SECRET") {
				t.Fatalf("FULL original semantic result JSON mismatch bytes=%d/%d err=%v", len(gotJSON), len(wantJSON), err)
			}
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != 0600 || info.Size() > work.ResultMaxBytes {
				t.Fatalf("artifact permissions/bound info=%v err=%v", info, err)
			}
			for _, old := range []string{bot.Dir(k.Bot.Root, "source"), work.BotDir(k.Bot.Root, "source")} {
				if _, err := os.Stat(old); !os.IsNotExist(err) {
					t.Fatalf("artifact revived old source path %s: %v", old, err)
				}
			}
		})
	}
}

func TestExperienceRuntimeArtifactFilesystemFailureHasNoFalseReference(t *testing.T) {
	k, endpoint := experienceRuntimeFixture(t, true)
	if _, err := k.RecordExperience("artifact error", strings.Repeat("\x02", 12000), 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(k.WorkDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(k.WorkDir, work.ResultDir), []byte("block actual result directory"), 0600); err != nil {
		t.Fatal(err)
	}
	var reply cliops.ExperienceSearchReply
	status, raw := experienceRuntimeHTTP(t, endpoint, k.Bot.Root, "search", "", cliops.ExperienceSearch{}, &reply)
	if status < 400 || reply.File != "" || reply.Bytes != 0 || reply.Access != "" || reply.Text != "" || !strings.Contains(reply.Error, "write search result") {
		t.Fatalf("failed real filesystem write returned a false artifact/full oversize text: %s status=%d", raw, status)
	}
}

func TestExperienceRuntimeErrorRedactionAndDisplayBudgetPreserveEntry(t *testing.T) {
	k, _ := experienceRuntimeFixture(t, true)
	experienceRuntimeAppend(t, k.Bot, bot.Entry{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("retain actual range")}})
	path := filepath.Join(bot.Dir(k.Bot.Root, k.Bot.Name()), experience.FileName)
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	const name, secret = "XBOT_EXPERIENCE_ERROR_TEST_KEY", "held-error-secret-12345"
	previous, _ := config.Held(name)
	config.SetHeld(name, secret)
	t.Cleanup(func() { config.SetHeld(name, previous) })
	k.Redact = func(string) string { return "[source-redacted] " + secret + strings.Repeat("\x01", 10000) }
	h := k.ExperienceHandlers()
	reply, err := h.Record(context.Background(), cliops.ExperienceRecord{Summary: "real SQLite failure"})
	raw, marshalErr := json.Marshal(reply)
	if err == nil || marshalErr != nil || reply.Recorded != nil || reply.From != 1 || reply.To != 1 || len(raw)+1 > tools.DefaultShellOutputBytes || len(cliops.FormatExperienceRecord(reply)) > tools.DefaultShellOutputBytes || strings.Contains(reply.Error, secret) || !strings.Contains(reply.Error, "[source-redacted]") || !strings.Contains(reply.Error, "[secret]") || err.Error() != reply.Error {
		t.Fatalf("display-only clipping lost entry/redaction range=%d/%d recorded=%v json=%d human=%d err=%v marshal=%v", reply.From, reply.To, reply.Recorded, len(raw)+1, len(cliops.FormatExperienceRecord(reply)), err, marshalErr)
	}
}

func TestExperienceRuntimeOperationLeaseSurvivesRetirementUntilCallbackReturns(t *testing.T) {
	k, _ := experienceRuntimeFixture(t, true)
	if _, err := k.RecordExperience("retained callback", "project", 0, 0); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	k.Redact = func(text string) string {
		once.Do(func() { close(entered); <-release })
		return text
	}
	type result struct {
		reply cliops.ExperienceSearchReply
		err   error
	}
	done := make(chan result, 1)
	h := k.ExperienceHandlers()
	go func() {
		reply, err := h.Search(context.Background(), cliops.ExperienceSearch{})
		done <- result{reply, err}
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("original search redaction not reached")
	}
	k.RetireOperations()
	k.operations.mu.Lock()
	retained := k.operations.server != nil && k.operations.calls == 1
	k.operations.mu.Unlock()
	close(release)
	select {
	case got := <-done:
		if !retained || got.err != nil || !strings.Contains(got.reply.Text, "retained callback") {
			t.Fatalf("retiring callback lost source lease retained=%v reply=%+v err=%v", retained, got.reply, got.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("source callback did not finish after releasing redactor")
	}
	k.operations.mu.Lock()
	closed := k.operations.server == nil && k.operations.calls == 0
	k.operations.mu.Unlock()
	if !closed {
		t.Fatal("finished original callback retained an idle endpoint")
	}
}
