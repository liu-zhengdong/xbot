package bot

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/provider"
)

func TestWindowVisibleTailAndBefore(t *testing.T) {
	b, err := Create(t.TempDir(), "visible", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer CloseStore(b)
	appendEntry := func(e Entry) {
		t.Helper()
		e.Time = time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
		if err := b.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	// Each page ends in sixty records with no chat bubbles at all.
	for page := 0; page < 2; page++ {
		for i := 0; i < 25; i++ {
			call := provider.ToolCall{ID: fmt.Sprintf("say-%d-%d", page, i), Type: "function"}
			call.Function.Name, call.Function.Arguments = "send_message_to_user", `{"text":"visible delivery"}`
			appendEntry(Entry{Message: provider.Message{Role: provider.RoleAssistant, Content: provider.ToolContent([]provider.ToolCall{call})}})
			appendEntry(Entry{Message: provider.Message{Role: provider.RoleTool, Content: provider.ToolResult(call.ID, "sent to the owner's chat")}})
		}
		for i := 0; i < 20; i++ {
			appendEntry(Entry{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("<untrusted_event source=\"timer\" type=\"tick\">\nwake\n</untrusted_event>")}})
			appendEntry(Entry{Message: provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("SILENT")}})
			appendEntry(Entry{Message: provider.Message{Role: provider.RoleTool, Content: provider.ToolResult("noise", "done")}})
		}
	}
	before, _ := b.ConcatRaw()
	q := WindowQuery{}
	for page := 0; page < 2; page++ {
		w, err := b.Window(q)
		if err != nil {
			t.Fatal(err)
		}
		says := 0
		for i, r := range w.Rows {
			if i > 0 && r.Seq != w.Rows[i-1].Seq+1 {
				t.Fatal("gap in window")
			}
			says += len(r.Entry.Message.Calls())
		}
		if says < 25 || len(w.Rows) > MaxVisibleWindowLimit || w.TailSeq != 220-page*110 || w.HeadSeq != 111-page*110 || w.HasMoreBefore != (page == 0) {
			t.Fatalf("page %d: says=%d rows=%d meta=%+v", page, says, len(w.Rows), w.WindowMeta)
		}
		q.Before = w.NextBefore
	}
	fixed, err := b.Window(WindowQuery{Limit: 3})
	if err != nil || len(fixed.Rows) != 3 {
		t.Fatalf("explicit limit: %d, %v", len(fixed.Rows), err)
	}
	after, err := b.ConcatRaw()
	if err != nil || string(before) != string(after) {
		t.Fatal("window changed transcript")
	}
}

func TestWindowVisibleExpansionCaps(t *testing.T) {
	for _, large := range []bool{false, true} {
		t.Run(fmt.Sprint("large=", large), func(t *testing.T) {
			b, err := Create(t.TempDir(), "capped", "", nil)
			if err != nil {
				t.Fatal(err)
			}
			defer CloseStore(b)
			text := "SILENT"
			if large {
				text = strings.Repeat("x", 3000)
			}
			for i := 0; i < MaxVisibleWindowLimit+50; i++ {
				m := provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent(text)}
				if !large && i == 170 {
					call := provider.ToolCall{ID: "sparse", Type: "function"}
					call.Function.Name, call.Function.Arguments = "send_message_to_user", `{"text":"only one say within the cap"}`
					m.Content = provider.ToolContent([]provider.ToolCall{call})
				} else if !large && i == 171 {
					m.Role, m.Content = provider.RoleTool, provider.ToolResult("sparse", "sent to the owner's chat")
				}
				if err := b.Append(Entry{Time: time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC), Message: m}); err != nil {
					t.Fatal(err)
				}
			}
			w, err := b.Window(WindowQuery{})
			if err != nil {
				t.Fatal(err)
			}
			if w.TailSeq != MaxVisibleWindowLimit+50 || !w.HasMoreBefore || w.HasMoreAfter || w.NextBefore != w.HeadSeq || len(w.Rows) > MaxVisibleWindowLimit {
				t.Fatalf("cap metadata: %+v", w.WindowMeta)
			}
			if !large && len(w.Rows) != MaxVisibleWindowLimit {
				t.Fatalf("raw cap: %d", len(w.Rows))
			}
			if large && len(w.Rows) >= windowBytes/3000 {
				t.Fatalf("byte cap ignored: %d", len(w.Rows))
			}
		})
	}
}

