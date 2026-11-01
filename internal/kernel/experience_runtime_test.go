package kernel

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/experience"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
	"xbot/internal/tools"
	"xbot/internal/work"
)

func experienceRuntimeFixture(t *testing.T, routed bool) (*Kernel, string) {
	t.Helper()
	b, err := bot.Create(t.TempDir(), "source", "Keep the project decisions", []string{link.NameTerminal, link.NameMemory, link.NameContext})
	if err != nil {
		t.Fatal(err)
	}
	k := &Kernel{Bot: b}
	if routed {
		k = botControlKernel(t, b)
	}
	k.Memory = memory.NopStore{}
	k.WorkDir = work.BotDir(b.Root, b.Name())
	k.Now = func() time.Time { return time.Unix(1700000000, 123456789).UTC() }
	if err := k.StartOperations(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(k.CloseOperations)
	t.Cleanup(func() { bot.CloseStore(b) })
	return k, operationEndpoint(k)
}

func experienceRuntimeAppend(t *testing.T, b *bot.Bot, entries ...bot.Entry) {
	t.Helper()
	for _, e := range entries {
		e.Time = time.Unix(1700000000, 0).UTC()
		if err := b.Append(e); err != nil {
			t.Fatal(err)
		}
	}
}

// This reaches the actual source listener, root/Host guard, transport decoder,
// callback factory and existing SQLite writer. It is a unit integration helper,
// not a CLI binary or browser end-to-end fixture.
func experienceRuntimeHTTP(t *testing.T, endpoint, root, action, callID string, in, out any) (int, []byte) {
	t.Helper()
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, endpoint+"/v1/experience/"+action, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	canonicalRoot, err := cliops.CanonicalRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-XBot-Root", canonicalRoot)
	if callID != "" {
		req.Header.Set("X-XBot-Call-ID", callID)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, out); err != nil {
		t.Fatalf("status=%d response=%q: %v", response.StatusCode, body, err)
	}
	if len(body) > tools.DefaultShellOutputBytes || len(body) == 0 || body[len(body)-1] != '\n' {
		t.Fatalf("JSON response budget/newline: %d bytes", len(body))
	}
	return response.StatusCode, body
}

func TestExperienceRuntimeAgentJobsHTTPUsesOperationTimeSource(t *testing.T) {
	k, endpoint := experienceRuntimeFixture(t, true)
	other, err := bot.Create(k.Bot.Root, "primary", "Other project", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := bot.SetPrimary(k.Bot.Root, other.Name()); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XBOT_BOT", "stale-foreign-bot")
	body := "TOOL_BODY_BEGIN " + strings.Repeat("x", 60<<10) + " TOOL_BODY_END"
	experienceRuntimeAppend(t, k.Bot,
		bot.Entry{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("old user")}},
		bot.Entry{Message: toolCall("c", "sh", `{"command":"inspect project"}`)},
		bot.Entry{Message: provider.Message{Role: provider.RoleTool, Content: provider.ToolResult("c", body)}},
	)
	k.ensureTurn().fromSeq = 77
	var recorded cliops.ExperienceRecordReply
	var actualCallID string
	runRoutineSource(t, k, nil, `{"command":"experience record --self --summary decision --json"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
		if turnSeqOf(ctx) != 77 {
			t.Errorf("source was not prepared with the original turn")
		}
		actualCallID = tools.CallID(ctx)
		if actualCallID == "" {
			t.Error("actual Jobs call ID missing")
		}
		experienceRuntimeAppend(t, k.Bot,
			bot.Entry{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("latest bot-origin user"), Internal: false}},
			bot.Entry{Origin: bot.OriginInternal, Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("internal user")}},
			bot.Entry{Mark: &bot.ForkMark{Kind: bot.ForkKindStart, Branch: "bg_test", From: 1}, Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("fork-mark user")}},
			bot.Entry{Message: provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("decided release Friday")}},
		)
		tail, err := k.currentCount()
		if err != nil {
			t.Fatal(err)
		}
		status, raw := experienceRuntimeHTTP(t, endpoint, k.Bot.Root, "record", actualCallID, cliops.ExperienceRecord{Summary: "  release decision  ", Focus: "  project  "}, &recorded)
		if status != http.StatusOK || recorded.Recorded == nil || !*recorded.Recorded || recorded.ID != 1 || recorded.From != tail-3 || recorded.To != tail {
			t.Errorf("operation-time/nonprimary source: %s", raw)
		}
		if strings.Contains(string(raw), "release decision") || strings.Contains(string(raw), "project") {
			t.Error("short ACK echoed request or segment bodies")
		}
		return "completed", nil
	})
	if k.Provider != nil {
		t.Fatal("direct original primitive needed a model provider")
	}
	saved, count, ok, err := experience.At(bot.Dir(k.Bot.Root, k.Bot.Name())).Latest()
	if err != nil || !ok || count != 1 || saved.ID != recorded.ID || !saved.CreatedAt.Equal(k.now()) || saved.Summary != "release decision" || saved.Focus != "project" || saved.From != recorded.From || saved.To != recorded.To {
		t.Fatalf("original SQLite fields %+v count=%d ok=%v err=%v", saved, count, ok, err)
	}
	if !strings.HasPrefix(saved.Segment, "user: latest bot-origin user\n") || !strings.Contains(saved.Segment, "assistant: decided release Friday") || strings.Contains(saved.Segment, "internal user") || strings.Contains(saved.Segment, "fork-mark user") || strings.Contains(saved.Segment, "TOOL_BODY") {
		t.Fatalf("original segment vocabulary/filtering: %q", saved.Segment)
	}
	// Neither a plain request nor a completed/fake call ID adds a new eligibility
	// requirement. This primitive never consumed source call context.
	for _, callID := range []string{"", actualCallID, "fake-finished-id"} {
		var reply cliops.ExperienceRecordReply
		status, raw := experienceRuntimeHTTP(t, endpoint, k.Bot.Root, "record", callID, cliops.ExperienceRecord{Summary: "plain record", From: 1, To: 3}, &reply)
		if status != http.StatusOK || reply.Recorded == nil || !*reply.Recorded || reply.From != 1 || reply.To != 3 {
			t.Fatalf("optional call ID %q rejected: %s", callID, raw)
		}
	}
	var found cliops.ExperienceSearchReply
	status, raw := experienceRuntimeHTTP(t, endpoint, k.Bot.Root, "search", "fake-finished-id", cliops.ExperienceSearch{Query: "release"}, &found)
	want, err := k.SearchExperiences("release", 0)
	if status != http.StatusOK || err != nil || found.Text != want || strings.Contains(found.Text, "TOOL_BODY") {
		t.Fatalf("original search semantics: %s want=%q err=%v", raw, want, err)
	}
	for _, args := range [][2]int{{3, 0}, {2, 2}} {
		text, err := k.Detail(args[0], int64(args[1]))
		if err != nil || !strings.Contains(text, "TOOL_BODY_BEGIN") || !strings.Contains(text, "TOOL_BODY_END") {
			t.Fatalf("still-live full blob detail %v: %.100s err=%v", args, text, err)
		}
	}
}

func TestExperienceRuntimeOriginalBoundsSummaryFocusAndIgnoredContext(t *testing.T) {
	k, _ := experienceRuntimeFixture(t, false)
	h := k.ExperienceHandlers()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reply, err := h.Record(ctx, cliops.ExperienceRecord{Summary: "\n" + strings.Repeat("s", 2048) + strings.Repeat(" ", 20<<10), Focus: "  " + strings.Repeat("F", 90<<10) + "  "})
	if err != nil || reply.Recorded == nil || !*reply.Recorded || reply.From != 0 || reply.To != 0 {
		t.Fatalf("original trim/empty/context behavior %+v %v", reply, err)
	}
	e, _, _, err := experience.At(bot.Dir(k.Bot.Root, k.Bot.Name())).Latest()
	if err != nil || len(e.Summary) != 2048 || len(e.Focus) != 90<<10 {
		t.Fatalf("original caps changed summary=%d focus=%d err=%v", len(e.Summary), len(e.Focus), err)
	}
	for _, summary := range []string{" \n\t", strings.Repeat("s", 2049)} {
		reply, err := h.Record(context.Background(), cliops.ExperienceRecord{Summary: summary})
		raw, _ := json.Marshal(reply)
		if err == nil || reply.Recorded != nil || !strings.Contains(string(raw), `"recorded":null`) || reply.ID != 0 || reply.Error == "" {
			t.Fatalf("invalid original summary %+v %s %v", reply, raw, err)
		}
	}
	experienceRuntimeAppend(t, k.Bot, bot.Entry{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("owner")}}, bot.Entry{Message: provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("answer")}})
	for _, tc := range []struct {
		from, to, wantFrom, wantTo int
		bad                        bool
	}{
		{0, 0, 1, 2, false}, {2, 2, 2, 2, false}, {0, 1, 1, 1, false},
		{0, -1, 0, -1, false}, {2, -1, 2, -1, false},
		{-1, 2, 0, 0, true}, {2, 1, 0, 0, true}, {1, 3, 0, 0, true},
	} {
		reply, err := h.Record(ctx, cliops.ExperienceRecord{Summary: "range", From: tc.from, To: tc.to})
		if (err != nil) != tc.bad || reply.From != tc.wantFrom || reply.To != tc.wantTo || (reply.Recorded != nil) == tc.bad {
			t.Fatalf("legacy range %d/%d %+v err=%v", tc.from, tc.to, reply, err)
		}
	}
	if _, err := h.Search(ctx, cliops.ExperienceSearch{}); err != nil {
		t.Fatalf("original search gained ctx cancellation requirement: %v", err)
	}
}

func TestExperienceRuntimeLatestUserWindowAndSegment(t *testing.T) {
	k, _ := experienceRuntimeFixture(t, false)
	experienceRuntimeAppend(t, k.Bot, bot.Entry{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("outside latest 200")}})
	for i := 0; i < 205; i++ {
		experienceRuntimeAppend(t, k.Bot, bot.Entry{Message: provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent(fmt.Sprintf("assistant %d", i))}})
	}
	reply, err := k.ExperienceHandlers().Record(context.Background(), cliops.ExperienceRecord{Summary: "tail fallback"})
	if err != nil || reply.From != 206 || reply.To != 206 {
		t.Fatalf("latest 200 fallback changed: %+v %v", reply, err)
	}
	reply, err = k.ExperienceHandlers().Record(context.Background(), cliops.ExperienceRecord{Summary: "explicit outline", From: 1, To: 206})
	if err != nil {
		t.Fatal(err)
	}
	e, ok, err := experience.At(bot.Dir(k.Bot.Root, k.Bot.Name())).Get(reply.ID)
	if err != nil || !ok || len(e.Segment) > segmentDigestCap+40 || !strings.HasPrefix(e.Segment, "user: outside latest 200\n") || strings.Contains(e.Segment, "assistant 204") {
		t.Fatalf("original segment cap/window: %q %v", e.Segment, err)
	}
}

func TestExperienceRuntimeSearchOriginalBM25PoolAndLimits(t *testing.T) {
	k, _ := experienceRuntimeFixture(t, false)
	store := experience.At(bot.Dir(k.Bot.Root, k.Bot.Name()))
	if _, err := store.Add(experience.Entry{CreatedAt: k.now(), Summary: "archaeology OLD_FULL_WEB_ONLY"}); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(bot.Dir(k.Bot.Root, k.Bot.Name()), experience.FileName))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tx.Prepare(`INSERT INTO experiences(created_at,summary,focus,from_seq,to_seq,segment) VALUES(?,?,?,?,?,?)`)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2000; i++ {
		summary, segment := fmt.Sprintf("ordinary %d", i), ""
		if i == 1999 {
			summary, segment = "alpha design decision", "assistant: beta release\n[tools: sh]"
		}
		if _, err := stmt.Exec(k.now().Format(time.RFC3339Nano), summary, "project", nil, nil, segment); err != nil {
			t.Fatal(err)
		}
	}
	if err := stmt.Close(); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	h := k.ExperienceHandlers()
	for _, query := range []string{"", " ", "alpha beta", "archaeology", "does-not-exist"} {
		for _, limit := range []int{-3, 0, 1, 8, 9} {
			got, err := h.Search(context.Background(), cliops.ExperienceSearch{Query: query, Limit: limit})
			want, originalErr := k.SearchExperiences(query, limit)
			if err != nil || originalErr != nil || got.Text != want || got.File != "" || len(got.Text) > experienceSearchCap {
				t.Fatalf("original search %q/%d differs got=%+v want=%q err=%v/%v", query, limit, got, want, err, originalErr)
			}
		}
	}
	found, _ := h.Search(context.Background(), cliops.ExperienceSearch{Query: "alpha beta"})
	if !strings.Contains(found.Text, "#2001 ") || !strings.Contains(found.Text, "[tools: sh]") {
		t.Fatalf("BM25 original token search %q", found.Text)
	}
	old, _ := h.Search(context.Background(), cliops.ExperienceSearch{Query: "archaeology"})
	page, err := store.Page(8, 0, "archaeology")
	if old.Text != "no matching experiences" || err != nil || len(page) != 1 || page[0].ID != 1 {
		t.Fatalf("2000 recent pool vs existing full Web page: runtime=%q page=%+v err=%v", old.Text, page, err)
	}
}

func TestExperienceRuntimeSQLiteFailureRetainsOriginalUnconfirmedEntry(t *testing.T) {
	k, endpoint := experienceRuntimeFixture(t, false)
	experienceRuntimeAppend(t, k.Bot, bot.Entry{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("range retained")}})
	path := filepath.Join(bot.Dir(k.Bot.Root, k.Bot.Name()), experience.FileName)
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	var reply cliops.ExperienceRecordReply
	status, raw := experienceRuntimeHTTP(t, endpoint, k.Bot.Root, "record", "", cliops.ExperienceRecord{Summary: "attempt"}, &reply)
	if status < 400 || reply.Recorded != nil || reply.From != 1 || reply.To != 1 || reply.Error == "" || !strings.Contains(string(raw), `"recorded":null`) {
		t.Fatalf("original returned entry on real SQLite FS failure: %s status=%d", raw, status)
	}
	// An Add error is not a transaction receipt. This test deliberately makes
	// no assertion that SQLite could never have created a file/row before error.
}
