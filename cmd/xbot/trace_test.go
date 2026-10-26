package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/provider"
	"xbot/internal/vault"
)

func TestTraceCommands(t *testing.T) {
	t.Setenv("XBOT_VAULT_SOCKET", "")
	root := t.TempDir()
	b, err := bot.Create(root, "Ada", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := bot.SetPrimary(root, b.Name()); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 26, 1, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		if err := b.LogUsage(bot.Trace{Time: at.Add(time.Duration(i) * time.Hour), Prompt: 100 + i}); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.LogTrace(bot.TraceStop, "repetition", at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	rows := []bot.Entry{
		{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("outside-window")}},
		{Message: provider.Message{Role: provider.RoleAssistant, Content: append(provider.ToolContent([]provider.ToolCall{{ID: "c1", Function: provider.FunctionCall{Name: "shell", Arguments: `{"command":"ls"}`}}}), provider.ContentPart{Type: provider.PartReasoning, Text: "PRIVATE-REASONING"})}},
		{Message: provider.Message{Role: provider.RoleTool, Content: provider.ToolResult("c1", strings.Repeat("blob-output ", 2000)+"RESULT-END")}},
	}
	for _, e := range rows {
		if err := b.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	call := func(args ...string) string {
		t.Helper()
		var out bytes.Buffer
		if err := cmdTrace(root, args, &out); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}
	// Exercise the real dispatch and relative --root resolution too.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(wd, root)
	if err != nil {
		t.Fatal(err)
	}
	raw := captureStdout(t, func() error { return run([]string{"--root", relative, "trace", "paths"}) })
	var paths bot.TracePaths
	if err := json.Unmarshal([]byte(raw), &paths); err != nil {
		t.Fatal(err)
	}
	if paths.Trace != b.TracePath() || paths.Transcript != b.TranscriptDir() || paths.Agents != b.AgentsDir() || paths.Bot != bot.Dir(root, "Ada") {
		t.Fatalf("wrong paths: %+v", paths)
	}
	if got := call("paths", "Ada"); got != raw {
		t.Fatal("default did not select primary")
	}
	got := call("list", "Ada", "--since", "2026-10-26T10:00:00+08:00", "--until", "2026-10-26T02:00:00Z", "--kind", "usage")
	var usage bot.Trace
	if err := json.Unmarshal([]byte(got), &usage); err != nil {
		t.Fatal(err)
	}
	if usage.Prompt != 101 {
		t.Fatalf("inclusive time/kind filter: %s", got)
	}
	if got := call("list", "--kind=unknown"); got != "" {
		t.Fatal(got)
	}
	got = call("replay", "--from", "2", "Ada", "--to", "3")
	if !strings.Contains(got, "tool shell") || !strings.Contains(got, "RESULT-END") || strings.Contains(got, "outside-window") || strings.Contains(got, "PRIVATE-REASONING") {
		t.Fatal("replay lost tool/blob/window/public boundary")
	}
	// A value added to the vault after logging must still be redacted on output.
	store, err := vault.Open(b.VaultDir(), b.VaultKeyPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set("test", "RESULT-END", []string{"example.com"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(call("replay"), "RESULT-END") {
		t.Fatal("replay bypassed vault")
	}
	if err := b.LogTrace(bot.TraceStop, "RESULT-END", at); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(call("list"), "RESULT-END") {
		t.Fatal("list bypassed vault")
	}
	for _, args := range [][]string{
		{}, {"bad"}, {"paths", "Ada", "extra"}, {"paths", "missing"}, {"paths", "--kind", "usage"},
		{"list", "--since", "bad"}, {"list", "--until", "bad"}, {"list", "--kind"},
		{"list", "--since", "2026-10-27T00:00:00Z", "--until", "2026-10-26T00:00:00Z"},
		{"replay", "--from", "2"}, {"replay", "--from", "3", "--to", "2"}, {"replay", "--from", "1", "--to", "99"},
	} {
		if err := cmdTrace(root, args, io.Discard); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	empty := filepath.Join(t.TempDir(), "not-created")
	if err := cmdTrace(empty, []string{"paths"}, io.Discard); !errors.Is(err, bot.ErrNoPrimary) {
		t.Fatalf("empty root: %v", err)
	}
	if _, err := os.Stat(empty); !os.IsNotExist(err) {
		t.Fatal("read command created root")
	}
}

func TestListTraceIncompleteAndBounded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace.jsonl")
	var out bytes.Buffer
	read := func() error {
		return listTrace(&out, path, time.Time{}, time.Time{}, "", func(s string) string { return s })
	}
	if err := read(); err != nil || out.Len() != 0 {
		t.Fatalf("missing: %v", err)
	}
	complete := `{"time":"2026-10-26T00:00:00Z","kind":"usage","future_field":42}` + "\n"
	if err := os.WriteFile(path, []byte(complete+`{"kind":`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := read(); err != nil {
		t.Fatal(err)
	}
	if out.String() != complete {
		t.Fatal("partial tail or unknown fields mishandled")
	}
	for _, raw := range []string{"invalid\n", strings.Repeat("x", 1<<20) + "\n"} {
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if err := read(); err == nil {
			t.Fatal("accepted invalid/oversized line")
		}
	}
}
