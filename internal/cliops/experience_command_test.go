package cliops

import (
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestExperienceRuntimeGrammar(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want ExperienceCommand
	}{
		{[]string{"--help"}, ExperienceCommand{Help: true}},
		{[]string{"record", "--help"}, ExperienceCommand{Action: "record", Help: true}},
		{[]string{"search", "--help"}, ExperienceCommand{Action: "search", Help: true}},
		{[]string{"record", "--self", "--summary", ""}, ExperienceCommand{Action: "record", Self: true}},
		{[]string{"record", "--self", "--summary", "--help", "--focus", "--", "--from=-2", "--to", "0", "--json"}, ExperienceCommand{Action: "record", Self: true, Summary: "--help", Focus: "--", From: -2, JSON: true}},
		{[]string{"record", "--summary", "--self", "--self", "--focus-file=-", "--to=-1"}, ExperienceCommand{Action: "record", Self: true, Summary: "--self", FocusFile: "-", To: -1}},
		{[]string{"record", "--self", "--summary-file=-", "--focus-file=large", "--from=1", "--to=2"}, ExperienceCommand{Action: "record", Self: true, SummaryFile: "-", FocusFile: "large", From: 1, To: 2}},
		{[]string{"search", "--self"}, ExperienceCommand{Action: "search", Self: true}},
		{[]string{"search", "--self", "--query", "--help", "--limit=-8", "--json"}, ExperienceCommand{Action: "search", Self: true, Query: "--help", Limit: -8, JSON: true}},
		{[]string{"search", "--self", "--query-file=-", "--limit=9"}, ExperienceCommand{Action: "search", Self: true, QueryFile: "-", Limit: 9}},
	} {
		got, err := ParseExperienceRuntime(tc.args)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("args=%q got=%+v want=%+v err=%v", tc.args, got, tc.want, err)
		}
	}
	// Domain range tolerance and the original limit fallback must stay downstream.
	for _, n := range []int{-99, 0, 1, 8, 9, 123456} {
		command, err := ParseExperienceRuntime([]string{"search", "--self", "--limit", strconv.Itoa(n)})
		if err != nil || command.Limit != n {
			t.Fatalf("limit=%d command=%+v err=%v", n, command, err)
		}
	}
}

func TestExperienceRuntimeGrammarRejects(t *testing.T) {
	for _, args := range [][]string{
		nil, {}, {"record"}, {"search"}, {"record", "--self"},
		{"record", "--summary=x"}, {"search", "--query=x"},
		{"record", "--self", "--summary=x", "extra"}, {"search", "--self", "positional"},
		{"experience", "search", "--self"}, {"help"}, {"record", "-h"},
		{"search", "--self", "--"}, {"search", "--self", "-"}, {"search", "--self=true"},
		{"search", "--json=yes", "--help"}, {"search", "--help=yes"},
		{"search", "--self", "--self"}, {"search", "--help", "--help"},
		{"record", "--self", "--summary=x", "--summary=y"},
		{"record", "--self", "--summary=x", "--summary-file=f"},
		{"record", "--self", "--summary=x", "--focus=x", "--focus-file=f"},
		{"search", "--self", "--query=x", "--query-file=f"},
		{"record", "--self", "--summary-file=-", "--focus-file=-"},
		{"record", "--summary-file=-", "--focus-file=-", "--help"},
		{"record", "--self", "--summary-file="}, {"search", "--self", "--query-file="},
		{"record", "--self", "--summary=x", "--focus-file="},
		{"record", "--self", "--summary"}, {"search", "--self", "--query"},
		{"record", "--self", "--summary=x", "--from=1.5"},
		{"record", "--self", "--summary=x", "--to=--help"},
		{"search", "--self", "--limit=99999999999999999999999999999"},
		{"search", "--self", "--limit="}, {"search", "--self", "--limit=NaN"},
		{"record", "--self", "--summary=x", "--query=q"},
		{"search", "--self", "--summary=x"}, {"search", "--self", "--from=0"},
		{"record", "--self", "--summary=x", "--limit=8"},
		{"search", "--self", "--unknown"}, {"unknown", "--help"},
	} {
		if got, err := ParseExperienceRuntime(args); err == nil {
			t.Errorf("accepted args=%q as %+v", args, got)
		}
	}
	large := " \n" + strings.Repeat("x", 20<<10) + "\n "
	got, err := ParseExperienceRuntime([]string{"record", "--self", "--summary", large, "--focus", strings.Repeat("f", 20<<10)})
	if err != nil || got.Summary != large {
		t.Fatalf("parser applied domain size or trim: bytes=%d err=%v", len(got.Summary), err)
	}
}
