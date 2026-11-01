package cliops

import (
	"reflect"
	"strings"
	"testing"
)

func TestPeerMessageRuntimeGrammar(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want PeerMessageCommand
	}{
		{[]string{"B", "--self", "--text", "hello"}, PeerMessageCommand{Bot: "B", Self: true, Text: "hello"}},
		{[]string{"--self", "--json", "--file=-", "B@Laptop"}, PeerMessageCommand{Bot: "B@Laptop", Self: true, JSON: true, TextFile: "-"}},
		{[]string{"B", "--self", "--text=--help"}, PeerMessageCommand{Bot: "B", Self: true, Text: "--help"}},
		{[]string{"B", "--self", "--text", "--help"}, PeerMessageCommand{Bot: "B", Self: true, Text: "--help"}},
		{[]string{"B", "--text", "--self", "--self"}, PeerMessageCommand{Bot: "B", Self: true, Text: "--self"}},
		{[]string{"B", "--self", "--file", "--help"}, PeerMessageCommand{Bot: "B", Self: true, TextFile: "--help"}},
		{[]string{"--self", "--text=", "--", "--help"}, PeerMessageCommand{Bot: "--help", Self: true}},
		{[]string{"--self", "--text=x", "--", "--self"}, PeerMessageCommand{Bot: "--self", Self: true, Text: "x"}},
		{[]string{"--help"}, PeerMessageCommand{Help: true}},
		{[]string{"-h"}, PeerMessageCommand{Help: true}},
		{[]string{"B", "--file", "/does/not/exist", "--help"}, PeerMessageCommand{Bot: "B", TextFile: "/does/not/exist", Help: true}},
		// Parsing does not trim or enforce the writer's target/text limits.
		{[]string{" \nB@unknown-device ", "--self", "--text", " \n" + strings.Repeat("x", 8193)}, PeerMessageCommand{Bot: " \nB@unknown-device ", Self: true, Text: " \n" + strings.Repeat("x", 8193)}},
	} {
		got, err := ParsePeerMessageRuntime(tc.args)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("args=%q got=%+v want=%+v err=%v", tc.args, got, tc.want, err)
		}
	}
}

func TestPeerMessageRuntimeGrammarRejects(t *testing.T) {
	for _, args := range [][]string{
		nil, {"B"}, {"B", "--self"}, {"--self", "--text=x"},
		{"B", "C", "--self", "--text=x"}, {"", "--self", "--text=x"},
		{"B", "--text=x"}, {"B", "--self", "--text"}, {"B", "--self", "--file"},
		{"B", "--self", "--file="}, {"B", "--self", "--file", ""},
		{"B", "--self", "--text=x", "--file=-"},
		{"B", "--self", "--text=x", "--text=y"}, {"B", "--self", "--file=-", "--file=-"},
		{"B", "--self", "--self", "--text=x"}, {"B", "--self", "--text=x", "--json", "--json"},
		{"--help", "-h"}, {"--help", "--help"},
		{"B", "--self=true", "--text=x"}, {"B", "--self", "--text=x", "--json=false"}, {"--help=yes"},
		{"B", "--self", "--text=x", "--unknown"}, {"--help", "--unknown"},
		{"send", "B", "--self", "--text=x"}, {"message", "B", "--self", "--text=x"}, {"remote-send", "B", "--self", "--text=x"},
		{"B", "--self", "--text=x", "--", "--json"}, {"--", "B", "--self", "--text=x"},
		// A value that looks like a flag is consumed; it cannot supply --self/help.
		{"B", "--text", "--self"}, {"B", "--text", "--help"}, {"B", "--file", "--help"},
		{"B", "--self", "--text=x", "--help", "C"}, {"--help", "--text"},
	} {
		if got, err := ParsePeerMessageRuntime(args); err == nil {
			t.Fatalf("accepted args=%q command=%+v", args, got)
		}
	}
}
