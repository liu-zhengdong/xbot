package bot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"xbot/internal/provider"
)

func chatTestBot(t *testing.T) *Bot {
	t.Helper()
	b, err := Create(t.TempDir(), "chat", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { CloseStore(b) })
	return b
}
func chatAppend(t *testing.T, b *Bot, e Entry) int {
	t.Helper()
	seq, err := b.AppendSeq(e)
	if err != nil {
		t.Fatal(err)
	}
	return seq
}
func chatEntry(role, text string) Entry {
	return Entry{Time: toolChatSince.Add(time.Hour), Message: provider.Message{Role: role, Content: provider.TextContent(text)}}
}
func chatCall(id, text string) Entry {
	e := chatEntry(provider.RoleAssistant, "hidden narration")
	args, _ := json.Marshal(map[string]string{"text": text})
	e.Message.Content = append(e.Message.Content, provider.ToolContent([]provider.ToolCall{{ID: id, Function: provider.FunctionCall{Name: "send_message_to_user", Arguments: string(args)}}})...)
	return e
}
func chatResult(id string) Entry {
	e := chatEntry(provider.RoleTool, "")
	e.Message.Content = provider.ToolResult(id, "sent to the owner's chat")
	return e
}
func chatWindow(t *testing.T, b *Bot, q WindowQuery) ChatWindow {
	t.Helper()
	w, err := b.Chat().Window(q)
	if err != nil {
		t.Fatal(err)
	}
	return w
}
func TestChatRecordSnapshotAndIdempotence(t *testing.T) {
	b := chatTestBot(t)
	row := ChatRow{TranscriptSeq: 1, DeliveryKey: "say:manual", Kind: "bot", Text: "original", Images: []string{"first.png"}}
	if err := b.Chat().Record(row); err != nil {
		t.Fatal(err)
	}
	row.Text = "rewritten"
	row.Images = []string{"other.png"}
	if err := b.Chat().Record(row); err != nil {
		t.Fatal(err)
	}
	got := chatWindow(t, b, WindowQuery{}).Messages
	if len(got) != 1 || got[0].Text != "original" || !reflect.DeepEqual(got[0].Images, []string{"first.png"}) {
		t.Fatalf("not an immutable snapshot: %+v", got)
	}
	hits, err := b.Chat().Search("original", 20)
	if err != nil || len(hits) != 1 || hits[0].ID != got[0].ID {
		t.Fatalf("FTS snapshot: %+v %v", hits, err)
	}
}
func TestChatCompletedRunFreezesMultipleSaysWithMedia(t *testing.T) {
	b := chatTestBot(t)
	user := chatAppend(t, b, chatEntry(provider.RoleUser, "主人原话"))
	first := chatAppend(t, b, chatCall("a", "第一句"))
	chatAppend(t, b, chatResult("a"))
	chatAppend(t, b, chatCall("b", "第二句"))
	chatAppend(t, b, chatResult("b"))
	if got := chatWindow(t, b, WindowQuery{}).Messages; len(got) != 1 || got[0].Text != "主人原话" {
		t.Fatalf("unfinished run leaked: %+v", got)
	}
	// Restart between successful results and final media must retain the run.
	CloseStore(b)
	final := chatEntry(provider.RoleAssistant, "SILENT")
	final.Images = []string{"image.png"}
	final.Audios = []string{"voice.wav"}
	final.ReplyTo = user
	chatAppend(t, b, final)
	got := chatWindow(t, b, WindowQuery{}).Messages
	if len(got) != 3 || got[1].TranscriptSeq != first || got[1].Text != "第一句" || got[2].Text != "第二句" || len(got[1].Images) != 0 || len(got[1].Audios) != 0 || len(got[2].Images) != 1 || len(got[2].Audios) != 1 || got[1].ReplyToID != got[0].ID {
		t.Fatalf("delivery grouping: %+v", got)
	}
	original := got
	for i := 0; i < 15; i++ {
		chatAppend(t, b, chatEntry(provider.RoleTool, "noise"))
	}
	if got := chatWindow(t, b, WindowQuery{}).Messages; !reflect.DeepEqual(original, got) {
		t.Fatalf("tool noise changed frozen deliveries: %+v", got)
	}
}
func TestChatInternalPredicateAndBranchCompletion(t *testing.T) {
	b := chatTestBot(t)
	internal := chatEntry(provider.RoleUser, "ordinary-looking private note")
	internal.Origin = OriginInternal
	chatAppend(t, b, internal)
	owner := chatEntry(provider.RoleUser, "<environment_update>quoted by the owner</environment_update>")
	owner.Origin = OriginOwner
	chatAppend(t, b, owner)
	main := chatCall("main", "main delivery")
	chatAppend(t, b, main)
	chatAppend(t, b, chatResult("main"))
	background := chatCall("background", "background delivery")
	background.Branch = "bg_branch"
	chatAppend(t, b, background)
	chatAppend(t, b, chatResult("background"))
	final := chatEntry(provider.RoleAssistant, "hidden final")
	final.Branch = "bg_branch"
	chatAppend(t, b, final)
	got := chatWindow(t, b, WindowQuery{}).Messages
	if len(got) != 2 || got[0].Text != owner.Message.Text() || got[1].Text != "background delivery" {
		t.Fatalf("predicate/branch completion: %+v", got)
	}
	chatAppend(t, b, chatEntry(provider.RoleAssistant, "main final"))
	got = chatWindow(t, b, WindowQuery{}).Messages
	if len(got) != 3 || got[2].Text != "main delivery" {
		t.Fatalf("foreground run was flushed by another branch: %+v", got)
	}
}