func TestWindowVisibleProjection(t *testing.T) {
	for _, kind := range []string{"user", "legacy", "image", "audio", "notify_owner"} {
		t.Run(kind, func(t *testing.T) {
			b, err := Create(t.TempDir(), "projection", "", nil)
			if err != nil {
				t.Fatal(err)
			}
			defer CloseStore(b)
			at := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
			appendEntry := func(e Entry) {
				t.Helper()
				if e.Time.IsZero() {
					e.Time = at
				}
				if err := b.Append(e); err != nil {
					t.Fatal(err)
				}
			}
			for i := 0; i < 25; i++ {
				e := Entry{Message: provider.Message{Role: provider.RoleAssistant}}
				switch kind {
				case "user":
					e.Message.Role, e.Message.Content = provider.RoleUser, provider.TextContent("real input")
				case "legacy":
					e.Time, e.Message.Content = at.AddDate(0, 0, -10), provider.TextContent("old visible prose")
				case "image":
					e.Images = []string{"picture.png"}
				case "audio":
					e.Audios = []string{"voice.wav"}
				case "notify_owner":
					call := provider.ToolCall{ID: fmt.Sprint(i), Type: "function"}
					call.Function.Name, call.Function.Arguments = kind, `{"text":"old delivery"}`
					e.Message.Content = provider.ToolContent([]provider.ToolCall{call})
					appendEntry(e)
					e.Message = provider.Message{Role: provider.RoleTool, Content: provider.ToolResult(call.ID, "notified the owner")}
				}
				appendEntry(e)
				// A non-internal source event closes the run without a bubble.
				appendEntry(Entry{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("<untrusted_event source=\"timer\">\nwake\n</untrusted_event>")}})
			}
			for i := 0; i < 20; i++ {
				appendEntry(Entry{Message: provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("modern bare text is not speech")}})
				appendEntry(Entry{Origin: OriginInternal, Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("hidden kernel note")}})
				appendEntry(Entry{Message: provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("SILENT")}})
				call := provider.ToolCall{ID: fmt.Sprint("failed-", i), Type: "function"}
				call.Function.Name, call.Function.Arguments = "send_message_to_user", `{"text":"not delivered"}`
				appendEntry(Entry{Message: provider.Message{Role: provider.RoleAssistant, Content: provider.ToolContent([]provider.ToolCall{call})}})
				appendEntry(Entry{Message: provider.Message{Role: provider.RoleTool, Content: provider.ToolResult(call.ID, "error: not sent")}})
			}
			w, err := b.Window(WindowQuery{})
			if err != nil {
				t.Fatal(err)
			}
			if w.HeadSeq != 1 || w.HasMoreBefore {
				t.Fatalf("%s did not bring all 25 visible deliveries: %+v", kind, w.WindowMeta)
			}
		})
	}
}

