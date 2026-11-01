package cliops

import "testing"

func TestBotRuntimeParserOfflineArity(t *testing.T) {
	for _, args := range [][]string{
		{"list"}, {"list", "ignored"}, {"delete", "ada"}, {"new", "ada"},
		{"new", "ada", "--persona", "--self"}, {"new", "ada", "--provider", "--help", "--model", "--self"},
		{"new", "ada", "--persona=--help"}, {"new", "ada", "-persona", "--self"},
		{"new", "--", "--self"}, {"delete", "--", "--help"},
	} {
		c, err := ParseBotRuntime(args)
		if err != nil || !c.Offline || c.Self || c.Help {
			t.Errorf("%q: command=%+v error=%v", args, c, err)
		}
	}
}

func TestBotRuntimeParserValid(t *testing.T) {
	for _, tc := range []struct {
		args                        []string
		action, name, persona, file string
		steps                       int
	}{
		{[]string{"list", "--self", "--json"}, "list", "", "", "", 0},
		{[]string{"info", "--steps", "99", "ada", "--self"}, "info", "ada", "", "", 99},
		{[]string{"info", "--self", "--steps=-1"}, "info", "", "", "", -1},
		{[]string{"new", "--persona", "--self", "ada", "--self"}, "new", "ada", "--self", "", 0},
		{[]string{"new", "--self", "ada", "--persona-file", "-"}, "new", "ada", "", "-", 0},
		{[]string{"new", "--self", "ada", "--persona="}, "new", "ada", "", "", 0},
		{[]string{"delete", "--self", "--", "--help"}, "delete", "--help", "", "", 0},
	} {
		c, err := ParseBotRuntime(tc.args)
		if err != nil || c.Offline || !c.Self || c.Action != tc.action || c.Name != tc.name || c.Persona != tc.persona || c.PersonaFile != tc.file || c.Steps != tc.steps {
			t.Errorf("%q: command=%+v error=%v", tc.args, c, err)
		}
	}
	for _, args := range [][]string{{"list", "--help"}, {"new", "--persona-file", "missing", "--help"}, {"info", "-h"}, {"delete", "--help"}} {
		c, err := ParseBotRuntime(args)
		if err != nil || !c.Help || c.Offline {
			t.Errorf("help %q: command=%+v error=%v", args, c, err)
		}
	}
}

func TestBotRuntimeParserRejects(t *testing.T) {
	for _, args := range [][]string{
		{}, {"bot"}, {"info"}, {"list", "--json"}, {"new", "ada", "--persona-file", "missing"},
		{"list", "ada", "--self"}, {"info", "a", "b", "--self"}, {"delete", "--self"},
		{"new", "ada", "--self"}, {"new", "ada", "--self", "--persona", "p", "--persona-file", "-"},
		{"new", "ada", "--self", "--persona-file="}, {"new", "ada", "--self", "--provider", "p", "--model", "m", "--persona", "p"},
		{"info", "--self", "--steps", "x"}, {"info", "--self", "--steps", "99999999999999999999999999"},
		{"list", "--self", "--steps=1"}, {"delete", "ada", "--self", "--persona=p"},
		{"list", "--self=true"}, {"list", "--self", "--self"}, {"list", "--self", "--unknown"},
		{"list", "--persona", "--self"}, {"delete", "ada", "--provider", "--self"},
		{"new", "ada", "--self", "--persona"}, {"info", "--self", "--steps=1", "--steps=2"},
	} {
		if _, err := ParseBotRuntime(args); err == nil {
			t.Errorf("accepted %q", args)
		}
	}
}
