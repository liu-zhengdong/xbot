//go:build smoke

package smoke

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

// Only the upstream model is scripted; every setter runs through native Shell,
// the actual BIN and the original nonprimary source listener.
func TestCLIPersonaRuntimeInputBoundaries(t *testing.T) {
	cliSkillIsolation(t)
	model := newCLIPersonaModel(t)
	srv := Start(t, &Stub{srv: model.server})
	const source = "PersonaInputWorker"
	srv.CreateBot(source, "Original nonprimary input-boundary duties.")
	primaryPath := filepath.Join(srv.Root, "primary.json")
	primaryBefore, err := os.ReadFile(primaryPath)
	if err != nil {
		t.Fatal(err)
	}
	var primary struct{ Name string }
	if err := json.Unmarshal(primaryBefore, &primary); err != nil || primary.Name == "" || primary.Name == source {
		t.Fatal("input fixture must use an actual API-created nonprimary source")
	}
	run := func(cue, command string) string {
		t.Helper()
		model.base.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "Exercise complete self persona input through the original source CLI", "command": command}))
		cliSkillSend(t, srv, source, cue)
		results := model.base.results(cue)
		if len(results) != 1 || results[0] == "" {
			t.Fatal("actual source did not execute one input-boundary operation")
		}
		output := commandOutput(results[0])
		if len(output) > 32<<10 || strings.Contains(output, "PERSONA_INPUT_") {
			t.Fatal("persona ACK/diagnostic exceeded native Shell32KiB or echoed saved/input duties")
		}
		return results[0]
	}
	// The original first wake seeds/assembles PickedCard. Take config/card
	// comparison snapshots only after that initialization has actually finished.
	if result := run("CLI_PERSONA_INPUT_WARM", `printf 'persona-input-source-ready\n'`); !strings.HasPrefix(result, "exit 0\n") {
		t.Fatal("actual source warm wake failed")
	}
	cardSnapshot := func() map[string][]byte {
		t.Helper()
		root := filepath.Join(srv.Root, "persona-cards")
		files := make(map[string][]byte)
		if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			files[rel] = raw
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return files
	}
	cardsBefore := cardSnapshot()
	configPath := filepath.Join(srv.Root, "bots", source, "config.json")
	configBefore, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(srv.Root, "work", source)
	writeInput := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(workspace, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	const head, tail = "PERSONA_INPUT_FULL_BEGIN\n", "\nPERSONA_INPUT_FULL_END"
	remaining := (32 << 10) - len(head) - len(tail)
	full := head + strings.Repeat("界", remaining/3) + strings.Repeat("x", remaining%3) + tail
	if len(full) != 32<<10 || !utf8.ValidString(full) || len([]rune(full)) >= len(full) {
		t.Fatal("exact-boundary fixture must contain complete UTF-8 and exactly32768 bytes")
	}
	writeInput("full-duty.txt", full)
	padded := strings.Repeat(" ", 20<<10) + "\n\t" + full + "\t\n "
	if len(padded) <= 32<<10 || strings.TrimSpace(padded) != full {
		t.Fatal("raw whitespace fixture must exceed32KiB with a valid full trimmed duty")
	}
	writeInput("padded-duty.txt", padded)
	const tiny, literal = "PERSONA_INPUT_TINY_DUTY", "PERSONA_INPUT_LITERAL_FILE_DUTY"
	writeInput("--self", literal)
	personaPath := filepath.Join(srv.Root, "bots", source, "persona.md")
	success := func(cue, command, want string) {
		t.Helper()
		var reply cliPersonaReply
		cliBotDecode(t, run(cue, command), &reply)
		if reply.Name != source || !reply.Written || reply.Effective != "next_wake" || reply.Error != "" {
			t.Fatal("actual self persona write lost its exact written/next_wake ACK")
		}
		stored, err := os.ReadFile(personaPath)
		if err != nil || !bytes.Equal(stored, []byte(want+"\n")) {
			t.Fatal("actual persona input was clipped, not trimmed, or not completely saved with one newline")
		}
		info, err := os.Stat(personaPath)
		if err != nil || info.Mode().Perm() != 0600 || info.Size() != int64(len(want)+1) {
			t.Fatal("actual persona replacement lost its exact stored bytes or0600 mode")
		}
	}
	success("CLI_PERSONA_INPUT_EXACT_FILE", `"$XBOT_BIN" persona set --self --file full-duty.txt --json`, full)
	// This pipe supplies real process stdin; the setter's ACK is neither piped
	// nor redirected, and the command stays below the existing Shell argv cap.
	success("CLI_PERSONA_INPUT_EXACT_STDIN", `cat full-duty.txt | "$XBOT_BIN" persona set --self --file - --json`, full)
	success("CLI_PERSONA_INPUT_RAW_TRIM", `"$XBOT_BIN" persona set --self --file padded-duty.txt --json`, full)
	success("CLI_PERSONA_INPUT_TINY_TEXT", `"$XBOT_BIN" persona set --self --text `+shellQuote(tiny)+` --json`, tiny)
	// The first --self selects runtime, while --file consumes the second one
	// as its literal filename. It must not be interpreted as a duplicate flag.
	success("CLI_PERSONA_INPUT_LITERAL_FILE", `"$XBOT_BIN" persona set --self --file --self --json`, literal)
	refused := func(cue, command string, typed bool) {
		t.Helper()
		before, err := os.ReadFile(personaPath)
		if err != nil {
			t.Fatal(err)
		}
		result := run(cue, command)
		exit, _, ok := strings.Cut(result, "\n")
		if !ok || !exitLine.MatchString(exit) || exit == "exit 0" || strings.TrimSpace(commandOutput(result)) == "" {
			t.Fatal("invalid persona input did not produce a bounded nonzero diagnostic")
		}
		if typed {
			// CLI emits the real typed failure before any stderr diagnostic.
			// Decode that first JSON value without pretending stderr is JSON.
			var reply cliPersonaReply
			if err := json.NewDecoder(strings.NewReader(commandOutput(result))).Decode(&reply); err != nil {
				t.Fatal("domain rejection lost its first complete typed persona ACK")
			}
			if reply.Name != source || reply.Written || reply.Effective != "" || reply.Error == "" {
				t.Fatal("domain rejection claimed saved duties or next-wake effectiveness")
			}
		}
		after, err := os.ReadFile(personaPath)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("rejected input replaced or clipped the previously saved persona")
		}
	}
	refused("CLI_PERSONA_INPUT_EMPTY", `"$XBOT_BIN" persona set --self --text `+shellQuote(" \t\n ")+` --json`, true)
	writeInput("domain-overflow.txt", " \n"+full+"Z\t ")
	refused("CLI_PERSONA_INPUT_DOMAIN_OVERFLOW", `"$XBOT_BIN" persona set --self --file domain-overflow.txt --json`, true)
	// Both transport-overflow fixtures trim to a legal duty, so silently
	// clipping raw data or skipping the wire check cannot hide behind32KiB
	// domain rejection. Preflight failures need not contain an HTTP reply DTO.
	rawOverflow := strings.Repeat(" ", (1<<20)+1-len(tiny)) + tiny
	if len(rawOverflow) != (1<<20)+1 || strings.TrimSpace(rawOverflow) != tiny {
		t.Fatal("raw fixture must be max+1 bytes and trim to a valid duty")
	}
	writeInput("raw-overflow.txt", rawOverflow)
	refused("CLI_PERSONA_INPUT_RAW_OVERFLOW", `"$XBOT_BIN" persona set --self --file raw-overflow.txt --json`, false)
	escaped := strings.Repeat("\t", 600<<10) + full
	wire, err := json.Marshal(map[string]string{"text": escaped})
	if err != nil {
		t.Fatal(err)
	}
	if len(escaped) >= 1<<20 || len(wire)+1 <= 1<<20 || strings.TrimSpace(escaped) != full {
		t.Fatal("wire fixture must fit raw1MiB and domain32KiB but exceed escapedJSON1MiB")
	}
	writeInput("escaped-overflow.txt", escaped)
	refused("CLI_PERSONA_INPUT_ESCAPED_OVERFLOW", `"$XBOT_BIN" persona set --self --file escaped-overflow.txt --json`, false)
	configAfter, err := os.ReadFile(configPath)
	if err != nil || !bytes.Equal(configBefore, configAfter) {
		t.Fatal("persona inputs changed config fallback or unrelated source fields")
	}
	if !reflect.DeepEqual(cardsBefore, cardSnapshot()) {
		t.Fatal("persona inputs changed the already-initialized card library")
	}
	primaryAfter, err := os.ReadFile(primaryPath)
	if err != nil || !bytes.Equal(primaryBefore, primaryAfter) {
		t.Fatal("nonprimary self persona input changed the actual Primary pointer")
	}
}
