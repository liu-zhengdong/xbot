package cliops

import (
	"strconv"
	"strings"
	"testing"
)

func TestCompactParserLiteralArityAndDomainIntegers(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	for _, tc := range []struct {
		args []string
		want CompactCommand
	}{
		{[]string{"--help"}, CompactCommand{Help: true}},
		{[]string{"--self", "--help"}, CompactCommand{Self: true, Help: true}},
		{[]string{"--focus-file", "absent", "--help"}, CompactCommand{FocusFile: "absent", Help: true}},
		{[]string{"--self", "--from", "1", "--to=2"}, CompactCommand{Self: true, From: 1, To: 2}},
		{[]string{"--from=0", "--to", "-7", "--focus", "--help", "--self", "--json"}, CompactCommand{From: 0, To: -7, Focus: "--help", Self: true, JSON: true}},
		{[]string{"--self", "--from=+001", "--to=-0", "--focus", "--"}, CompactCommand{Self: true, From: 1, Focus: "--"}},
		{[]string{"--self", "--from=9", "--to=1", "--focus="}, CompactCommand{Self: true, From: 9, To: 1}},
		{[]string{"--self", "--from=1", "--to=2", "--focus-file", "--help"}, CompactCommand{Self: true, From: 1, To: 2, FocusFile: "--help"}},
		{[]string{"--self", "--from=1", "--to=2", "--focus-file=-"}, CompactCommand{Self: true, From: 1, To: 2, FocusFile: "-"}},
		{[]string{"--self", "--from=" + strconv.Itoa(-maxInt-1), "--to=" + strconv.Itoa(maxInt)}, CompactCommand{Self: true, From: -maxInt - 1, To: maxInt}},
		{[]string{"--self", "--from=1", "--to=2", "--focus", " \n" + strings.Repeat("界", 3000) + "\t "}, CompactCommand{Self: true, From: 1, To: 2, Focus: " \n" + strings.Repeat("界", 3000) + "\t "}},
	} {
		got, err := ParseCompactRuntime(tc.args)
		if err != nil || got != tc.want {
			t.Fatalf("args=%q got=%+v want=%+v err=%v", tc.args, got, tc.want, err)
		}
	}
	for _, args := range [][]string{
		nil, {"help"}, {"-h"}, {"--"}, {"compact", "--help"}, {"run", "--help"},
		{"--self"}, {"--from=1", "--to=2"}, {"--self", "--from=1"}, {"--self", "--to=2"},
		{"--self=true"}, {"--help=false"}, {"--json=true", "--help"},
		{"--help", "--self", "--self"}, {"--help", "--help"}, {"--help", "--json", "--json"},
		{"--help", "--from=1", "--from=2"}, {"--help", "--to=1", "--to=2"},
		{"--help", "--focus=a", "--focus=b"}, {"--help", "--focus-file=a", "--focus-file=b"},
		{"--help", "--from"}, {"--help", "--to"}, {"--help", "--focus"}, {"--help", "--focus-file"},
		{"--help", "--focus=", "--focus-file=-"}, {"--help", "--focus-file="},
		{"--self", "--from", "--help", "--to=2"}, {"--self", "--from=1", "--to", "--"},
		{"--help", "--from=1.5"}, {"--help", "--from=0x10"}, {"--help", "--from= 1"}, {"--help", "--to="},
		{"--help", "--from=" + strconv.Itoa(maxInt) + "0"}, {"--help", "--to=" + strconv.Itoa(-maxInt-1) + "0"},
		{"--help", "--bot=other"}, {"--help", "--file=x"}, {"--help", "--person=x"}, {"--help", "--call-id=x"},
		{"--help", "--unknown"}, {"--help", "positional"}, {"--help", "-"},
	} {
		if got, err := ParseCompactRuntime(args); err == nil {
			t.Fatalf("invalid args=%q accepted=%+v", args, got)
		}
	}
}
