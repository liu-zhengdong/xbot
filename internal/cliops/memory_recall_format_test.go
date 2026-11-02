package cliops

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMemoryRecallRuntimeExactFormatAndFullSemanticText(t *testing.T) {
	for _, tc := range []struct {
		reply MemoryRecallReply
		want  string
	}{
		{MemoryRecallReply{Text: "no matches"}, "no matches\n"},
		{MemoryRecallReply{Text: "- bot:notes/a.md:3: 界\n(7 more; narrow the query or use since=)\n(search incomplete: file/byte limit reached)"}, "- bot:notes/a.md:3: 界\n(7 more; narrow the query or use since=)\n(search incomplete: file/byte limit reached)\n"},
		{MemoryRecallReply{Text: "original trailing newline\n"}, "original trailing newline\n\n"},
		{MemoryRecallReply{File: "/current/results/recall.json", Access: "local", Bytes: 36712}, "/current/results/recall.json (access: local, bytes: 36712)\n"},
		{MemoryRecallReply{Text: "actual", File: "file", Access: "local", Bytes: 17, Error: "late failure"}, "actual\nfile (access: local, bytes: 17)\nerror: late failure\n"},
	} {
		if got := FormatMemoryRecall(tc.reply); got != tc.want {
			t.Fatalf("format=%q want=%q", got, tc.want)
		}
	}
	// Recall has no experience-search 6 KiB semantic cap.
	text := strings.Repeat("- bot:notes.md:1: "+strings.Repeat("界", 200)+"…\n", 20)
	value := MemoryRecallReply{Text: text}
	got, err := boundMemoryRecallReply(value)
	if len(text) <= 6<<10 || err != nil || got != value || FormatMemoryRecall(got) != text+"\n" {
		t.Fatalf("full recall bytes=%d retained=%d err=%v", len(text), len(got.Text), err)
	}
}

func TestMemoryRecallRuntimeDualOutputBudgetsPreserveFacts(t *testing.T) {
	actual := MemoryRecallReply{Text: "actual text", File: "/current/results/recall.json", Access: "local", Bytes: 9223372036854775807, Error: strings.Repeat("\x00<&>", 12<<10)}
	got, err := boundMemoryRecallReply(actual)
	wire, marshalErr := json.Marshal(got)
	actual.Error = got.Error
	if err == nil || marshalErr != nil || got.Error == "" || got != actual || len(wire)+1 > memoryRecallOutputBudget || len(FormatMemoryRecall(got)) > memoryRecallOutputBudget {
		t.Fatalf("facts=%+v bytes=%d err=%v", got, len(wire), err)
	}
	for _, text := range []string{strings.Repeat("\x00", 6<<10), strings.Repeat("x", memoryRecallOutputBudget+1)} {
		actual := MemoryRecallReply{Text: text}
		got, err := boundMemoryRecallReply(actual)
		if err == nil || got.Text != text || got.File != "" {
			t.Fatal("complete semantic text clipped or invented artifact")
		}
	}
	for _, n := range []int{memoryRecallOutputBudget - len(`{"text":""}`) - 1, memoryRecallOutputBudget - len(`{"text":""}`)} {
		value := MemoryRecallReply{Text: strings.Repeat("x", n)}
		wire, _ := json.Marshal(value)
		_, err := boundMemoryRecallReply(value)
		if (err == nil) != (len(wire)+1 <= memoryRecallOutputBudget) {
			t.Fatalf("JSONNL bytes=%d err=%v", len(wire)+1, err)
		}
	}
	// Human metadata can exceed the budget even while compact JSON fits.
	value := MemoryRecallReply{File: strings.Repeat("f", memoryRecallOutputBudget-46), Access: "local", Bytes: 1}
	wire, _ = json.Marshal(value)
	if len(wire)+1 > memoryRecallOutputBudget || len(FormatMemoryRecall(value)) > memoryRecallOutputBudget {
		t.Fatal("invalid metadata fixture")
	}
	got, err = boundMemoryRecallReply(value)
	if err != nil || got != value {
		t.Fatal("valid metadata changed")
	}
}
