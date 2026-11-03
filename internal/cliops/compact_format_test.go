package cliops

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCompactFormatterAcknowledgementAndDualErrorBudgets(t *testing.T) {
	for _, tc := range []struct {
		reply CompactReply
		want  string
	}{
		{CompactReply{}, ""},
		{CompactReply{Text: "compacted 1-12"}, "compacted 1-12\n"},
		{CompactReply{Text: "original\n"}, "original\n\n"},
		{CompactReply{Error: "state save failed"}, "error: state save failed\n"},
		// Synthetic mixed facts exercise formatting, not CompactRange late-error semantics.
		{CompactReply{Text: "supplied fact", Error: "supplied error"}, "supplied fact\nerror: supplied error\n"},
	} {
		if got := FormatCompact(tc.reply); got != tc.want {
			t.Fatalf("format=%q want=%q", got, tc.want)
		}
		got, err := boundCompactReply(tc.reply)
		if err != nil || got != tc.reply {
			t.Fatalf("bounded small reply=%+v err=%v", got, err)
		}
	}
	for _, message := range []string{
		strings.Repeat("x", compactOutputBudget+1),
		strings.Repeat("\x00<&>\"\\", 8<<10),
		strings.Repeat("界", compactOutputBudget),
	} {
		actual := CompactReply{Error: "actual state save failure: " + message}
		got, err := boundCompactReply(actual)
		wire, marshalErr := json.Marshal(got)
		if err != nil || marshalErr != nil || got.Text != "" || got.Error == "" || got.Error == actual.Error || !strings.HasPrefix(got.Error, "actual state save failure: ") || !strings.Contains(got.Error, "[truncated]") || !utf8.ValidString(got.Error) || len(wire)+1 > compactOutputBudget || len(FormatCompact(got)) > compactOutputBudget {
			t.Fatalf("error clipping text bytes=%d error bytes=%d JSONNL=%d err=%v marshal=%v", len(got.Text), len(got.Error), len(wire)+1, err, marshalErr)
		}
	}
	for _, n := range []int{compactOutputBudget - len(`{"text":""}`) - 1, compactOutputBudget - len(`{"text":""}`)} {
		actual := CompactReply{Text: strings.Repeat("x", n)}
		wire, err := json.Marshal(actual)
		if err != nil {
			t.Fatal(err)
		}
		got, err := boundCompactReply(actual)
		if got.Text != actual.Text || (err == nil) != (len(wire)+1 <= compactOutputBudget) {
			t.Fatalf("JSONNL=%d text bytes=%d err=%v", len(wire)+1, len(got.Text), err)
		}
	}
	for _, text := range []string{strings.Repeat("x", compactOutputBudget+1), strings.Repeat("\x00", 6<<10)} {
		for _, message := range []string{"", strings.Repeat("error", 16<<10)} {
			got, err := boundCompactReply(CompactReply{Text: text, Error: message})
			if err == nil || got.Text != text {
				t.Fatalf("oversized original text changed bytes=%d err=%v", len(got.Text), err)
			}
		}
	}
	// A large display error may shrink while independent supplied text stays exact.
	actual := CompactReply{Text: "supplied fact\n", Error: strings.Repeat("\x00", 8<<10)}
	got, err := boundCompactReply(actual)
	wire, marshalErr := json.Marshal(got)
	if err != nil || marshalErr != nil || got.Text != actual.Text || got.Error == "" || len(wire)+1 > compactOutputBudget || len(FormatCompact(got)) > compactOutputBudget {
		t.Fatalf("facts changed or dual budget exceeded JSONNL=%d err=%v", len(wire)+1, err)
	}
}
