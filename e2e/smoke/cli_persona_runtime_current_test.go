//go:build smoke

package smoke

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIPersonaRuntimeAtomicFailureAndCurrentRoute(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("real permission-denied file fault requires the ordinary unprivileged serve user")
	}
	cliSkillIsolation(t)
	model := newCLIPersonaModel(t)
	srv := Start(t, &Stub{srv: model.server})
	const original, current = "PersonaCurrentWorker", "PersonaCurrentMoved"
	const oldDuty = "PERSONA_ORIGINAL_ATOMIC_DUTY"
	srv.CreateBot(original, oldDuty)
	primaryPath := filepath.Join(srv.Root, "primary.json")
	primaryBefore, err := os.ReadFile(primaryPath)
	if err != nil {
		t.Fatal(err)
	}
	var primary struct{ Name string }
	if err := json.Unmarshal(primaryBefore, &primary); err != nil || primary.Name == "" || primary.Name == original {
		t.Fatal("current-route setter fixture must be nonprimary")
	}
	run := func(name, cue, command string) string {
		t.Helper()
		model.base.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "Observe original atomic persona failure and same source current route", "command": command}))
		cliSkillSend(t, srv, name, cue)
		results := model.base.results(cue)
		if len(results) != 1 || len(commandOutput(results[0])) > 32<<10 {
			t.Fatal("actual source did not return one bounded persona result")
		}
		return results[0]
	}
	warm := run(original, "CLI_PERSONA_CURRENT_WARM", `printf '%s\n' "$XBOT_ENDPOINT"`)
	if !strings.HasPrefix(warm, "exit 0\n") || strings.TrimSpace(commandOutput(warm)) == "" {
		t.Fatal("actual source warm/card assembly and original endpoint observation failed")
	}
	endpointBefore := strings.TrimSpace(commandOutput(warm))
	botDir := filepath.Join(srv.Root, "bots", original)
	personaPath := filepath.Join(botDir, "persona.md")
	before, err := os.ReadFile(personaPath)
	// The existing authenticated CreateBot helper appends its BOTNAME marker.
	// Preserve that actual complete old file, not only our input-duty fragment.
	if err != nil || string(before) != oldDuty+"\n\nBOTNAME="+original+"\n" {
		t.Fatal("actual complete helper-created old persona was not available before the FS fault")
	}
	configBefore, err := os.ReadFile(filepath.Join(botDir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	duty := "PERSONA_CURRENT_ROUTE_BEGIN\n" + strings.Repeat("Current complete duties after resource move.\n", 200) + "PERSONA_CURRENT_ROUTE_END"
	inputPath := filepath.Join(srv.Root, "work", original, "current-duty.txt")
	if err := os.WriteFile(inputPath, []byte(duty), 0600); err != nil {
		t.Fatal(err)
	}
	mode, err := os.Stat(botDir)
	if err != nil {
		t.Fatal(err)
	}
	// The daemon, not CLI child, performs WriteAtomic. Change only its actual
	// target directory permission inside the already running native command;
	// restore it before the tool returns so normal Handle state IO can finish.
	// The old persona remains readable throughout the real CreateTemp failure.
	faultCommand := `chmod 0500 ` + shellQuote(botDir) + ` || exit; "$XBOT_BIN" persona set --self --file current-duty.txt --json; persona_status=$?; chmod ` + fmt.Sprintf("%04o", mode.Mode().Perm()) + ` ` + shellQuote(botDir) + ` || exit; exit "$persona_status"`
	failed := run(original, "CLI_PERSONA_REAL_FS_FAILURE", faultCommand)
	line, _, ok := strings.Cut(failed, "\n")
	if !ok || !exitLine.MatchString(line) || line == "exit 0" {
		t.Fatal("real target-directory write denial did not cause a nonzero persona call")
	}
	var refusal cliPersonaReply
	if err := json.NewDecoder(strings.NewReader(commandOutput(failed))).Decode(&refusal); err != nil {
		t.Fatal("FS failure lost its actual typed save status")
	}
	if refusal.Name != original || refusal.Written || refusal.Effective != "" || refusal.Error == "" {
		t.Fatal("single atomic writer failure falsely reported saved or a later partial stage")
	}
	after, err := os.ReadFile(personaPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("actual failed atomic replacement changed the old duty")
	}
	configAfter, err := os.ReadFile(filepath.Join(botDir, "config.json"))
	if err != nil || !bytes.Equal(configBefore, configAfter) {
		t.Fatal("failed persona write changed source config fallback or fields")
	}
	modeAfter, err := os.Stat(botDir)
	if err != nil || modeAfter.Mode().Perm() != mode.Mode().Perm() {
		t.Fatal("scoped FS fixture did not restore original directory permissions")
	}
	// Both commands run in the same actual Shell with its original endpoint,
	// cwd inode and stale XBOT_BOT; profile moves the resources before persona.
	command := `"$XBOT_BIN" profile set --self --name ` + current + ` --json && "$XBOT_BIN" persona set --self --file current-duty.txt --json && printf 'PERSONA_ENDPOINT=%s\n' "$XBOT_ENDPOINT"`
	moved := run(original, "CLI_PERSONA_CURRENT_RENAME_WRITE", command)
	if !strings.HasPrefix(moved, "exit 0\n") {
		t.Fatal("original Shell could not write self persona after its resource move")
	}
	var objects []string
	var endpointAfter string
	for _, line := range strings.Split(strings.TrimSpace(commandOutput(moved)), "\n") {
		if strings.HasPrefix(line, "{") {
			objects = append(objects, line)
		}
		if value, ok := strings.CutPrefix(line, "PERSONA_ENDPOINT="); ok {
			endpointAfter = value
		}
	}
	if len(objects) != 2 || endpointAfter != endpointBefore {
		t.Fatal("same source did not preserve its original endpoint and two complete replies")
	}
	var profile struct {
		Name, PreviousName, Error string
		Applied, Moved            bool
	}
	if err := json.Unmarshal([]byte(objects[0]), &profile); err != nil || profile.Name != current || profile.PreviousName != original || !profile.Applied || !profile.Moved || profile.Error != "" {
		t.Fatal("actual source resource move did not complete")
	}
	var saved cliPersonaReply
	if err := json.Unmarshal([]byte(objects[1]), &saved); err != nil || saved.Name != current || !saved.Written || saved.Effective != "next_wake" || saved.Error != "" {
		t.Fatal("persona used the old name or failed the original source's current path")
	}
	storedPath := filepath.Join(srv.Root, "bots", current, "persona.md")
	stored, err := os.ReadFile(storedPath)
	if err != nil || !bytes.Equal(stored, []byte(duty+"\n")) {
		t.Fatal("current persona path lost complete duties")
	}
	if info, err := os.Stat(storedPath); err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("current-path persona file lost0600")
	}
	const next = "CLI_PERSONA_CURRENT_NEXT"
	if got := run(current, next, `printf 'persona-current-next\n'`); !strings.HasPrefix(got, "exit 0\n") {
		t.Fatal("next source wake did not follow its real current name")
	}
	requests := model.requests(next)
	if len(requests) == 0 {
		t.Fatal("next current-name wake did not reach provider")
	}
	for _, request := range requests {
		system := cliPersonaSystem(request)
		if !strings.Contains(system, duty) || strings.Contains(system, oldDuty) {
			t.Fatal("next current route did not assemble the newly stored duties")
		}
	}
	for _, path := range []string{filepath.Join(srv.Root, "bots", original), filepath.Join(srv.Root, "work", original)} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("old persona source path was recreated after current-route setter and next wake")
		}
	}
	primaryAfter, err := os.ReadFile(primaryPath)
	if err != nil || !bytes.Equal(primaryBefore, primaryAfter) {
		t.Fatal("nonprimary self persona writing moved the Primary pointer")
	}
}
