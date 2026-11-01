//go:build smoke

package smoke

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCLIPersonaRuntimeFileNextWake(t *testing.T) {
	cliSkillIsolation(t)
	model := newCLIPersonaModel(t)
	srv := Start(t, &Stub{srv: model.server})
	const source = "PersonaFileWorker"
	const original = "PERSONA_ORIGINAL_FILE_WORKER_DUTY"
	srv.CreateBot(source, original)
	primaryRaw, err := os.ReadFile(filepath.Join(srv.Root, "primary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var primary struct{ Name string }
	if err := json.Unmarshal(primaryRaw, &primary); err != nil || primary.Name == "" || primary.Name == source {
		t.Fatal("file setter must use a real nonprimary source")
	}
	run := func(cue, command string) string {
		t.Helper()
		model.base.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "Observe complete nonprimary source duty write and original next wake", "command": command}))
		cliSkillSend(t, srv, source, cue)
		results := model.base.results(cue)
		if len(results) != 1 || len(commandOutput(results[0])) > 32<<10 {
			t.Fatal("actual source did not return one bounded native Shell result")
		}
		return results[0]
	}
	const warm = "CLI_PERSONA_FILE_WARM"
	if result := run(warm, `printf 'persona-card-assembly-ready\n'`); !strings.HasPrefix(result, "exit 0\n") {
		t.Fatal("actual initial warm wake failed")
	}
	// PickedCard/EnsureSeeds is an original first-wake initializer, so compare
	// card bytes only after that actual source wake has finished.
	cardSnapshot := func() map[string][]byte {
		t.Helper()
		root := filepath.Join(srv.Root, "persona-cards")
		files := map[string][]byte{}
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
	warmRequests := model.requests(warm)
	if len(warmRequests) == 0 {
		t.Fatal("warm wake never reached the actual provider")
	}
	oldEnv := cliPersonaEnvironment(warmRequests[len(warmRequests)-1])
	_, cardEnv, hasCard := strings.Cut(oldEnv, "；人设卡 ")
	if !hasCard || cardEnv == "" {
		t.Fatal("actual warm environment did not expose its separately seeded card fingerprint")
	}
	cardEnv = "；人设卡 " + cardEnv
	defaultID := strings.TrimSpace(string(cardsBefore[".default"]))
	cardRaw, ok := cardsBefore[filepath.Join(defaultID, "card.md")]
	if !ok {
		t.Fatal("actual selected default card was not present after warm")
	}
	cardHeader, cardBody, ok := strings.Cut(string(cardRaw), "\n\n")
	if !ok {
		t.Fatal("actual seeded card has no body")
	}
	cardName := strings.TrimSpace(strings.TrimPrefix(cardHeader, "name:"))
	cardBody = strings.TrimSpace(cardBody)
	if cardName == "" || cardBody == "" {
		t.Fatal("actual seeded card is incomplete")
	}
	duty := "PERSONA_COMPLETE_FILE_BEGIN\n" + strings.Repeat("职责与边界完整保存。", 800) + "\nPERSONA_COMPLETE_FILE_END"
	if len(duty) <= 16<<10 || len(duty) > 32<<10 {
		t.Fatal("fixture must prove a complete UTF-8 duty beyond native command argv limit")
	}
	inputPath := filepath.Join(srv.Root, "work", source, "complete-duty.txt")
	if err := os.WriteFile(inputPath, []byte(" \n\t"+duty+"\n\t "), 0600); err != nil {
		t.Fatal(err)
	}
	const writeCue = "CLI_PERSONA_FILE_WRITE"
	var reply cliPersonaReply
	cliBotDecode(t, run(writeCue, `XBOT_BOT=`+shellQuote(primary.Name)+` "$XBOT_BIN" persona set --self --file complete-duty.txt --json`), &reply)
	if reply.Name != source || !reply.Written || reply.Effective != "next_wake" || reply.Error != "" {
		t.Fatal("nonprimary self file write lost exact saved/next-wake receipt or trusted diagnostic Primary")
	}
	personaPath := filepath.Join(srv.Root, "bots", source, "persona.md")
	stored, err := os.ReadFile(personaPath)
	if err != nil || !bytes.Equal(stored, []byte(duty+"\n")) {
		t.Fatal("file input was clipped, not trimmed, or not stored as complete text plus newline")
	}
	info, err := os.Stat(personaPath)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("actual persona replacement did not preserve0600")
	}
	writtenRequests := model.requests(writeCue)
	if len(writtenRequests) < 2 {
		t.Fatal("setter never returned to the same actual active provider request chain")
	}
	for _, request := range writtenRequests {
		system := cliPersonaSystem(request)
		if !strings.Contains(system, original) || strings.Contains(system, duty) {
			t.Fatal("setter proactively rebuilt the already assembled active system prefix")
		}
		for _, message := range request.Messages {
			if message.Role != "system" && strings.Contains(message.Content, duty) {
				t.Fatal("full file duty was duplicated into environment, event or appended history")
			}
		}
	}
	const nextCue = "CLI_PERSONA_FILE_NEXT"
	if result := run(nextCue, `printf 'persona-next-wake-observed\n'`); !strings.HasPrefix(result, "exit 0\n") {
		t.Fatal("original next source wake failed")
	}
	nextRequests := model.requests(nextCue)
	if len(nextRequests) == 0 {
		t.Fatal("next actual Handle never reached the provider")
	}
	sum := sha256.Sum256([]byte(duty))
	wantEnv := "职责 " + hex.EncodeToString(sum[:6]) + cardEnv
	for _, request := range nextRequests {
		system := cliPersonaSystem(request)
		if strings.Count(system, duty) != 1 || strings.Contains(system, original) || !strings.Contains(system, "### 人设卡 · "+cardName) || !strings.Contains(system, cardBody) {
			t.Fatal("next actual wake lost full duties, kept old duties, or changed the separate card section")
		}
		if got := cliPersonaEnvironment(request); got != wantEnv {
			t.Fatal("next original environment did not carry exact duty12hex hash plus unchanged card fingerprint")
		}
		for _, message := range request.Messages {
			if message.Role != "system" && strings.Contains(message.Content, duty) {
				t.Fatal("full next-wake duties leaked into non-system history or environment")
			}
		}
	}
	configAfter, err := os.ReadFile(configPath)
	if err != nil || !bytes.Equal(configBefore, configAfter) {
		t.Fatal("persona write changed config fallback or other source fields")
	}
	if !reflect.DeepEqual(cardsBefore, cardSnapshot()) {
		t.Fatal("duty replacement changed the already-initialized card library")
	}
}
