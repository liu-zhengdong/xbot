//go:build smoke

package smoke

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/provider"
)

func TestCLICompactRuntimeSourceConsumers(t *testing.T) {
	cliSkillIsolation(t)
	const summary = "COMPACT_RUNTIME_SUMMARY: preserve the original decision42 and its readable source evidence."
	// Reuse only the existing isolated provider responder. Production compact.LLM
	// must still make its real two-message/no-tools Chat and record its usage.
	model := newCLIBotForkModel(t, summary)
	srv := Start(t, &Stub{srv: model.server})
	const source = "CompactChainSource"
	srv.CreateBot(source, "Compress a finished range while preserving its original evidence and source turn usage.")
	var primary struct{ Name string }
	primaryRaw, err := os.ReadFile(filepath.Join(srv.Root, "primary.json"))
	if err != nil || json.Unmarshal(primaryRaw, &primary) != nil || primary.Name == "" || primary.Name == source {
		t.Fatal("compact source must be actually nonprimary")
	}
	actual, err := bot.Load(srv.Root, source)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { bot.CloseStore(actual) })
	// Memory files are private persisted DATA, not a replacement memory service.
	memoryDir := filepath.Join(srv.Root, "bots", source, "memory")
	if err := os.MkdirAll(filepath.Join(memoryDir, "log"), 0700); err != nil {
		t.Fatal(err)
	}
	for path, text := range map[string]string{filepath.Join(memoryDir, "memory.md"): "# memory\n## 索引\n（暂无）\n## 条目\n- (2026-11-02) COMPACT_MEMORY_BODY_UNCHANGED\n", filepath.Join(memoryDir, "log", "2026-11.md"): "# 2026-11\n\n- 2026-11-02 COMPACT_LOG_BODY_UNCHANGED\n"} {
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	run := func(cue, command string) string {
		t.Helper()
		model.base.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "Use the actual original source Native Shell and its prepared call context", "command": command}))
		cliSkillSend(t, srv, source, cue)
		results := model.base.results(cue)
		if len(results) != 1 || !strings.HasPrefix(results[0], "exit 0\n") {
			t.Fatalf("actual source Native Shell did not succeed: %.900q", results)
		}
		return results[0]
	}
	const original = "COMPACT_ORIGINAL_NATIVE_DECISION_42"
	run("COMPACT_FINISHED_A17", `printf '`+original+`\n'`)
	to, err := actual.Count()
	if err != nil || to < 1 {
		t.Fatal("finished original source range missing")
	}
	originalRows, err := actual.Slice(1, to)
	if err != nil {
		t.Fatal(err)
	}
	originalToolSeq := 0
	for i, e := range originalRows {
		if e.Message.Role == provider.RoleTool && e.Message.CallID() == "COMPACT_FINISHED_A17_0" && strings.Contains(e.Message.Text(), original) {
			originalToolSeq = i + 1
		}
	}
	if originalToolSeq == 0 {
		t.Fatal("finished range did not contain the real original Native Shell result")
	}
	// Three genuine completed source turns leave the original default eight
	// raw entries intact; no fake owner/turn context or public Append is used.
	for i := 0; i < 3; i++ {
		run(fmt.Sprintf("COMPACT_RAW_TAIL_%d", i), `printf 'keep this completed source turn raw\n'`)
	}
	count, err := actual.Count()
	if err != nil || count-to < 8 {
		t.Fatal("finished compact range does not leave the original default raw tail")
	}
	before, err := actual.LoadState()
	if err != nil || len(before.Blocks) != 0 {
		t.Fatal("first range unexpectedly already compacted")
	}
	memoryBefore := map[string][]byte{}
	for _, path := range []string{filepath.Join(memoryDir, "memory.md"), filepath.Join(memoryDir, "log", "2026-11.md")} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		memoryBefore[path] = raw
	}
	const cue = "COMPACT_PREPARED_TURN_B29"
	const focus = "Preserve decision42 and its stable original tool evidence."
	result := run(cue, `"$XBOT_BIN" compact --self --from 1 --to `+strconv.Itoa(to)+` --focus `+shellQuote(focus)+` --json`)
	var reply struct {
		Text  string `json:"text,omitempty"`
		Error string `json:"error,omitempty"`
	}
	decoder := json.NewDecoder(strings.NewReader(commandOutput(result)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&reply); err != nil || reply.Error != "" || reply.Text != fmt.Sprintf("compacted 1-%d", to) {
		t.Fatal("source compact CLI did not preserve the original short acknowledgement")
	}
	model.mu.Lock()
	requests := append([]cliBotForkArchiveRequest(nil), model.archive...)
	model.mu.Unlock()
	if len(requests) != 1 || len(requests[0].Messages) != 2 || len(requests[0].Tools) != 0 || requests[0].Messages[0].Role != "system" || !strings.HasPrefix(requests[0].Messages[0].Content, compactionPrefix) || requests[0].Messages[1].Role != "user" || !strings.Contains(requests[0].Messages[1].Content, original) || !strings.Contains(requests[0].Messages[1].Content, focus) {
		t.Fatal("original compact.LLM did not make exactly one genuine two-message/no-tools provider request with original source data")
	}
	state, err := actual.LoadState()
	if err != nil || len(state.Blocks) != 1 || state.Blocks[0].From != 1 || state.Blocks[0].To != to || state.Blocks[0].Text != summary || state.Blocks[0].Focus != focus || !state.MemSnapSet || !strings.Contains(state.MemSnap, "COMPACT_MEMORY_BODY_UNCHANGED") {
		t.Fatal("actual source State did not save the original model Block and refreshed memory snapshot")
	}
	afterCount, err := actual.Count()
	if err != nil {
		t.Fatal(err)
	}
	rows, err := actual.Slice(1, afterCount)
	if err != nil {
		t.Fatal(err)
	}
	ownerSeq, ackSeq, snapshotSeq := 0, 0, 0
	sum := sha256.Sum256([]byte(state.MemSnap + "\n1:" + strconv.Itoa(to)))
	basis := hex.EncodeToString(sum[:6])
	for i, e := range rows {
		seq := i + 1
		if e.Message.Role == provider.RoleUser && !e.IsInternal() && strings.Contains(e.Message.Text(), cue) {
			ownerSeq = seq
		}
		if e.Message.Role == provider.RoleTool && e.Message.CallID() == cue+"_0" && strings.Contains(e.Message.Text(), reply.Text) {
			ackSeq = seq
		}
		text := e.Message.Text()
		if seq > count && e.IsInternal() && strings.HasPrefix(text, bot.EnvironmentOpen) && strings.HasSuffix(text, bot.EnvironmentClose) {
			var env struct {
				Snapshot string                     `json:"snapshot"`
				Set      map[string]json.RawMessage `json:"set"`
			}
			body := strings.TrimSuffix(strings.TrimPrefix(text, bot.EnvironmentOpen), bot.EnvironmentClose)
			if json.Unmarshal([]byte(body), &env) == nil && env.Snapshot == basis && len(env.Set) > 0 && env.Set["source"] != nil && env.Set["tools"] != nil {
				snapshotSeq = seq
			}
		}
	}
	// Original CompactRange publishes refreshMemSnap's complete environment
	// before saveCurrentState (blocks.go263-266). This is the matching persisted
	// publication before its ACK, not a guessed filesystem timestamp ordering.
	if ownerSeq <= count || snapshotSeq <= ownerSeq || ackSeq <= snapshotSeq {
		t.Fatal("new Block environment snapshot was not fully published under the saved State basis before the actual compact tool acknowledgement")
	}
	traces, err := actual.Traces()
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	for _, trace := range traces {
		if trace.Call == bot.TraceCallCompact {
			calls++
			if trace.Kind != bot.TraceUsage || trace.Turn != ownerSeq || trace.Prompt != 20 || trace.Completion != 10 || trace.Estimated {
				t.Fatal("compact usage did not preserve the actual prepared source turn and provider counters")
			}
		}
	}
	if calls != 1 {
		t.Fatal("actual compact.LLM usage was not recorded exactly once")
	}
	if len(rows) < len(originalRows) || !reflect.DeepEqual(rows[:len(originalRows)], originalRows) {
		t.Fatal("successful compact rewrote the original persisted transcript range")
	}
	for path, want := range memoryBefore {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatal("compact wrote memory.md or log body instead of only refreshing its snapshot")
		}
	}
	var expanded cliTranscriptReadReply
	cliBotDecode(t, run("COMPACT_RAW_READBACK_C31", `"$XBOT_BIN" transcript expand --self --from `+strconv.Itoa(originalToolSeq)+` --to `+strconv.Itoa(originalToolSeq)+` --json`), &expanded)
	if expanded.Error != "" || expanded.File != "" || !strings.Contains(expanded.Text, fmt.Sprintf("# seq %d tool", originalToolSeq)) || !strings.Contains(expanded.Text, originalRows[originalToolSeq-1].Message.Text()) {
		t.Fatal("the original compacted tool result is no longer readable at the same real seq")
	}
}
