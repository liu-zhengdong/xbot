//go:build smoke

package smoke

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// Only the upstream model is scripted. All four reads cross native Shell and
// the actual BIN/source listener, without redirecting or piping their output.
func TestCLIBotRuntimeListInfoProjectionCurrentRoute(t *testing.T) {
	cliSkillIsolation(t)
	model := newMCPRuntimeModel(t)
	srv := Start(t, &Stub{srv: model.server})
	const original, current = "BotProjectionWorker", "BotProjectionMoved"
	const template, duty, count = "BotProjectionTemplate", "Projection duty.", 380
	srv.CreateBot(original, "Independent current-source Bot projection acceptance.")
	// A second actual authenticated API creation supplies a short persona and
	// valid config/state/persona files. Cold clones preserve its real owners,
	// links, binding and all other persisted fields; only config.name changes.
	status, _ := srv.do(http.MethodPost, "/api/bots", map[string]string{"name": template, "persona": duty})
	if status != http.StatusOK {
		t.Fatalf("create actual projection template: HTTP %d", status)
	}
	fixture := make(map[string][]byte)
	for _, file := range []string{"config.json", "state.json", "persona.md"} {
		raw, err := os.ReadFile(filepath.Join(srv.Root, "bots", template, file))
		if err != nil {
			t.Fatal(err)
		}
		if file != "persona.md" && !json.Valid(raw) {
			t.Fatal("actual API fixture has invalid persisted JSON")
		}
		fixture[file] = raw
	}
	var cfg map[string]json.RawMessage
	if err := json.Unmarshal(fixture["config.json"], &cfg); err != nil {
		t.Fatal(err)
	}
	var actual struct {
		Name, Persona, Provider, Model string
		Owners, Links                  []string
	}
	if err := json.Unmarshal(fixture["config.json"], &actual); err != nil {
		t.Fatal(err)
	}
	if actual.Name != template || actual.Persona != duty || strings.TrimSpace(string(fixture["persona.md"])) != duty || actual.Provider == "" || actual.Model == "" || len(actual.Owners) != 1 || actual.Owners[0] == "" {
		t.Fatal("actual authenticated fixture lacks its valid duty/owner/link/binding")
	}
	// Capture the real authenticated host identities, including its actual
	// Primary, rather than hard-coding a seed or inventing owner identities.
	status, raw := srv.do(http.MethodGet, "/api/bots", nil)
	if status != http.StatusOK {
		t.Fatalf("read authenticated host scope: HTTP %d", status)
	}
	var host struct{ Bots []struct{ Name string } }
	if err := json.Unmarshal(raw, &host); err != nil {
		t.Fatal(err)
	}
	wantLocal := make(map[string]string)
	var wantHost []string
	for _, item := range host.Bots {
		data, err := os.ReadFile(filepath.Join(srv.Root, "bots", item.Name, "config.json"))
		if err != nil {
			t.Fatal(err)
		}
		var persisted struct {
			Name, Persona string
			Owners        []string
		}
		if err := json.Unmarshal(data, &persisted); err != nil {
			t.Fatal(err)
		}
		if persisted.Name != item.Name || !slices.Contains(persisted.Owners, actual.Owners[0]) {
			t.Fatal("actual host identity does not belong to the authenticated owner")
		}
		if item.Name == original {
			wantHost = append(wantHost, current)
			continue // List excludes current source; host Info includes it.
		}
		wantHost = append(wantHost, item.Name)
		preview := strings.TrimSpace(persisted.Persona)
		wantLocal[item.Name] = preview[:min(len(preview), 80)]
	}
	if !slices.Contains(wantHost, current) || !slices.Contains(wantHost, template) {
		t.Fatal("authenticated host scope lacks the source or actual template")
	}
	cold := make(map[string]bool)
	var lowerList []map[string]string
	var lowerInfo []map[string]string
	for i := 0; i < count; i++ {
		// Exactly 64 ASCII letters/digits: legal, NFC and portable on all
		// supported platforms. Even minimal cold status DTOs exceed 32KiB.
		name := fmt.Sprintf("Projection%03d%s", i, strings.Repeat("x", 51))
		if len(name) != 64 {
			t.Fatal("projection fixture name must contain exactly 64 ASCII bytes")
		}
		dir := filepath.Join(srv.Root, "bots", name)
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		cfg["name"], _ = json.Marshal(name)
		clone, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		for file, data := range fixture {
			if file == "config.json" {
				data = append(clone, '\n')
			}
			if err := os.WriteFile(filepath.Join(dir, file), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
		cold[name] = true
		wantHost = append(wantHost, name)
		wantLocal[name] = duty
		lowerList = append(lowerList, map[string]string{"name": name, "persona_preview": duty})
		lowerInfo = append(lowerInfo, map[string]string{"bot": name, "state": "idle"})
	}
	// Canonical lower bounds use only mandatory/known fields. No trial runs or
	// product DTO/formatter is needed to choose how many real definitions fit.
	for _, reply := range []any{
		map[string]any{"local": lowerList, "remote": []any{}, "count": count},
		map[string]any{"bots": lowerInfo, "count": count},
	} {
		canonical, err := json.Marshal(reply)
		if err != nil {
			t.Fatal(err)
		}
		if len(canonical)+1 <= 32<<10 {
			t.Fatalf("full projection fixture must exceed native Shell32KiB: %d bytes", len(canonical)+1)
		}
	}
	slices.Sort(wantHost)
	workspace := filepath.Join(srv.Root, "work", original)
	if err := os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	const cue = "CLI_BOT_LIST_INFO_PROJECTION_CURRENT_ROUTE"
	model.add(cue,
		mcpRuntimeTool("sh", map[string]string{"description": "Move the source before projecting actual Bot reads", "command": `"$XBOT_BIN" profile set --name ` + current + ` --self --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Read the full peer list through bounded native Shell JSON", "command": `XBOT_BOT=` + original + ` "$XBOT_BIN" list --self --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Read full live-host and cold Bot status through bounded native Shell JSON", "command": `XBOT_BOT=` + original + ` "$XBOT_BIN" info --self --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Read the same peer list through normal human CLI output", "command": `XBOT_BOT=` + original + ` "$XBOT_BIN" list --self`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Read the same host status view through normal human CLI output", "command": `XBOT_BOT=` + original + ` "$XBOT_BIN" info --self`}),
	)
	cliSkillSend(t, srv, original, cue)
	results := model.results(cue)
	if len(results) != 5 {
		t.Fatalf("rename plus four actual projection results=%d", len(results))
	}
	var renamed cliFullProfile
	cliBotDecode(t, results[0], &renamed)
	if !renamed.Applied || !renamed.Moved || renamed.Name != current || renamed.PreviousName != original || renamed.Scope != "runtime" || renamed.Error != "" {
		t.Fatal("actual profile setup did not move the original source")
	}
	strictJSON := func(data []byte, dst any) {
		t.Helper()
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(dst); err != nil {
			t.Fatalf("projection must be a complete typed Reply JSON: %v", err)
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			t.Fatal("projection contains trailing output instead of one complete Reply JSON")
		}
	}
	readArtifact := func(file, access string, advertised int64) []byte {
		t.Helper()
		rel := filepath.Clean(filepath.FromSlash(file))
		if file == "" || filepath.IsAbs(rel) || !strings.HasPrefix(rel, "results"+string(filepath.Separator)) || access != "local" {
			t.Fatal("projection lacks a usable relative current-workspace results reference")
		}
		path := filepath.Join(srv.Root, "work", current, rel)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat actual artifact in renamed source workspace: %v", err)
		}
		if !info.Mode().IsRegular() || info.Size() <= 32<<10 || info.Size() > 20<<20 || info.Size() != advertised {
			t.Fatal("artifact lost complete bytes or changed the existing20MiB result limit")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if int64(len(data)) != advertised || bytes.Contains(data, []byte(srv.Token)) {
			t.Fatal("artifact byte facts or private authentication redaction failed")
		}
		return data
	}
	for index, result := range results[1:] {
		if !strings.HasPrefix(result, "exit 0\n") {
			t.Fatalf("actual projection read %d failed: %.1024q", index, result)
		}
		output := commandOutput(result)
		if len(output) == 0 || len(output) > 32<<10 {
			t.Fatalf("actual final JSON/human read %d exceeded native Shell32KiB: %d bytes", index, len(output))
		}
		isList := index%2 == 0
		var file, access string
		var advertised int64
		if index < 2 {
			if isList {
				var ref cliBotListReply
				strictJSON([]byte(output), &ref)
				if ref.Count != len(wantLocal) || ref.Local != nil || ref.Remote != nil || ref.Error != "" {
					t.Fatal("large List JSON lost complete count or retained partial inline arrays")
				}
				file, access, advertised = ref.File, ref.Access, ref.Bytes
			} else {
				var ref cliBotInfoReply
				strictJSON([]byte(output), &ref)
				if ref.Count != len(wantHost) || ref.Bots != nil || ref.Steps != nil || ref.Error != "" {
					t.Fatal("large Info JSON lost complete count or retained partial inline arrays")
				}
				file, access, advertised = ref.File, ref.Access, ref.Bytes
			}
		} else {
			// Checked against delivered FormatBotList/Info: count plus the
			// same relative path/access/bytes markers as the result convention.
			wantCount := len(wantHost)
			if isList {
				wantCount = len(wantLocal)
			}
			if !strings.Contains(output, fmt.Sprintf("count: %d\n", wantCount)) {
				t.Fatal("human projection lost the complete count")
			}
			for _, token := range strings.Fields(output) {
				token = strings.Trim(token, "`\"'(),")
				if strings.HasPrefix(token, "results/") {
					if file != "" {
						t.Fatal("human projection must identify one complete artifact")
					}
					file = token
				}
			}
			if file == "" || !strings.Contains(output, "access: local") {
				t.Fatal("human projection lacks its usable result path/access marker")
			}
			access = "local"
			_, after, ok := strings.Cut(output, "bytes: ")
			if !ok {
				t.Fatal("human projection lacks an actual bytes marker")
			}
			if _, err := fmt.Sscanf(after, "%d", &advertised); err != nil || advertised <= 0 {
				t.Fatal("human projection has no usable artifact byte count")
			}
		}
		data := readArtifact(file, access, advertised)
		if isList {
			var full cliBotListReply
			strictJSON(data, &full)
			if full.Local == nil || full.Remote == nil || len(*full.Remote) != 0 || full.Count != len(wantLocal) || len(*full.Local) != len(wantLocal) || full.File != "" || full.Access != "" || full.Bytes != 0 || full.Error != "" {
				t.Fatal("List artifact must be the full Reply JSON, with complete local/empty remote arrays")
			}
			var names []string
			seen := make(map[string]bool)
			for _, item := range *full.Local {
				preview, ok := wantLocal[item.Name]
				if !ok || seen[item.Name] || item.PersonaPreview != preview {
					t.Fatal("List artifact changed an actual complete identity/duty preview or included self")
				}
				seen[item.Name] = true
				names = append(names, item.Name)
			}
			if !slices.IsSorted(names) {
				t.Fatal("List artifact lost original loader ordering")
			}
		} else {
			var full cliBotInfoReply
			strictJSON(data, &full)
			if full.Bots == nil || full.Count != len(wantHost) || len(*full.Bots) != len(wantHost) || full.Steps != nil || full.File != "" || full.Access != "" || full.Bytes != 0 || full.Error != "" {
				t.Fatal("Info artifact must be the full typed host Reply JSON without reference/step substitution")
			}
			var names []string
			for _, item := range *full.Bots {
				names = append(names, item.Bot)
				if item.State == "" {
					t.Fatal("Info artifact omitted actual status metadata")
				}
				if cold[item.Bot] {
					if _, err := time.Parse(time.RFC3339, item.Since); err != nil {
						t.Fatal("cold host Info lost its actual absolute since timestamp")
					}
					item.Since = ""
					if item != (cliBotStatusView{Bot: item.Bot, State: "idle"}) {
						t.Fatal("cold host Info changed its complete real idle status metadata")
					}
				}
			}
			slices.Sort(names)
			if !slices.Equal(names, wantHost) {
				t.Fatal("Info artifact lost host-owned self/all identities or used List's exclude-self scope")
			}
		}
	}
	for _, old := range []string{filepath.Join(srv.Root, "work", original), filepath.Join(srv.Root, "bots", original)} {
		if _, err := os.Stat(old); !os.IsNotExist(err) {
			t.Fatal("projection revived or retained an old source directory after rename")
		}
	}
}