func TestWindowVisibleSaySuppressesLegacyRun(t *testing.T) {
	b, err := Create(t.TempDir(), "suppressed", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer CloseStore(b)
	at := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	appendEntry := func(m provider.Message) {
		t.Helper()
		if err := b.Append(Entry{Time: at, Message: m}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 24; i++ {
		appendEntry(provider.Message{Role: provider.RoleUser, Content: provider.TextContent("input")})
	}
	for i := 0; i < 25; i++ {
		appendEntry(provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("legacy prose suppressed by the landed say in this run")})
	}
	call := provider.ToolCall{ID: "landed", Type: "function"}
	call.Function.Name, call.Function.Arguments = "send_message_to_user", `{"text":"visible"}`
	appendEntry(provider.Message{Role: provider.RoleAssistant, Content: provider.ToolContent([]provider.ToolCall{call})})
	appendEntry(provider.Message{Role: provider.RoleTool, Content: provider.ToolResult(call.ID, "sent to the owner's chat")})
	for i := 0; i < 60; i++ {
		appendEntry(provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("SILENT")})
	}
	w, err := b.Window(WindowQuery{})
	// The run contributes one say plus at most one aside, not 25 legacy bubbles.
	if err != nil || w.HeadSeq != 2 {
		t.Fatalf("legacy run did not collapse to say + aside: %+v %v", w.WindowMeta, err)
	}
}

func TestWindowVisibleSayResultOutsideBefore(t *testing.T) {
	b, err := Create(t.TempDir(), "outside", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer CloseStore(b)
	at := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	appendEntry := func(m provider.Message) {
		t.Helper()
		if err := b.Append(Entry{Time: at, Message: m}); err != nil {
			t.Fatal(err)
		}
	}
	// 24 genuine user bubbles, then a say whose success is outside this page.
	for i := 0; i < 24; i++ {
		appendEntry(provider.Message{Role: provider.RoleUser, Content: provider.TextContent("input")})
	}
	for i := 0; i < 80; i++ {
		appendEntry(provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("SILENT")})
	}
	call := provider.ToolCall{ID: "outside", Type: "function"}
	call.Function.Name, call.Function.Arguments = "send_message_to_user", `{"text":"delivered"}`
	appendEntry(provider.Message{Role: provider.RoleAssistant, Content: provider.ToolContent([]provider.ToolCall{call})})
	appendEntry(provider.Message{Role: provider.RoleTool, Content: provider.ToolResult(call.ID, "sent to the owner's chat")})
	w, err := b.Window(WindowQuery{Before: 106})
	if err != nil || w.HeadSeq != 1 || w.TailSeq != 105 {
		t.Fatalf("outside success: %+v %v", w.WindowMeta, err)
	}
}

func TestWindowRangeReplayAndIndexUpgrade(t *testing.T) {
	b, err := Create(t.TempDir(), "reader", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer CloseStore(b)
	b.SetSegmentMax(300)
	for i := 0; i < 100; i++ {
		if err := b.Append(Entry{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("unchanged original")}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := b.AppendFork("bg_old", 10, "legacy interval"); err != nil {
		t.Fatal(err)
	}
	call := provider.ToolCall{ID: "call", Type: "function"}
	call.Function.Name = "shell"
	call.Function.Arguments = `{}`
	if err := b.Append(Entry{Message: provider.Message{Role: provider.RoleAssistant, Content: provider.ToolContent([]provider.ToolCall{call})}}); err != nil {
		t.Fatal(err)
	}
	if err := b.Append(Entry{Message: provider.Message{Role: provider.RoleTool, Content: provider.ToolResult("call", "done")}}); err != nil {
		t.Fatal(err)
	}
	before, err := b.ConcatRaw()
	if err != nil {
		t.Fatal(err)
	}
	w, err := b.Window(WindowQuery{Around: 50, Limit: 3})
	if err != nil || len(w.Rows) != 3 || w.Rows[1].Seq != 50 {
		t.Fatalf("window %+v: %v", w, err)
	}
	for _, row := range w.Rows {
		if row.Branch != "bg_old" {
			t.Fatalf("outside marker lost branch: %+v", row)
		}
	}
	older, err := b.Window(WindowQuery{Before: w.HeadSeq, Limit: 3})
	if err != nil || older.TailSeq != w.HeadSeq-1 {
		t.Fatalf("older %+v %v", older, err)
	}
	newer, err := b.Window(WindowQuery{After: older.TailSeq, Limit: 3})
	if err != nil || newer.HeadSeq != w.HeadSeq || newer.TailSeq != w.TailSeq {
		t.Fatalf("newer %+v %v", newer, err)
	}
	// Simulate an interrupted derived-index upgrade: tables exist, but their
	// version is uncommitted and their contents incomplete. Reopen repairs it.
	s := b.store()
	if _, err := s.db.Exec(`DELETE FROM fork_spans; DELETE FROM tool_results; PRAGMA user_version=0`); err != nil {
		t.Fatal(err)
	}
	CloseStore(b)
	w, err = b.Window(WindowQuery{Around: 50, Limit: 1})
	if err != nil || w.Rows[0].Branch != "bg_old" {
		t.Fatalf("upgrade lost old fork: %+v %v", w, err)
	}
	result, found, err := b.ToolResult("call")
	if err != nil || !found || result.Message.Text() != "done" {
		t.Fatalf("result upgrade: %+v %v %v", result, found, err)
	}
	after, err := b.ConcatRaw()
	if err != nil || string(before) != string(after) {
		t.Fatal("window/index upgrade changed transcript")
	}
	// No full-history fallback after upgrade: the index still names a corrupt
	// sealed old segment, but windows covering other segments remain readable.
	files, err := b.SegmentFiles()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(files[0], 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(files[0], []byte("broken history\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Window(WindowQuery{}); err != nil {
		t.Fatalf("tail touched unrelated sealed history: %v", err)
	}
	if _, err := b.Window(WindowQuery{Around: 1, Limit: 1}); err == nil {
		t.Fatal("selected corruption not reported")
	}
	if _, err := os.Stat(filepath.Join(b.dir(), "transcript", "index.sqlite")); err != nil {
		t.Fatal(err)
	}
}
