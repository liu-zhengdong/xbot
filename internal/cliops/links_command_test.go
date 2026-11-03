package cliops

import (
	"strings"
	"testing"
)

func TestLinksParserNounsHelpAndStrictFlags(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want LinksCommand
	}{
		{[]string{"--help"}, LinksCommand{Help: true}},
		{[]string{"search", "--help"}, LinksCommand{Action: "search", Help: true}},
		{[]string{"load", "--help"}, LinksCommand{Action: "load", Help: true}},
		{[]string{"search", "--self"}, LinksCommand{Action: "search", Self: true}},
		{[]string{"search", "--self", "--query", "browser"}, LinksCommand{Action: "search", Query: "browser", Self: true}},
		{[]string{"search", "--query=telegram", "--self", "--json"}, LinksCommand{Action: "search", Query: "telegram", Self: true, JSON: true}},
		{[]string{"search", "--self", "--query", "--help"}, LinksCommand{Action: "search", Query: "--help", Self: true}},
		{[]string{"search", "--self", "--query", " \n" + strings.Repeat("界", 3000) + "\t "}, LinksCommand{Action: "search", Query: " \n" + strings.Repeat("界", 3000) + "\t ", Self: true}},
		{[]string{"search", "--self", "--query-file", "q.txt"}, LinksCommand{Action: "search", QueryFile: "q.txt", Self: true}},
		{[]string{"search", "--self", "--query-file=-"}, LinksCommand{Action: "search", QueryFile: "-", Self: true}},
		{[]string{"search", "--self", "--query-file", "--help"}, LinksCommand{Action: "search", QueryFile: "--help", Self: true}},
		{[]string{"load", "--self", "browser"}, LinksCommand{Action: "load", ID: "browser", Self: true}},
		{[]string{"load", "telegram", "--self"}, LinksCommand{Action: "load", ID: "telegram", Self: true}},
		{[]string{"load", "--self", "--json", "电脑操作"}, LinksCommand{Action: "load", ID: "电脑操作", Self: true, JSON: true}},
		{[]string{"load", "--self", ""}, LinksCommand{Action: "load", ID: "", Self: true}},
		// Help wins over every other constraint at either layer.
		{[]string{"search", "--self", "--query", "x", "--help"}, LinksCommand{Action: "search", Query: "x", Self: true, Help: true}},
		{[]string{"load", "--self", "browser", "--help"}, LinksCommand{Action: "load", ID: "browser", Self: true, Help: true}},
		{[]string{"search", "--help", "--query", "x"}, LinksCommand{Action: "search", Query: "x", Help: true}},
	} {
		got, err := ParseLinksRuntime(tc.args)
		if err != nil || got != tc.want {
			t.Fatalf("args=%q got=%+v want=%+v err=%v", tc.args, got, tc.want, err)
		}
	}
	for _, args := range [][]string{
		nil, {"help"}, {"-h"}, {"--"}, {"install", "--self", "browser"}, {"remove", "--self", "browser"},
		{"links", "--help"}, {"SEARCH", "--self"}, {"Load", "--self", "x"}, {"s", "--self"},
		{"search"}, {"load"}, {"load", "--self"}, {"load", "id1", "id2", "--self"},
		{"load", "--self", "browser", "extra"}, {"load", "--self", "--self", "browser"},
		{"search", "--self", "--query", "a", "--query", "b"},
		{"search", "--self", "--query-file", "a", "--query-file", "b"},
		{"search", "--self", "--json", "--json"},
		{"search", "--self", "--query", "a", "--query-file", "b"},
		{"search", "--self", "--query-file="}, {"search", "--self", "--query-file", ""},
		{"search", "--self", "--query"}, {"search", "--self", "--query-file"},
		{"search", "--self=true"}, {"load", "--self", "--json=true", "x"}, {"search", "--help=false"},
		{"load", "--self", "--query", "x", "browser"}, {"load", "--self", "--query-file", "x", "browser"},
		{"search", "--self", "--unknown"}, {"search", "--self", "positional"},
		{"search", "--self", "-"}, {"search", "--self", "-h"},
		{"load", "--self", "--unknown", "x"}, {"load", "--self", "-h", "x"},
		{"search", "--self", "--bot=other"}, {"search", "--self", "--call-id=x"},
		{"--self", "search"}, {"--json", "load", "--self", "x"},
		{"search", "--query", "--self"},
	} {
		if got, err := ParseLinksRuntime(args); err == nil {
			t.Fatalf("invalid args=%q accepted=%+v", args, got)
		}
	}
}