func TestChatSparseHistoryPagingAndFTS(t *testing.T) {
	b := chatTestBot(t)
	for i := 0; i < 65; i++ {
		chatAppend(t, b, chatEntry(provider.RoleUser, fmt.Sprintf("owner %02d", i)))
		for j := 0; j < 10; j++ {
			chatAppend(t, b, chatEntry(provider.RoleTool, "toolonlyneedle "+strings.Repeat("noise ", 100)))
		}
		chatAppend(t, b, chatCall(fmt.Sprintf("s%d", i), fmt.Sprintf("visible %02d 历史线索", i)))
		chatAppend(t, b, chatResult(fmt.Sprintf("s%d", i)))
		chatAppend(t, b, chatEntry(provider.RoleAssistant, "hiddenonlyneedle"))
	}
	tail := chatWindow(t, b, WindowQuery{})
	if len(tail.Messages) != 40 || tail.Window.Total != 130 || tail.Window.HeadID != 91 {
		t.Fatalf("not chat-row limited: %+v", tail.Window)
	}
	before := chatWindow(t, b, WindowQuery{Before: tail.Window.NextBefore, Limit: 17})
	after := chatWindow(t, b, WindowQuery{After: before.Window.NextAfter, Limit: 40})
	around := chatWindow(t, b, WindowQuery{Around: 12, Limit: 8})
	if len(before.Messages) != 17 || before.Window.TailID != 90 || after.Window.HeadID != 91 || around.Window.HeadID > 12 || around.Window.TailID < 12 {
		t.Fatalf("cursor windows: %+v %+v %+v", before.Window, after.Window, around.Window)
	}
	for _, q := range []string{"toolonlyneedle", "hiddenonlyneedle"} {
		hits, err := b.Chat().Search(q, 20)
		if err != nil || len(hits) != 0 {
			t.Fatalf("hidden content in chat FTS: %s %+v %v", q, hits, err)
		}
	}
	hits, err := b.Chat().Search("线索", 100)
	if err != nil || len(hits) != 65 || hits[0].Kind != "bot" {
		t.Fatalf("Chinese chat FTS: %d %v", len(hits), err)
	}
}
func TestChatBackfillGenerationAndLedgerUnchanged(t *testing.T) {
	b := chatTestBot(t)
	old := chatEntry(provider.RoleAssistant, "old reply")
	old.Time = toolChatSince.Add(-time.Hour)
	source := chatEntry(provider.RoleUser, "<untrusted_event source=\"cli\" type=\"message\" sender=\"builder\">\nsource body\n</untrusted_event>")
	entries := []Entry{chatEntry(provider.RoleUser, "owner"), old, chatEntry(provider.RoleUser, "next"), chatCall("legacy-say", "landed"), chatResult("legacy-say"), chatEntry(provider.RoleAssistant, "hidden"), source, chatEntry(provider.RoleUser, ApprovalCardText("card", "ask_user", "", "", "question", ApprovalPending)), chatEntry(provider.RoleUser, "<untrusted_event source=\"timer\">\nwake\n</untrusted_event>")}
	var ledger []byte
	for _, e := range entries {
		raw, _ := json.Marshal(e)
		ledger = append(ledger, raw...)
		ledger = append(ledger, '\n')
	}
	legacy := filepath.Join(b.dir(), "transcript.jsonl")
	if err := os.WriteFile(legacy, ledger, 0600); err != nil {
		t.Fatal(err)
	}
	got := chatWindow(t, b, WindowQuery{}).Messages
	var texts []string
	for _, m := range got {
		texts = append(texts, m.Text)
	}
	if len(got) != 6 || got[1].Text != "old reply" || got[3].Text != "landed" || got[4].Kind != "source" || got[5].Kind != "card" {
		t.Fatalf("backfill: %+v", texts)
	}
	saved, err := os.ReadFile(legacy + ".migrated")
	if err != nil || !reflect.DeepEqual(saved, ledger) {
		t.Fatal("legacy ledger was edited", err)
	}
	s := b.store()
	if err := s.with(func() error { _, err := s.db.Exec(`UPDATE chat_projection SET generation=0`); return err }); err != nil {
		t.Fatal(err)
	}
	rebuilt := chatWindow(t, b, WindowQuery{}).Messages
	if !reflect.DeepEqual(got, rebuilt) {
		t.Fatalf("generation rebuild changed deliveries: %+v", rebuilt)
	}
}
