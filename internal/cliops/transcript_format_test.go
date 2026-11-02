package cliops

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTranscriptFormatterExactNewlineAndDualBudgets(t *testing.T) {
	reply := TranscriptReadReply{Text: " raw\ntext ", File: "results/actual.json", Access: "local", Bytes: 40123, Error: "late read failure"}
	want := " raw\ntext \nresults/actual.json (access: local, bytes: 40123)\nerror: late read failure\n"
	if got := FormatTranscriptRead(reply); got != want {
		t.Fatalf("facts changed: %q", got)
	}
	if got, err := boundTranscriptReadReply(reply); err != nil || got != reply {
		t.Fatalf("valid facts changed: %+v err=%v", got, err)
	}
	// An exact JSON-with-newline boundary fits; the next byte must fail even
	// though its human text still fits. Escaping can independently exhaust JSON.
	// Empty Text is omitted, so measure overhead using one nonempty byte.
	one, _ := json.Marshal(TranscriptReadReply{Text: "x"})
	exact := TranscriptReadReply{Text: strings.Repeat("x", transcriptOutputBudget-len(one))}
	if got, err := boundTranscriptReadReply(exact); err != nil || got != exact {
		t.Fatalf("exact JSONNL boundary rejected: err=%v", err)
	}
	for _, text := range []string{exact.Text + "x", strings.Repeat("<", transcriptOutputBudget/6), strings.Repeat("x", transcriptOutputBudget)} {
		actual := reply
		actual.Text = text
		got, err := boundTranscriptReadReply(actual)
		if err == nil || got.Text != text || got.File != actual.File || got.Access != actual.Access || got.Bytes != actual.Bytes || got.Error == "" {
			t.Fatalf("oversize facts truncated or accepted: text=%d reply=%+v err=%v", len(text), got, err)
		}
	}
}
