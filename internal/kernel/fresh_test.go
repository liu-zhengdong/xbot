package kernel

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/compact"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

func toolCall(id, name, args string) provider.Message {
	return provider.Message{
		Role: provider.RoleAssistant,
		ToolCalls: []provider.ToolCall{{
			ID: id, Type: "function",
			Function: provider.FunctionCall{Name: name, Arguments: args},
		}},
	}
}

func mustArgs(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func freshBot(t *testing.T) (*Kernel, *scripted, *time.Time) {
	t.Helper()
	b := testBot(t, link.NameTerminal, link.NameTools)
	p := &scripted{}
	now := time.Unix(1_700_000_000, 0).UTC()
	k := &Kernel{
		Bot: b, Provider: p, Memory: memory.NopStore{}, MaxSteps: 8,
		Links: []link.Link{link.Terminal{}, link.NewToolsLink()},
		Now:   func() time.Time { return now },
	}
	return k, p, &now
}

func advance(now *time.Time, d time.Duration) {
	*now = now.Add(d)
}

func runTurn(t *testing.T, k *Kernel, now *time.Time, text string, resps ...provider.Message) {
	t.Helper()
	p := k.Provider.(*scripted)
	p.resps = append(p.resps, resps...)
	ev := link.TerminalEvent("u1", text, *now)
	if err := k.Handle(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
}

func bodies(t *testing.T, k *Kernel) []string {
	t.Helper()
	entries, err := k.Bot.Entries()
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if e.Message.Role == provider.RoleTool {
			out = append(out, e.Message.Content)
		}
	}
	return out
}

func TestUnreadWriteIsRefused(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(path, []byte("alpha\n"), 0644); err != nil {
		t.Fatal(err)
	}
	k, _, now := freshBot(t)
	write := mustArgs(t, map[string]string{"path": path, "content": "beta\n"})
	read := mustArgs(t, map[string]string{"path": path})
	runTurn(t, k, now, "edit it",
		toolCall("w1", "write_file", write),
		toolCall("r1", "read_file", read),
		toolCall("w2", "write_file", write),
		provider.Message{Role: provider.RoleAssistant, Content: "done"},
	)
	got := bodies(t, k)
	if len(got) != 3 {
		t.Fatalf("results %d: %q", len(got), got)
	}
	if !strings.Contains(got[0], msgNotRead) || strings.Contains(got[0], "beta") {
		t.Fatalf("unread write: %q", got[0])
	}
	if !strings.Contains(got[2], msgRefresh) {
		t.Fatalf("write after read: %q", got[2])
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "beta\n" {
		t.Fatalf("file %q err %v", raw, err)
	}
}

func TestStaleWriteIsRefused(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(path, []byte("alpha\n"), 0644); err != nil {
		t.Fatal(err)
	}
	k, _, now := freshBot(t)
	read := mustArgs(t, map[string]string{"path": path})
	runTurn(t, k, now, "look",
		toolCall("r1", "read_file", read),
		provider.Message{Role: provider.RoleAssistant, Content: "saw it"},
	)
	if err := os.WriteFile(path, []byte("external\n"), 0644); err != nil {
		t.Fatal(err)
	}
	advance(now, time.Second)
	write := mustArgs(t, map[string]string{"path": path, "content": "beta\n"})
	runTurn(t, k, now, "overwrite",
		toolCall("w1", "write_file", write),
		provider.Message{Role: provider.RoleAssistant, Content: "stopped"},
	)
	got := bodies(t, k)
	last := got[len(got)-1]
	if !strings.Contains(last, msgStale) || !strings.Contains(last, "step 1") {
		t.Fatalf("stale write: %q", last)
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "external\n" {
		t.Fatalf("file changed by the refused write: %q %v", raw, err)
	}
}

func TestOwnWriteRefreshesRecord(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(path, []byte("alpha\n"), 0644); err != nil {
		t.Fatal(err)
	}
	k, _, now := freshBot(t)
	read := mustArgs(t, map[string]string{"path": path})
	write := mustArgs(t, map[string]string{"path": path, "content": "beta\n"})
	edit := mustArgs(t, map[string]string{"path": path, "old_string": "beta\n", "new_string": "gamma\n"})
	runTurn(t, k, now, "rewrite",
		toolCall("r1", "read_file", read),
		toolCall("w1", "write_file", write),
		toolCall("e1", "edit_file", edit),
		provider.Message{Role: provider.RoleAssistant, Content: "done"},
	)
	got := bodies(t, k)
	if len(got) != 3 {
		t.Fatalf("results %q", got)
	}
	if !strings.Contains(got[1], msgRefresh) || !strings.Contains(got[2], msgRefresh) {
		t.Fatalf("refresh notes missing: %q", got)
	}
	if strings.Contains(got[2], msgNotRead) || strings.Contains(got[2], msgStale) {
		t.Fatalf("edit asked for another read: %q", got[2])
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "gamma\n" {
		t.Fatalf("file %q err %v", raw, err)
	}
}

func TestUnchangedReadIsReceipt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	const body = "purplefinch\nsecond\n"
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	k, _, now := freshBot(t)
	plain := mustArgs(t, map[string]string{"path": path})
	force := mustArgs(t, map[string]any{"path": path, "force": true})
	span := mustArgs(t, map[string]any{"path": path, "offset": 1, "limit": 1})
	runTurn(t, k, now, "read twice",
		toolCall("r1", "read_file", plain),
		toolCall("r2", "read_file", plain),
		toolCall("r3", "read_file", force),
		toolCall("r4", "read_file", span),
		provider.Message{Role: provider.RoleAssistant, Content: "done"},
	)
	got := bodies(t, k)
	if len(got) != 4 {
		t.Fatalf("results %d %q", len(got), got)
	}
	if !strings.Contains(got[0], "purplefinch") || !strings.Contains(got[0], "second") {
		t.Fatalf("first read: %q", got[0])
	}
	if !strings.Contains(got[1], msgReceipt) || !strings.Contains(got[1], "step 1") || strings.Contains(got[1], "purplefinch") {
		t.Fatalf("receipt: %q", got[1])
	}
	// ~30 tokens: the stand-in stays a short line, not the file.
	if len(strings.Fields(got[1])) > 40 {
		t.Fatalf("receipt too long (%d words): %q", len(strings.Fields(got[1])), got[1])
	}
	if !strings.Contains(got[2], "purplefinch") || !strings.Contains(got[2], "second") {
		t.Fatalf("force: %q", got[2])
	}
	if !strings.Contains(got[3], "purplefinch") || strings.Contains(got[3], "second") {
		t.Fatalf("range: %q", got[3])
	}
}

func TestFileChangeReminder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(path, []byte("alpha\n"), 0644); err != nil {
		t.Fatal(err)
	}
	k, p, now := freshBot(t)
	read := mustArgs(t, map[string]string{"path": path})
	runTurn(t, k, now, "look",
		toolCall("r1", "read_file", read),
		provider.Message{Role: provider.RoleAssistant, Content: "saw alpha"},
	)
	if err := os.WriteFile(path, []byte("alpha\nbeta\n"), 0644); err != nil {
		t.Fatal(err)
	}
	advance(now, fileChangeCooldown+time.Second)
	runTurn(t, k, now, "again",
		provider.Message{Role: provider.RoleAssistant, Content: "noted"},
	)
	last := p.reqs[len(p.reqs)-1].Messages
	newest := last[len(last)-1].Content
	if !strings.Contains(newest, "again") || !strings.Contains(newest, "<file_change>") || !strings.Contains(newest, "+ beta") {
		t.Fatalf("file change reminder: %q", newest)
	}
	entries, err := k.Bot.Entries()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Message.Content, "<file_change>") {
			t.Fatal("file change reminder was written into the transcript")
		}
	}
	advance(now, time.Second)
	runTurn(t, k, now, "soon",
		provider.Message{Role: provider.RoleAssistant, Content: "quiet"},
	)
	soon := p.reqs[len(p.reqs)-1].Messages
	if strings.Contains(soon[len(soon)-1].Content, "<file_change>") {
		t.Fatalf("cooldown did not hold: %q", soon[len(soon)-1].Content)
	}
	advance(now, fileChangeCooldown+time.Second)
	runTurn(t, k, now, "later",
		provider.Message{Role: provider.RoleAssistant, Content: "still"},
	)
	later := p.reqs[len(p.reqs)-1].Messages
	if strings.Contains(later[len(later)-1].Content, "<file_change>") {
		t.Fatalf("same change reminded twice: %q", later[len(later)-1].Content)
	}
	if err := os.WriteFile(path, []byte("alpha\nbeta\ngamma\n"), 0644); err != nil {
		t.Fatal(err)
	}
	advance(now, fileChangeCooldown+time.Second)
	runTurn(t, k, now, "third change",
		provider.Message{Role: provider.RoleAssistant, Content: "saw gamma"},
	)
	third := p.reqs[len(p.reqs)-1].Messages
	tip := third[len(third)-1].Content
	if !strings.Contains(tip, "<file_change>") || !strings.Contains(tip, "+ gamma") {
		t.Fatalf("new change: %q", tip)
	}
}

