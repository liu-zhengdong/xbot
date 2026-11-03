package cliops

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestLinksFormatterTextErrorAndDualErrorBudgets(t *testing.T) {
	for _, tc := range []struct {
		reply LinksReply
		want  string
	}{
		{LinksReply{}, ""},
		{LinksReply{Text: "browser\tavailable\tLocal Chrome"}, "browser\tavailable\tLocal Chrome\n"},
		{LinksReply{Text: "loaded browser: browser_open\n"}, "loaded browser: browser_open\n\n"},
		{LinksReply{Error: "not installed"}, "error: not installed\n"},
		// Synthetic mixed facts exercise formatting, not late-error semantics.
		{LinksReply{Text: "partial facts", Error: "late failure"}, "partial facts\nerror: late failure\n"},
	} {
		if got := FormatLinks(tc.reply); got != tc.want {
			t.Fatalf("format=%q want=%q", got, tc.want)
		}
		got, err := boundLinksReply(tc.reply)
		if err != nil || got != tc.reply {
			t.Fatalf("bounded small reply=%+v err=%v", got, err)
		}
	}
	for _, message := range []string{
		strings.Repeat("x", linksOutputBudget+1),
		strings.Repeat("\x00<&>\"\\", 8<<10),
		strings.Repeat("界", linksOutputBudget),
	} {
		actual := LinksReply{Error: "actual load failure: " + message}
		got, err := boundLinksReply(actual)
		wire, marshalErr := json.Marshal(got)
		if err != nil || marshalErr != nil || got.Text != "" || got.Error == "" || got.Error == actual.Error || !strings.HasPrefix(got.Error, "actual load failure: ") || !strings.Contains(got.Error, "[truncated]") || !utf8.ValidString(got.Error) || len(wire)+1 > linksOutputBudget || len(FormatLinks(got)) > linksOutputBudget {
			t.Fatalf("error clipping text bytes=%d error bytes=%d JSONNL=%d err=%v marshal=%v", len(got.Text), len(got.Error), len(wire)+1, err, marshalErr)
		}
	}
	// Text is never clipped by the error-only binder.
	for _, n := range []int{linksOutputBudget - len(`{"text":""}`) - 1, linksOutputBudget - len(`{"text":""}`)} {
		actual := LinksReply{Text: strings.Repeat("x", n)}
		wire, err := json.Marshal(actual)
		if err != nil {
			t.Fatal(err)
		}
		got, err := boundLinksReply(actual)
		if got.Text != actual.Text || (err == nil) != (len(wire)+1 <= linksOutputBudget) {
			t.Fatalf("JSONNL=%d text bytes=%d err=%v", len(wire)+1, len(got.Text), err)
		}
	}
	for _, text := range []string{strings.Repeat("x", linksOutputBudget+1), strings.Repeat("\x00", 6<<10)} {
		for _, message := range []string{"", strings.Repeat("error", 16<<10)} {
			got, err := boundLinksReply(LinksReply{Text: text, Error: message})
			if err == nil || got.Text != text {
				t.Fatalf("oversized original text changed bytes=%d err=%v", len(got.Text), err)
			}
		}
	}
	// A large display error may shrink while independent supplied text stays exact.
	actual := LinksReply{Text: "partial catalog\n", Error: strings.Repeat("\x00", 8<<10)}
	got, err := boundLinksReply(actual)
	wire, marshalErr := json.Marshal(got)
	if err != nil || marshalErr != nil || got.Text != actual.Text || got.Error == "" || len(wire)+1 > linksOutputBudget || len(FormatLinks(got)) > linksOutputBudget {
		t.Fatalf("facts changed or dual budget exceeded JSONNL=%d err=%v", len(wire)+1, err)
	}
}
