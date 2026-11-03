package cliops

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTextResultExactFormat(t *testing.T) {
	for _, tc := range []struct {
		reply TextResult
		want  string
	}{
		{TextResult{}, ""},
		{TextResult{Text: "no matches"}, "no matches\n"},
		{TextResult{Text: " raw\ntext \n"}, " raw\ntext \n\n"},
		{TextResult{File: "results/actual.json", Access: "local", Bytes: 36712}, "results/actual.json (access: local, bytes: 36712)\n"},
		{TextResult{Text: "actual", File: "file", Access: "local", Bytes: 17, Error: "late failure"}, "actual\nfile (access: local, bytes: 17)\nerror: late failure\n"},
	} {
		if got := FormatTextResult(tc.reply); got != tc.want {
			t.Fatalf("format=%q want=%q", got, tc.want)
		}
	}
	// All three protocol names are the same type, including JSON field order.
	var experience ExperienceSearchReply = TextResult{Text: "semantic", File: "file", Access: "local", Bytes: 17, Error: "late failure"}
	var recall MemoryRecallReply = experience
	var transcript TranscriptReadReply = recall
	raw, err := json.Marshal(transcript)
	if err != nil || string(raw) != `{"text":"semantic","file":"file","access":"local","bytes":17,"error":"late failure"}` {
		t.Fatalf("wire=%s err=%v", raw, err)
	}
}

func TestTextResultDualBudgetsPreserveFacts(t *testing.T) {
	const diagnostic = "test: source must provide a complete result artifact"
	actual := TextResult{Text: "actual text", File: "/current/results/recall.json", Access: "local", Bytes: 9223372036854775807, Error: strings.Repeat("\x00<&>", 12<<10)}
	got, err := boundTextResult(actual, diagnostic)
	actual.Error = diagnostic
	if err == nil || got != actual || !FitsOutput(got, FormatTextResult(got)) {
		t.Fatalf("display error changed facts: %+v err=%v", got, err)
	}
	// Keep the full recall projection; do not inherit experience's 6 KiB cap.
	text := strings.Repeat("- bot:notes.md:1: "+strings.Repeat("界", 200)+"…\n", 20)
	full := TextResult{Text: text}
	if got, err := boundTextResult(full, diagnostic); len(text) <= 6<<10 || err != nil || got != full {
		t.Fatalf("complete semantic text bytes=%d err=%v", len(text), err)
	}
	one, _ := json.Marshal(TextResult{Text: "x"})
	exact := TextResult{Text: strings.Repeat("x", TextOutputBudget-len(one))}
	if got, err := boundTextResult(exact, diagnostic); err != nil || got != exact {
		t.Fatalf("exact JSONNL boundary rejected: %v", err)
	}
	for _, text := range []string{exact.Text + "x", strings.Repeat("\x00", 6<<10), strings.Repeat("<", TextOutputBudget/6), strings.Repeat("x", TextOutputBudget)} {
		full := TextResult{Text: text}
		got, err := boundTextResult(full, diagnostic)
		if err == nil || got.Text != text || got.File != "" || got.Error != diagnostic {
			t.Fatalf("oversize text was clipped or accepted: bytes=%d err=%v", len(text), err)
		}
	}
	// Either format may exhaust its own budget. Human includes a final newline.
	if FitsOutput(struct{}{}, strings.Repeat("h", TextOutputBudget+1)) {
		t.Fatal("human overflow accepted while JSON fits")
	}
	metadata := TextResult{File: strings.Repeat("f", TextOutputBudget-46), Access: "local", Bytes: 1}
	if got, err := boundTextResult(metadata, diagnostic); err != nil || got != metadata {
		t.Fatal("valid near-limit metadata changed")
	}
}
