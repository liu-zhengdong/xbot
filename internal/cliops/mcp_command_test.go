package cliops

import "testing"

func TestMCPCommandTargetsAndArgumentForms(t *testing.T) {
	command, err := ParseMCP([]string{"call", "docs", "raw/name", "--args", `{"n":9007199254740993}`, "--self", "--readonly", "--json"})
	if err != nil || command.Tool != "raw/name" || !command.ReadOnly || !command.ArgsSet || command.Timeout != 0 {
		t.Fatalf("%+v %v", command, err)
	}
	for _, args := range [][]string{
		{"list"}, {"list", "ada"}, {"list", "--self", "--http", "127.0.0.1:9", "--bot", "x"},
		{"call", "s", "t", "--self", "--args", "{}", "--args-file", "-"},
		{"call", "s", "t", "--self", "--args", "{}", "--args", "{}"},
		{"tools", "s", "--self", "--scope", "bot"}, {"offline", "list", "ada", "--self"},
	} {
		if _, err := ParseMCP(args); err == nil {
			t.Fatalf("invalid argv accepted: %q", args)
		}
	}
	if _, err := ParseMCP([]string{"call", "--help"}); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`null`, `[]`, `{} {}`, `{"n":}`} {
		if ValidateMCPArgs([]byte(raw)) == nil {
			t.Fatalf("nonobject accepted: %s", raw)
		}
	}
	if err := ValidateMCPArgs([]byte(`{"n":9007199254740993}`)); err != nil {
		t.Fatal(err)
	}
}
