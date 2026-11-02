//go:build smoke

package smoke

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/provider"
)

func TestCLICompactRuntimeCallCancel(t *testing.T) {
	cliSkillIsolation(t)
	model := newCLICompactModel(t, "success", "THIS_BLOCKED_SUMMARY_MUST_NOT_COMPLETE")
	release := make(chan struct{})
	model.release = release
	defer close(release)
	srv := Start(t, &Stub{srv: model.server})
	const source = "CompactCancelSource"
	srv.CreateBot(source, "Cancel a waiting source compaction request while preserving its prepared Native Shell identity.")
	var primary struct{ Name string }
	raw, err := os.ReadFile(filepath.Join(srv.Root, "primary.json"))
	if err != nil || json.Unmarshal(raw, &primary) != nil || primary.Name == "" || primary.Name == source {
		t.Fatal("cancel source must be actually nonprimary")
	}
	actual, err := bot.Load(srv.Root, source)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { bot.CloseStore(actual) })
	run := func(cue, command string) {
		t.Helper()
		model.base.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "Call the canonical CLI through the original source Native Shell", "command": command}))
		cliSkillSend(t, srv, source, cue)
		r := model.base.results(cue)
		if len(r) != 1 || !strings.HasPrefix(r[0], "exit 0\n") {
			t.Fatalf("actual source Native Shell failed: %.900q", r)
		}
	}
	run("COMPACT_CANCEL_FINISHED_A11", `printf 'ORIGINAL_CANCEL_SOURCE_FINISHED_DATA\n'`)
	to, err := actual.Count()
	if err != nil || to < 1 {
		t.Fatal("original finished range missing")
	}
	for i := 0; i < 3; i++ {
		run("COMPACT_CANCEL_TAIL_"+strconv.Itoa(i), `printf 'preserve the original raw tail\n'`)
	}
	count, err := actual.Count()
	if err != nil || count-to < 8 {
		t.Fatal("actual raw tail is insufficient")
	}
	const cue = "COMPACT_CANCEL_PREPARED_B22"
	// Kill only this CLI's live HTTP client after the real compact.LLM Chat is
	// waiting. The same original Native Shell/CallID stays alive until wait ends.
	command := `printf '%s\n' "$XBOT_CALL_ID" > compact-cancel-call.txt` + "\n" +
		`"$XBOT_BIN" compact --self --from 1 --to ` + strconv.Itoa(to) + ` --json > compact-cancel-receipt.txt 2>&1 &` + "\n" +
		`compact_client_pid=$!` + "\n" + `while [ ! -f compact-cancel-ready ]; do sleep 0.01; done` + "\n" +
		`kill -TERM "$compact_client_pid"` + "\n" + `wait "$compact_client_pid"` + "\n" +
		`compact_client_status=$?` + "\n" + `printf '%s\n' "$compact_client_status" > compact-cancel-exit.txt` + "\n" +
		`test "$compact_client_status" -ne 0 || exit 1` + "\n" + `printf 'COMPACT_CANCEL_ORIGINAL_SHELL_STILL_RUNNING\n'`
	model.base.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "Cancel the original prepared Compact HTTP client while its real provider Chat is held", "command": command}))
	stream := srv.BeginSend(source, cue)
	select {
	case <-model.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("original compact.LLM did not enter its actual provider Chat")
	}
	workspace := filepath.Join(srv.Root, "work", source)
	// XBOT_CALL_ID is the actual running Native Shell job id (sh_*), not the
	// provider tool_call label. CallContext rejects anything else.
	call, err := os.ReadFile(filepath.Join(workspace, "compact-cancel-call.txt"))
	if err != nil || !strings.HasPrefix(strings.TrimSpace(string(call)), "sh_") {
		t.Fatal("the held Compact request did not inherit its actual Native Shell CallID")
	}
	if err := os.WriteFile(filepath.Join(workspace, "compact-cancel-ready"), []byte("CANCEL_THE_SAME_LIVE_CLIENT\n"), 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-model.canceled:
	case <-time.After(10 * time.Second):
		t.Fatal("actual provider Chat context did not cancel after this live source CLI disconnected")
	}
	stream.Wait()
	result := model.base.results(cue)
	if len(result) != 1 || !strings.HasPrefix(result[0], "exit 0\n") || !strings.Contains(result[0], "COMPACT_CANCEL_ORIGINAL_SHELL_STILL_RUNNING") {
		t.Fatal("the original prepared Native Shell did not survive and reap its canceled CLI")
	}
	status, err := os.ReadFile(filepath.Join(workspace, "compact-cancel-exit.txt"))
	if err != nil || strings.TrimSpace(string(status)) == "0" || strings.TrimSpace(string(status)) == "" {
		t.Fatal("canceled source CLI did not terminate with a real nonzero client status")
	}
	receipt, err := os.ReadFile(filepath.Join(workspace, "compact-cancel-receipt.txt"))
	if err != nil || strings.Contains(string(receipt), "compacted 1-") {
		t.Fatal("canceled waiting Compact client falsely returned a success acknowledgement")
	}
	model.mu.Lock()
	requests := append([]cliBotForkArchiveRequest(nil), model.requests...)
	model.mu.Unlock()
	if len(requests) != 1 || len(requests[0].Messages) != 2 || len(requests[0].Tools) != 0 {
		t.Fatal("cancellation did not affect exactly one actual two-message/no-tools compact.LLM call")
	}
	st, err := actual.LoadState()
	if err != nil || len(st.Blocks) != 0 {
		t.Fatal("provider Chat canceled while waiting nevertheless saved a successful summary Block")
	}
	traces, err := actual.Traces()
	if err != nil {
		t.Fatal(err)
	}
	for _, trace := range traces {
		if trace.Call == bot.TraceCallCompact {
			t.Fatal("provider Chat cancellation was incorrectly recorded as successful compact usage")
		}
	}
	n, err := actual.Count()
	if err != nil {
		t.Fatal(err)
	}
	rows, err := actual.Slice(1, n)
	if err != nil {
		t.Fatal(err)
	}
	paired := false
	for _, entry := range rows {
		if entry.Message.Role == provider.RoleTool && entry.Message.CallID() == cue+"_0" && strings.Contains(entry.Message.Text(), "COMPACT_CANCEL_ORIGINAL_SHELL_STILL_RUNNING") {
			paired = true
		}
	}
	if !paired {
		t.Fatal("canceled CLI did not belong to the original surviving Native Shell tool call")
	}
	// Ordinary environment announcements may already exist. Cancellation does not
	// imply zero side effects or create a new never-commit barrier after Chat.
}
