package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"xbot/internal/cliops"
)

func TestBotInputPreservesPersonaAndFile(t *testing.T) {
	// More than 32 KiB must reach the domain's real WritePersona stage.
	persona := " \n" + strings.Repeat("p", 33<<10) + "\n "
	command := cliops.BotCommand{Name: " ada ", Persona: persona}
	input, err := readBotCreateInput(command, nil)
	if err != nil || input.Name != " ada " || input.Persona != persona {
		t.Fatalf("inline name=%q bytes=%d error=%v", input.Name, len(input.Persona), err)
	}
	for _, value := range []string{"", " \n ", "--self", "--help"} {
		input, err := readBotCreateInput(cliops.BotCommand{Name: "ada", Persona: value}, nil)
		if err != nil || input.Persona != value {
			t.Errorf("persona=%q input=%+v error=%v", value, input, err)
		}
	}
	path := filepath.Join(t.TempDir(), "persona.txt")
	if err := os.WriteFile(path, []byte(persona), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		file  string
		stdin string
	}{{path, "ignored"}, {"-", persona}} {
		input, err := readBotCreateInput(cliops.BotCommand{Name: "ada", PersonaFile: tc.file}, strings.NewReader(tc.stdin))
		if err != nil || input.Persona != persona {
			t.Errorf("file=%q bytes=%d error=%v", tc.file, len(input.Persona), err)
		}
	}
}

func TestBotInputRawAndEscapedRequestLimits(t *testing.T) {
	base, _ := json.Marshal(cliops.BotCreate{Name: "ada"})
	persona := strings.Repeat("p", cliops.BotMaxRequest-len(base))
	input, err := readBotCreateInput(cliops.BotCommand{Name: "ada", PersonaFile: "-"}, strings.NewReader(persona))
	if err != nil {
		t.Fatal(err)
	}
	wire, _ := json.Marshal(input)
	if len(wire) != cliops.BotMaxRequest {
		t.Fatalf("exact request bytes=%d", len(wire))
	}
	if _, err := readBotCreateInput(cliops.BotCommand{Name: "ada", Persona: persona + "p"}, nil); err == nil {
		t.Fatal("accepted final JSON request overflow")
	}
	if _, err := readBotCreateInput(cliops.BotCommand{Name: "ada", Persona: strings.Repeat("<", cliops.BotMaxRequest/2)}, nil); err == nil {
		t.Fatal("accepted JSON escaping overflow")
	}
	reader := strings.NewReader(strings.Repeat("p", cliops.BotMaxRequest+100))
	if _, err := readBotCreateInput(cliops.BotCommand{Name: "ada", PersonaFile: "-"}, reader); err == nil || reader.Len() != 99 {
		t.Fatalf("raw max+1 error=%v bytes left=%d", err, reader.Len())
	}
	path := filepath.Join(t.TempDir(), "oversized.txt")
	if err := os.WriteFile(path, []byte(strings.Repeat("p", cliops.BotMaxRequest+1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readBotCreateInput(cliops.BotCommand{Name: "ada", PersonaFile: path}, nil); err == nil {
		t.Fatal("accepted oversized file")
	}
}
