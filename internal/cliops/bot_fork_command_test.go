package cliops

import (
	"reflect"
	"strings"
	"testing"
)

func TestBotForkRuntimeGrammar(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want BotForkCommand
	}{
		{[]string{"child", "--self", "--role", "review changes"}, BotForkCommand{Name: "child", Self: true, Role: "review changes"}},
		{[]string{"--self", "--role-file=-", "--group", "project", "--json", "child"}, BotForkCommand{Name: "child", Self: true, RoleFile: "-", Group: "project", JSON: true}},
		{[]string{"child", "--self", "--role", "--help"}, BotForkCommand{Name: "child", Self: true, Role: "--help"}},
		{[]string{"child", "--role", "--self", "--self"}, BotForkCommand{Name: "child", Self: true, Role: "--self"}},
		{[]string{"child", "--self", "--role", "--"}, BotForkCommand{Name: "child", Self: true, Role: "--"}},
		{[]string{"child", "--self", "--role-file", "--help", "--group", "--self"}, BotForkCommand{Name: "child", Self: true, RoleFile: "--help", Group: "--self"}},
		{[]string{"--self", "--role=", "--", "--help"}, BotForkCommand{Name: "--help", Self: true}},
		{[]string{"--self", "--role=x", "--group=", "--", "--self"}, BotForkCommand{Name: "--self", Self: true, Role: "x"}},
		{[]string{"--self", "--role=x", "--", "--"}, BotForkCommand{Name: "--", Self: true, Role: "x"}},
		{[]string{"--help"}, BotForkCommand{Help: true}},
		{[]string{"child", "--role-file", "/absent/file", "--help"}, BotForkCommand{Name: "child", RoleFile: "/absent/file", Help: true}},
		// Domain limits and normalization are left to the original writer.
		{[]string{" \nchild/ ", "--self", "--role", " \n" + strings.Repeat("x", 8193), "--group", strings.Repeat("g", 49)}, BotForkCommand{Name: " \nchild/ ", Self: true, Role: " \n" + strings.Repeat("x", 8193), Group: strings.Repeat("g", 49)}},
	} {
		got, err := ParseBotForkRuntime(tc.args)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("args=%q got=%+v want=%+v err=%v", tc.args, got, tc.want, err)
		}
	}
}

func TestBotForkRuntimeGrammarRejects(t *testing.T) {
	for _, args := range [][]string{
		nil, {"child"}, {"child", "--self"}, {"--self", "--role=x"},
		{"child", "--role=x"}, {"", "--self", "--role=x"}, {"source", "child", "--self", "--role=x"},
		{"child", "--self", "--role"}, {"child", "--self", "--role-file"}, {"child", "--self", "--role=x", "--group"},
		{"child", "--self", "--role-file="}, {"child", "--self", "--role-file", ""},
		{"child", "--self", "--role=x", "--role-file=-"}, {"child", "--self", "--role=x", "--role=y"},
		{"child", "--self", "--role-file=-", "--role-file=-"}, {"child", "--self", "--role=x", "--group=a", "--group=b"},
		{"child", "--self", "--self", "--role=x"}, {"child", "--self", "--role=x", "--json", "--json"}, {"--help", "--help"},
		{"child", "--self=true", "--role=x"}, {"child", "--self", "--role=x", "--json=false"}, {"--help=yes"},
		{"child", "--self", "--role=x", "--unknown"}, {"--help", "--unknown"}, {"-h"},
		{"fork", "child", "--self", "--role=x"}, {"make", "child", "--self", "--role=x"}, {"clone", "child", "--self", "--role=x"},
		{"child", "--self", "--role=x", "--", "--json"}, {"--", "child", "--self", "--role=x"},
		// Arity consumes flag-looking data; it cannot establish self or help.
		{"child", "--role", "--self"}, {"child", "--role", "--help"}, {"child", "--role-file", "--help"},
		{"child", "--self", "--role=x", "--help", "extra"}, {"--help", "--role"}, {"--help", "--role=x", "--role-file=-"},
	} {
		if command, err := ParseBotForkRuntime(args); err == nil {
			t.Fatalf("accepted args=%q command=%+v", args, command)
		}
	}
}