func TestDeniedArgsAreNotRetried(t *testing.T) {
	k, _, now := freshBot(t)
	var asked int
	k.Approve = func(context.Context, string, string) (bool, error) {
		asked++
		return false, nil
	}
	first := `{"command":"rm -rf note.txt","timeout_seconds":1}`
	same := `{"timeout_seconds":1,"command":"rm -rf note.txt"}`
	other := `{"command":"rm -rf other.txt","timeout_seconds":1}`
	runTurn(t, k, now, "delete",
		toolCall("s1", "shell", first),
		toolCall("s2", "shell", same),
		toolCall("s3", "shell", other),
		provider.Message{Role: provider.RoleAssistant, Content: "stopped"},
	)
	if asked != 2 {
		t.Fatalf("approver called %d times, want 2", asked)
	}
	got := bodies(t, k)
	if len(got) != 3 {
		t.Fatalf("results %q", got)
	}
	if got[0] != "denied by user" {
		t.Fatalf("first denial: %q", got[0])
	}
	if !strings.Contains(got[1], msgDenied) {
		t.Fatalf("retry: %q", got[1])
	}
	if got[2] != "denied by user" {
		t.Fatalf("different args: %q", got[2])
	}
}

func TestFileStateIsPerBot(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(path, []byte("alpha\n"), 0644); err != nil {
		t.Fatal(err)
	}
	k1, _, now1 := freshBot(t)
	read := mustArgs(t, map[string]string{"path": path})
	runTurn(t, k1, now1, "look",
		toolCall("r1", "read_file", read),
		provider.Message{Role: provider.RoleAssistant, Content: "saw it"},
	)
	k2, _, now2 := freshBot(t)
	write := mustArgs(t, map[string]string{"path": path, "content": "beta\n"})
	runTurn(t, k2, now2, "overwrite",
		toolCall("w1", "write_file", write),
		provider.Message{Role: provider.RoleAssistant, Content: "no"},
	)
	got := bodies(t, k2)
	if len(got) != 1 || !strings.Contains(got[0], msgNotRead) {
		t.Fatalf("other bot wrote unread file: %q", got)
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "alpha\n" {
		t.Fatalf("file %q %v", raw, err)
	}
}

func TestMtimeJitterDoesNotBlockWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(path, []byte("alpha\n"), 0644); err != nil {
		t.Fatal(err)
	}
	k, _, now := freshBot(t)
	read := mustArgs(t, map[string]string{"path": path})
	runTurn(t, k, now, "look",
		toolCall("r1", "read_file", read),
		provider.Message{Role: provider.RoleAssistant, Content: "saw it"},
	)
	later := now.Add(2 * time.Hour)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	advance(now, time.Second)
	write := mustArgs(t, map[string]string{"path": path, "content": "beta\n"})
	runTurn(t, k, now, "rewrite",
		toolCall("w1", "write_file", write),
		provider.Message{Role: provider.RoleAssistant, Content: "done"},
	)
	got := bodies(t, k)
	last := got[len(got)-1]
	if strings.Contains(last, msgStale) || !strings.Contains(last, msgRefresh) {
		t.Fatalf("jitter blocked the write: %q", last)
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "beta\n" {
		t.Fatalf("file %q %v", raw, err)
	}
}

func TestReminderCacheReloadsWhenFileChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reminders.jsonl")
	a := newChannel(path)
	b := newChannel(path)
	now := time.Unix(10, 0).UTC()
	if err := a.Record(1, KindLoop, "from-a", now); err != nil {
		t.Fatal(err)
	}
	got, err := b.log.all()
	if err != nil || len(got) != 1 || got[0].Text != "from-a" {
		t.Fatalf("load %+v %v", got, err)
	}
	if err := a.Record(2, KindTodo, "from-a-2", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	got, err = b.log.all()
	if err != nil || len(got) != 2 || got[1].Text != "from-a-2" {
		t.Fatalf("stale cache %+v %v", got, err)
	}
	raw := []byte("{\"seq\":2,\"kind\":\"todo\",\"text\":\"kept\",\"at\":\"2026-10-17T00:00:02Z\"}\n")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	got, err = b.log.all()
	if err != nil || len(got) != 1 || got[0].Text != "kept" {
		t.Fatalf("rewrite not seen %+v %v", got, err)
	}
}

func TestRangedReadThenEditDoesNotReceipt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\nHIDDEN-LINE-3\n"), 0644); err != nil {
		t.Fatal(err)
	}
	k, _, now := freshBot(t)
	runTurn(t, k, now, "go",
		toolCall("r1", "read_file", mustArgs(t, map[string]any{"path": path, "offset": 1, "limit": 1})),
		toolCall("e1", "edit_file", mustArgs(t, map[string]any{"path": path, "old_string": "one", "new_string": "ONE"})),
		toolCall("r2", "read_file", mustArgs(t, map[string]string{"path": path})),
		provider.Message{Role: provider.RoleAssistant, Content: "done"},
	)
	got := bodies(t, k)
	if len(got) != 3 {
		t.Fatalf("results %q", got)
	}
	last := got[len(got)-1]
	if strings.Contains(last, msgReceipt) || !strings.Contains(last, "HIDDEN-LINE-3") {
		t.Fatalf("plain read after ranged read+edit hid unread lines: %q", last)
	}
}

func TestWriteFileMarksFullAfterPartialRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\n"), 0644); err != nil {
		t.Fatal(err)
	}
	k, _, now := freshBot(t)
	runTurn(t, k, now, "replace",
		toolCall("r1", "read_file", mustArgs(t, map[string]any{"path": path, "offset": 1, "limit": 1})),
		toolCall("w1", "write_file", mustArgs(t, map[string]string{"path": path, "content": "ONE\nTWO\nTHREE\n"})),
		toolCall("r2", "read_file", mustArgs(t, map[string]string{"path": path})),
		provider.Message{Role: provider.RoleAssistant, Content: "done"},
	)
	got := bodies(t, k)
	if len(got) != 3 {
		t.Fatalf("results %q", got)
	}
	if strings.Contains(got[1], msgNotRead) || strings.Contains(got[1], msgStale) || !strings.Contains(got[1], msgRefresh) {
		t.Fatalf("write after partial read: %q", got[1])
	}
	if !strings.Contains(got[2], msgReceipt) || strings.Contains(got[2], "THREE") {
		t.Fatalf("write_file should mark the file fully seen: %q", got[2])
	}
}

func TestReceiptDoesNotCrossTurns(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("purplefinch body\n"), 0644); err != nil {
		t.Fatal(err)
	}
	k, _, now := freshBot(t)
	k.Budget = compact.Budget{KeepTail: 1, CompactAt: 1 << 30}
	k.Compactor = &recCompactor{}
	runTurn(t, k, now, "look",
		toolCall("r1", "read_file", mustArgs(t, map[string]string{"path": path})),
		provider.Message{Role: provider.RoleAssistant, Content: "seen"},
	)
	n, err := k.Bot.Count()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := k.CompactRange(context.Background(), 1, n-1, "x"); err != nil {
		t.Fatalf("compact: %v", err)
	}
	runTurn(t, k, now, "what is in the file?",
		toolCall("r2", "read_file", mustArgs(t, map[string]string{"path": path})),
		provider.Message{Role: provider.RoleAssistant, Content: "?"},
	)
	got := bodies(t, k)
	last := got[len(got)-1]
	if strings.Contains(last, msgReceipt) || !strings.Contains(last, "purplefinch") {
		t.Fatalf("later turn returned a receipt for a compacted read: %q", last)
	}
}

