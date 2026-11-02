package cliops

import (
	"reflect"
	"strings"
	"testing"
)

func TestMemoryRecallRuntimeParserGrammarAndLiteralArity(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want MemoryRecallCommand
	}{
		{[]string{"--self"}, MemoryRecallCommand{Self: true}},
		{[]string{"--self", "--query="}, MemoryRecallCommand{Self: true}},
		{[]string{"--self", "--query", "--help", "--scope", "--", "--since= 2026-11-02 ", "--json"}, MemoryRecallCommand{Query: "--help", Scope: "--", Since: " 2026-11-02 ", Self: true, JSON: true}},
		{[]string{"--query-file=-", "--self", "--scope= BOT ", "--since=bad"}, MemoryRecallCommand{QueryFile: "-", Scope: " BOT ", Since: "bad", Self: true}},
		{[]string{"--help"}, MemoryRecallCommand{Help: true}},
		{[]string{"--query-file", "absent", "--help"}, MemoryRecallCommand{QueryFile: "absent", Help: true}},
		{[]string{"--scope", "--help", "--self"}, MemoryRecallCommand{Scope: "--help", Self: true}},
		{[]string{"--since", "--", "--self"}, MemoryRecallCommand{Since: "--", Self: true}},
		{[]string{"--self", "--query", strings.Repeat("q", 7000)}, MemoryRecallCommand{Self: true, Query: strings.Repeat("q", 7000)}},
	} {
		got, err := ParseMemoryRecallRuntime(tc.args)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("args=%q got=%+v want=%+v err=%v", tc.args, got, tc.want, err)
		}
	}
	for _, args := range [][]string{
		nil, {"help"}, {"-h"}, {"--"}, {"--self", "query"}, {"--self", "-"}, {"memory-recall", "--self"},
		{"--query=x"}, {"--self=true"}, {"--json=true", "--self"}, {"--help=true"},
		{"--self", "--self"}, {"--self", "--json", "--json"}, {"--help", "--help"},
		{"--self", "--query=a", "--query=b"}, {"--self", "--query-file=a", "--query-file=b"},
		{"--self", "--scope=a", "--scope=b"}, {"--self", "--since=a", "--since=b"},
		{"--self", "--query"}, {"--self", "--query-file"}, {"--self", "--scope"}, {"--self", "--since"},
		{"--self", "--query-file="}, {"--self", "--query=", "--query-file=-"},
		{"--self", "--file=a"}, {"--self", "--person=a"}, {"--self", "--limit=20"},
		{"--help", "--unknown"}, {"--help", "positional"}, {"--help", "--query-file="},
	} {
		if got, err := ParseMemoryRecallRuntime(args); err == nil {
			t.Fatalf("invalid args=%q accepted=%+v", args, got)
		}
	}
}
