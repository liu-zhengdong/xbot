package cliops

import (
	"reflect"
	"strings"
	"testing"
)

func TestChatGroupRuntimeParserGrammar(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want ChatGroupCommand
	}{
		{[]string{"--help"}, ChatGroupCommand{Help: true}},
		{[]string{"list", "--self", "--json"}, ChatGroupCommand{Action: "list", Self: true, JSON: true}},
		{[]string{"history", "--keyword", "--help", "g_1", "--self", "--limit=-2", "--before-id=b", "--sender", "--self"}, ChatGroupCommand{Action: "history", ID: "g_1", Keyword: "--help", Sender: "--self", BeforeID: "b", Limit: -2, Self: true}},
		{[]string{"create", "--member", "--self", "--member=B,C", "--self", "--", "--help"}, ChatGroupCommand{Action: "create", Name: "--help", Members: []string{"--self", "B,C"}, Self: true}},
		{[]string{"post", "g", "--self", "--text", "--help"}, ChatGroupCommand{Action: "post", ID: "g", Text: "--help", Self: true}},
		{[]string{"post", "g", "--self", "--file", "-"}, ChatGroupCommand{Action: "post", ID: "g", TextFile: "-", Self: true}},
		{[]string{"post", "--help"}, ChatGroupCommand{Action: "post", Help: true}},
	} {
		got, err := ParseChatGroupRuntime(tc.args)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("args=%q got=%+v want=%+v err=%v", tc.args, got, tc.want, err)
		}
	}
	for _, args := range [][]string{
		{}, {"group"}, {"send"}, {"list"}, {"list", "--self", "extra"}, {"history", "--self"},
		{"history", "g", "--self", "--limit=no"}, {"history", "g", "--self", "--limit"},
		{"history", "g", "--self", "--limit=1", "--limit=2"}, {"list", "--self", "--self"},
		{"list", "--self=true"}, {"list", "--self", "--member=B"}, {"create", "n", "--self"},
		{"post", "g", "--self"}, {"post", "g", "--self", "--text=x", "--file=-"},
		{"post", "g", "--self", "--file="}, {"post", "g", "--self", "--text=x", "--targetSource=A"},
		{"post", "g", "--self", "--text=x", "--", "--help"}, {"list", "--help", "--bogus"},
	} {
		if c, err := ParseChatGroupRuntime(args); err == nil {
			t.Fatalf("accepted %q: %+v", args, c)
		}
	}
	// CLI syntax must not steal the existing domain's name/member/text validation.
	c, err := ParseChatGroupRuntime([]string{"create", strings.Repeat("n", 100), "--self", "--member="})
	if err != nil || len(c.Members) != 1 || c.Members[0] != "" {
		t.Fatalf("domain validation moved into parser: %+v %v", c, err)
	}
}