func TestDenialClearsOnNextOwnerTurn(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("x\n"), 0644); err != nil {
		t.Fatal(err)
	}
	k, _, now := freshBot(t)
	k.Links = []link.Link{link.Terminal{}, link.NewToolsLinkAt(dir, "")}
	k.WorkDir = dir
	asked := 0
	answer := false
	k.Approve = func(context.Context, string, string) (bool, error) {
		asked++
		return answer, nil
	}
	call := `{"command":"rm -rf note.txt","timeout_seconds":1}`
	runTurn(t, k, now, "delete",
		toolCall("s1", "shell", call),
		provider.Message{Role: provider.RoleAssistant, Content: "denied"},
	)
	answer = true
	runTurn(t, k, now, "ok I changed my mind, go ahead",
		toolCall("s2", "shell", call),
		provider.Message{Role: provider.RoleAssistant, Content: "?"},
	)
	got := bodies(t, k)
	if asked < 2 {
		t.Fatalf("owner changed their mind but the call was refused without asking (%d): %q", asked, got)
	}
	if len(got) < 2 || strings.Contains(got[len(got)-1], msgDenied) {
		t.Fatalf("later turn reused the denial: %q", got)
	}
}

func TestSensitiveFileChangeOmitsDiff(t *testing.T) {
	const secret = "supersecret-value"
	names := []string{".env", ".env.local", "prod.env", "api.key", "cert.pem", "credentials", "credentials.json", "id_rsa", "id_ed25519.pub", ".netrc"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, name)
			if err := os.WriteFile(path, []byte("TOKEN="+secret+"-old\n"), 0600); err != nil {
				t.Fatal(err)
			}
			k, p, now := freshBot(t)
			runTurn(t, k, now, "look",
				toolCall("r1", "read_file", mustArgs(t, map[string]string{"path": path})),
				provider.Message{Role: provider.RoleAssistant, Content: "saw"},
			)
			if err := os.WriteFile(path, []byte("TOKEN="+secret+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			advance(now, fileChangeCooldown+time.Second)
			runTurn(t, k, now, "again",
				provider.Message{Role: provider.RoleAssistant, Content: "noted"},
			)
			last := p.reqs[len(p.reqs)-1].Messages
			tip := last[len(last)-1].Content
			if !strings.Contains(tip, "<file_change>") || !strings.Contains(tip, "changed") {
				t.Fatalf("missing change note: %q", tip)
			}
			if strings.Contains(tip, secret) || strings.Contains(tip, "TOKEN") || strings.Contains(tip, "\n  +") || strings.Contains(tip, "\n  -") || strings.Contains(tip, "size ") {
				t.Fatalf("sensitive diff leaked: %q", tip)
			}
		})
	}
}

