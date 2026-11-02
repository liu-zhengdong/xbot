package cliops

import (
	"strconv"
	"strings"
	"testing"
)

func TestTranscriptParserArityAndActionFlags(t *testing.T) {
	command, err := ParseTranscriptRuntime([]string{"search", "--self", "--query", "--help", "--bot", "--"})
	if err != nil || command.Help || command.Query != "--help" || command.Bot != "--" {
		t.Fatalf("literal values changed: %+v err=%v", command, err)
	}
	for _, args := range [][]string{{"search", "--self"}, {"search", "--self", "--query="}} {
		got, err := ParseTranscriptRuntime(args)
		if err != nil || got.Query != "" {
			t.Fatalf("optional empty query: %+v err=%v", got, err)
		}
	}
	for _, args := range [][]string{
		{"search", "--self", "--query"},
		{"search", "--self", "--query=x", "--query", "y"},
		{"search", "--self", "--query=", "--query-file=-"},
		{"search", "--self", "--unknown"},
		{"search", "--self", "positional"},
		{"search", "--self=true"},
		{"detail", "--self", "--seq=1", "--bot=other"},
		{"expand", "--self", "--from=1"},
		{"detail", "--self"},
		{"help"}, {"search", "-h"},
	} {
		if _, err := ParseTranscriptRuntime(args); err == nil {
			t.Fatalf("invalid invocation accepted: %q", args)
		}
	}
}

func TestTranscriptParserPreservesDomainIntegers(t *testing.T) {
	expand, err := ParseTranscriptRuntime([]string{"expand", "--self", "--from=-3", "--to=0", "--bot= target "})
	if err != nil || expand.From != -3 || expand.To != 0 || expand.Bot != " target " {
		t.Fatalf("range validated before domain: %+v err=%v", expand, err)
	}
	for _, experience := range []string{"-1", "0", "9223372036854775807"} {
		detail, err := ParseTranscriptRuntime([]string{"detail", "--self", "--seq=0", "--experience=" + experience})
		want, _ := strconv.ParseInt(experience, 10, 64)
		if err != nil || detail.Seq != 0 || detail.Experience != want {
			t.Fatalf("detail changed: %+v err=%v", detail, err)
		}
	}
	for _, flag := range []string{"--seq", "--experience"} {
		args := []string{"detail", "--self", flag + "=" + strings.Repeat("9", 30)}
		if _, err := ParseTranscriptRuntime(args); err == nil {
			t.Fatalf("overflow accepted: %s", flag)
		}
	}
	if _, err := ParseTranscriptRuntime([]string{"expand", "--self", "--from=1.5", "--to=2"}); err == nil {
		t.Fatal("noninteger range accepted")
	}
}
