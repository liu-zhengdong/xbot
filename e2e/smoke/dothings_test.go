//go:build smoke

package smoke

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestDoThings is the smoke case for 「动手做事」 (journey and spec in
// docs/guard/六条基本旅程.md). The foreground dispatches a dev subagent; the subagent
// lists a directory with shell and writes summary.txt; the owner reads the
// report and the file on disk.
//
// Not covered: whether the reply explains the work well. That is wording, for
// a person to judge.
func TestDoThings(t *testing.T) {
	stub := StartStub(t)
	srv := Start(t, stub)
	srv.CreateBot("ada", "You are ada, the bot in the do-things smoke test.")

	// The directory lives under the data root and is named by absolute path,
	// so the task does not depend on the bot's default working directory.
	dir := filepath.Join(srv.Root, "scratch")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("make %s: %v", dir, err)
	}
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name+"\n"), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	// The reference is taken before anything is dispatched: the subagent
	// writes into this same directory, and listing it later would pick up
	// summary.txt or an atomic-write temp file.
	summary := filepath.Join(dir, "summary.txt")
	wantContent := "files:\n" + lsOutput(t, dir)

	task := srv.Send("ada", "SMOKE_DO dir="+dir)
	if task.Done != "交给子代理了" {
		t.Fatalf("SMOKE_DO: done.text %q, want %q", task.Done, "交给子代理了")
	}

	// The file appears within 60 seconds with exactly `files:` plus the
	// listing captured above.
	WaitFor(t, 60*time.Second, "summary.txt on disk", func() (bool, string) {
		info, err := os.Stat(summary)
		if err != nil {
			return false, err.Error()
		}
		return info.Mode().IsRegular(), "not a regular file"
	})
	got, err := os.ReadFile(summary)
	if err != nil {
		t.Fatalf("read summary.txt: %v", err)
	}
	if string(got) != wantContent {
		t.Fatalf("summary.txt is not `files:` plus the real ls -1 output (旅程「动手做事」: 内容和要求一致)\nwant %q\ngot  %q",
			wantContent, got)
	}

	// The final reply names the full path, and reads like an answer rather
	// than tool output.
	var report string
	WaitFor(t, 60*time.Second, "the report in the transcript", func() (bool, string) {
		rows := srv.Transcript("ada")
		report = ""
		for _, r := range rows {
			if r.Role == "assistant" && strings.Contains(r.Content, summary) {
				report = r.Content
			}
		}
		return report != "", dumpRows(rows)
	})
	if !strings.Contains(report, dir) || !strings.Contains(report, "summary.txt") {
		t.Fatalf("the final reply does not name the path and summary.txt (旅程「动手做事」: 说清做了什么、文件在哪): %q", report)
	}

	forbidden := []string{`{"command"`, `"path":`, "<untrusted_event", "tool_calls"}
	checkClean := func(where, text string) {
		for _, f := range forbidden {
			if strings.Contains(text, f) {
				t.Fatalf("%s carries tool output %q (旅程「动手做事」: 对话里不夹工具调用的原文):\n%s", where, f, text)
			}
		}
	}
	checkClean("done.text", task.Done)
	var streamed strings.Builder
	for _, d := range task.Deltas {
		streamed.WriteString(d.Text)
	}
	checkClean("streamed deltas joined", streamed.String())
	rows := srv.Transcript("ada")
	for _, r := range rows {
		if r.Role == "assistant" {
			checkClean("assistant transcript row", r.Content)
		}
	}

	var lastAssistant string
	for _, r := range rows {
		if r.Role == "assistant" && strings.TrimSpace(r.Content) != "" {
			lastAssistant = r.Content
		}
	}
	if lastAssistant != report {
		t.Fatalf("the final reply is not the report:\nlast:   %q\nreport: %q", lastAssistant, report)
	}

	// The trace: this turn dispatched work, and a subagent result carried the
	// summary.txt path back.
	dispatched := false
	for _, r := range rows {
		for _, tc := range r.ToolCalls {
			if tc.Function.Name == "dispatch" {
				dispatched = true
			}
		}
	}
	if !dispatched {
		t.Fatalf("no assistant row calls dispatch (旅程「动手做事」: 轨迹里能看到这一轮派了活):\n%s", dumpRows(rows))
	}
	returned := false
	for _, r := range rows {
		if r.Role == "user" && strings.Contains(r.Content, "<untrusted_event") && strings.Contains(r.Content, summary) {
			returned = true
		}
	}
	if !returned {
		t.Fatalf("no subagent result row carries the summary.txt path (旅程「动手做事」: 子代理产出了文件):\n%s", dumpRows(rows))
	}
}

// lsOutput is what ls -1 prints for dir here, the reference for the file.
func lsOutput(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("ls", "-1", dir).Output()
	if err != nil {
		t.Fatalf("ls -1 %s: %v", dir, err)
	}
	return string(out)
}