func TestSameSizeAndMtimeSkipsContentHash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(path, []byte("alpha\n"), 0644); err != nil {
		t.Fatal(err)
	}
	k, _, now := freshBot(t)
	runTurn(t, k, now, "look",
		toolCall("r1", "read_file", mustArgs(t, map[string]string{"path": path})),
		provider.Message{Role: provider.RoleAssistant, Content: "saw"},
	)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("BETA!\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	advance(now, time.Second)
	runTurn(t, k, now, "overwrite",
		toolCall("w1", "write_file", mustArgs(t, map[string]string{"path": path, "content": "gamma\n"})),
		provider.Message{Role: provider.RoleAssistant, Content: "done"},
	)
	got := bodies(t, k)
	last := got[len(got)-1]
	if strings.Contains(last, msgStale) || !strings.Contains(last, msgRefresh) {
		t.Fatalf("same size and mtime should skip the hash and allow the write: %q", last)
	}
}

func TestSnapshotSmallFileAlloc(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.txt")
	if err := os.WriteFile(path, []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}
	res := testing.Benchmark(func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := snapshotFile(path); err != nil {
				b.Fatal(err)
			}
		}
	})
	if res.AllocedBytesPerOp() > 64<<10 {
		t.Fatalf("snapshot of 6 bytes allocated %d bytes/op", res.AllocedBytesPerOp())
	}
}

func BenchmarkSnapshotSmall(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "s.txt")
	if err := os.WriteFile(path, []byte("hello\n"), 0644); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := snapshotFile(path); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFileFreshGuard(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "n.txt")
	if err := os.WriteFile(path, []byte("hello\n"), 0644); err != nil {
		b.Fatal(err)
	}
	k := &Kernel{}
	snap, err := snapshotFile(path)
	if err != nil {
		b.Fatal(err)
	}
	k.storeFile(path, snap, 1, true, path, nil)
	tool := &tools.WriteFile{}
	args := json.RawMessage(`{"path":"` + path + `","content":"hello\n"}`)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := k.rejectUnreadOrStale(tool, args); err != nil {
			b.Fatal(err)
		}
	}
}
