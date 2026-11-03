//go:build smoke

package smoke

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/provider"
)

func TestCLICompactRuntimeFailureSemantics(t *testing.T) {
	cliSkillIsolation(t)
	for _, flavor := range []struct {
		name, mode, wantError string
		lateState             bool
		wantUsage             int
	}{
		{name: "provider_error", mode: "error", wantError: "COMPACT_SCRIPTED_PROVIDER_FAILURE"},
		{name: "empty_summary", mode: "empty", wantError: "compact: empty summary", wantUsage: 1},
		{name: "late_state_save", mode: "success", wantError: "permission denied", lateState: true, wantUsage: 1},
	} {
		t.Run(flavor.name, func(t *testing.T) {
			if flavor.lateState && (runtime.GOOS == "windows" || os.Geteuid() == 0) {
				t.Skip("actual EACCES State-save fault requires an ordinary unprivileged Unix serve user")
			}
			const summary = "COMPACT_FAILURE_SUCCESSFUL_CHAT: preserve the finished source decision42."
			model := newCLICompactModel(t, flavor.mode, summary)
			var release chan struct{}
			released := false
			if flavor.lateState {
				release = make(chan struct{})
				model.release = release // Fixed before any provider request, including warm turns.
				defer func() {
					if !released {
						close(release)
					}
				}()
			}
			srv := Start(t, &Stub{srv: model.server})
			const source = "CompactFailureSource"
			srv.CreateBot(source, "Preserve actual compact error semantics and original source-turn usage.")
			var primary struct{ Name string }
			primaryRaw, err := os.ReadFile(filepath.Join(srv.Root, "primary.json"))
			if err != nil || json.Unmarshal(primaryRaw, &primary) != nil || primary.Name == "" || primary.Name == source {
				t.Fatal("actual compact failure source must be nonprimary")
			}
			actual, err := bot.Load(srv.Root, source)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { bot.CloseStore(actual) })
			run := func(cue, command string) string {
				t.Helper()
				model.base.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "Use the original source Native Shell and canonical runtime CLI", "command": command}))
				cliSkillSend(t, srv, source, cue)
				results := model.base.results(cue)
				if len(results) != 1 || !strings.HasPrefix(results[0], "exit 0\n") {
					t.Fatalf("finished source Native Shell failed: %.900q", results)
				}
				return results[0]
			}
			const original = "COMPACT_FAILURE_FINISHED_NATIVE_DECISION_42"
			run("COMPACT_FAILURE_FINISHED_A17", `printf '`+original+`\n'`)
			to, err := actual.Count()
			if err != nil || to < 1 {
				t.Fatal("actual finished source range is missing")
			}
			originalRows, err := actual.Slice(1, to)
			if err != nil {
				t.Fatal(err)
			}
			foundOriginal := false
			for _, e := range originalRows {
				if e.Message.Role == provider.RoleTool && e.Message.CallID() == "COMPACT_FAILURE_FINISHED_A17_0" && strings.Contains(e.Message.Text(), original) {
					foundOriginal = true
				}
			}
			if !foundOriginal {
				t.Fatal("compact input lacks the genuine finished original Native Shell result")
			}
			// Three actual completed owner/Shell turns leave the default eight raw
			// entries. No fabricated USER entries or replacement Kernel are used.
			for i := 0; i < 3; i++ {
				run(fmt.Sprintf("COMPACT_FAILURE_TAIL_%d", i), `printf 'keep this completed source turn raw\n'`)
			}
			count, err := actual.Count()
			if err != nil || count-to < 8 {
				t.Fatal("actual compact range does not leave the original raw tail")
			}
			before, err := actual.LoadState()
			if err != nil || len(before.Blocks) != 0 || !before.MemSnapSet {
				t.Fatal("warm source State must have its real memory snapshot and no compact Block")
			}
			// Memory is unchanged throughout this test, so the existing frozen
			// snapshot plus the proposed range gives the new publication basis.
			sum := sha256.Sum256([]byte(before.MemSnap + "\n1:" + strconv.Itoa(to)))
			basis := hex.EncodeToString(sum[:6])
			const cue = "COMPACT_FAILURE_ORIGINAL_OWNER_B29"
			// XBOT_CALL_ID is the actual running Native Shell job id (sh_*), not
			// the provider tool_call label. CallContext rejects anything else.
			command := `case "$XBOT_CALL_ID" in sh_????????) ;; *) exit 97;; esac` + "\n" +
				`"$XBOT_BIN" compact --self --from 1 --to ` + strconv.Itoa(to) + ` --json`
			var botDir string
			var originalMode os.FileMode
			if flavor.lateState {
				botDir = filepath.Join(srv.Root, "bots", source)
				info, err := os.Stat(botDir)
				if err != nil {
					t.Fatal(err)
				}
				originalMode = info.Mode()
				t.Cleanup(func() {
					if err := os.Chmod(botDir, originalMode); err != nil {
						t.Error("restore actual source directory permissions:", err)
					}
				})
				trace, err := os.Stat(actual.TracePath())
				if err != nil || !trace.Mode().IsRegular() || trace.Size() == 0 {
					t.Fatal("warm source must already have its appendable trace file before the permission fault")
				}
				// Keep state.json readable. Turning it into a directory here would
				// fail refreshMemSnap's read BEFORE publication, not the final save.
				// Only CreateTemp in the real SaveState/WriteAtomic is denied.
				// The Shell restores exact permissions before returning its real
				// nonzero compact status, allowing normal turn/job cleanup to finish.
				restore := fmt.Sprintf("chmod %04o ", originalMode.Perm()) + shellQuote(botDir)
				command = "set +e\ntrap " + shellQuote(restore) + " 0\n" + command +
					"\ncompact_status=$?\n" + restore + " || exit $?\ntrap - 0\nexit \"$compact_status\""
			}
			model.base.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "Preserve actual compact.LLM failure facts under the original prepared source call", "command": command}))
			if flavor.lateState {
				done := make(chan struct{})
				go func() { defer close(done); cliSkillSend(t, srv, source, cue) }()
				select {
				case <-model.entered:
				case <-time.After(10 * time.Second):
					t.Fatal("original compact.LLM did not reach the actual HTTP provider")
				}
				// No observer race: the real provider cannot return until this
				// actual filesystem fault is installed and the gate is released.
				if err := os.Chmod(botDir, 0500); err != nil {
					t.Fatal(err)
				}
				close(release)
				released = true
				select {
				case <-done:
				case <-time.After(25 * time.Second):
					t.Fatal("actual late State-save failure did not return through the original source Shell")
				}
				info, err := os.Stat(botDir)
				if err != nil || info.Mode() != originalMode {
					t.Fatal("actual Shell did not restore original source permissions before returning")
				}
			} else {
				cliSkillSend(t, srv, source, cue)
			}
			results := model.base.results(cue)
			if len(results) != 1 {
				t.Fatal("compact failure must return one actual Native Shell result")
			}
			line, _, ok := strings.Cut(results[0], "\n")
			if !ok || !exitLine.MatchString(line) || line == "exit 0" {
				t.Fatal("actual compact failure lost its nonzero CLI exit status")
			}
			var reply map[string]json.RawMessage
			if err := json.NewDecoder(strings.NewReader(commandOutput(results[0]))).Decode(&reply); err != nil || len(reply) != 1 {
				t.Fatal("compact failure must return exactly one error-only JSON object without an ACK")
			}
			var actualError string
			if json.Unmarshal(reply["error"], &actualError) != nil || !strings.Contains(actualError, flavor.wantError) {
				t.Fatalf("compact failure lost its actual cause: %.900q", actualError)
			}
			if flavor.lateState && (!strings.Contains(actualError, filepath.Join(botDir, ".tmp-")) || strings.Contains(actualError, "rename ")) {
				t.Fatalf("late State-save fault must report the actual EACCES/CreateTemp error, not rename EISDIR: %.900q", actualError)
			}
			model.mu.Lock()
			requests := append([]cliBotForkArchiveRequest(nil), model.requests...)
			model.mu.Unlock()
			if len(requests) != 1 || len(requests[0].Messages) != 2 || len(requests[0].Tools) != 0 || requests[0].Messages[0].Role != "system" || !strings.HasPrefix(requests[0].Messages[0].Content, compactionPrefix) || requests[0].Messages[1].Role != "user" || !strings.Contains(requests[0].Messages[1].Content, original) {
				t.Fatal("failure must preserve exactly one real two-message/no-tools compact.LLM Chat without retry")
			}
			state, err := actual.LoadState()
			if err != nil || len(state.Blocks) != 0 || state.Covered != 0 || state.Summary != "" {
				t.Fatal("failed compact saved a compact Block or legacy coverage")
			}
			afterCount, err := actual.Count()
			if err != nil {
				t.Fatal(err)
			}
			rows, err := actual.Slice(1, afterCount)
			if err != nil {
				t.Fatal(err)
			}
			ownerSeq, resultSeq, snapshotSeq := 0, 0, 0
			for i, e := range rows {
				seq := i + 1
				if e.Message.Role == provider.RoleUser && !e.IsInternal() && strings.Contains(e.Message.Text(), cue) {
					ownerSeq = seq
				}
				if e.Message.Role == provider.RoleTool && e.Message.CallID() == cue+"_0" && strings.Contains(e.Message.Text(), actualError) {
					resultSeq = seq
				}
				text := e.Message.Text()
				if seq <= count || !e.IsInternal() || !strings.HasPrefix(text, bot.EnvironmentOpen) || !strings.HasSuffix(text, bot.EnvironmentClose) {
					continue
				}
				var env struct {
					Snapshot string                     `json:"snapshot"`
					Set      map[string]json.RawMessage `json:"set"`
				}
				body := strings.TrimSuffix(strings.TrimPrefix(text, bot.EnvironmentOpen), bot.EnvironmentClose)
				if json.Unmarshal([]byte(body), &env) != nil || env.Snapshot != basis {
					continue
				}
				for _, key := range []string{"source", "tools", "paths", "persona", "rules", "skills", "profile", "overlay", "reference", "tool_index", "notes"} {
					if env.Set[key] == nil {
						t.Fatalf("new-basis snapshot lost its complete environment field %q", key)
					}
				}
				if snapshotSeq == 0 {
					snapshotSeq = seq
				}
			}
			if ownerSeq <= count || resultSeq <= ownerSeq {
				t.Fatal("actual failure result lost its original owner turn and Native Shell call ID")
			}
			if flavor.lateState {
				if snapshotSeq <= ownerSeq || snapshotSeq >= resultSeq {
					t.Fatalf("real State-save failure lost the already-published full new-basis environment before its error result: owner=%d snapshot=%d result=%d", ownerSeq, snapshotSeq, resultSeq)
				}
			} else if snapshotSeq != 0 {
				t.Fatal("provider or empty-summary failure published the proposed compact Block environment")
			}
			traces, err := actual.Traces()
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			for _, trace := range traces {
				if trace.Call != bot.TraceCallCompact {
					continue
				}
				calls++
				if trace.Kind != bot.TraceUsage || trace.Turn != ownerSeq || trace.Prompt != 20 || trace.Completion != 10 || trace.Estimated {
					t.Fatal("successful Chat usage lost the original source owner turn or its reported 20/10 counters")
				}
			}
			if calls != flavor.wantUsage {
				t.Fatalf("compact usage count=%d want=%d; provider errors must not record successful usage, while empty/late errors retain actual Chat usage", calls, flavor.wantUsage)
			}
		})
	}
}
