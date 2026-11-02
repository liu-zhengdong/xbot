//go:build smoke

package smoke

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/provider"
)

func TestCLICompactRuntimeCurrentRename(t *testing.T) {
	cliSkillIsolation(t)
	const old, current = "CompactCurrentOld", "CompactCurrentRenamed"
	const summary = "COMPACT_CURRENT_ORIGINAL_SUMMARY: retain decision73 and its original Native Shell evidence."
	const focus = "Preserve decision73 and its original source tool result."
	const description = "Current renamed source with the original compact call still running."
	model := newCLICompactModel(t, "success", summary)
	release := make(chan struct{})
	model.release = release // Set before Start can issue any provider request.
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	srv := Start(t, &Stub{srv: model.server})
	srv.CreateBot(old, "Compact a finished source range while its live source is renamed through the operator API.")
	srv.PatchBot(old, map[string]any{"timezone": "UTC"})
	primaryRaw, err := os.ReadFile(filepath.Join(srv.Root, "primary.json"))
	var primary struct{ Name string }
	if err != nil || json.Unmarshal(primaryRaw, &primary) != nil || primary.Name == "" || primary.Name == old || primary.Name == current {
		t.Fatal("compact rename requires an actual nonprimary source")
	}
	run := func(name, cue, command string) string {
		t.Helper()
		model.base.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "Use the actual source Native Shell and its original prepared call context", "command": command}))
		cliSkillSend(t, srv, name, cue)
		results := model.base.results(cue)
		if len(results) != 1 || !strings.HasPrefix(results[0], "exit 0\n") {
			t.Fatalf("actual source Native Shell did not succeed: %.900q", results)
		}
		return results[0]
	}
	const finished, original = "COMPACT_CURRENT_FINISHED_A73", "COMPACT_CURRENT_ORIGINAL_NATIVE_DECISION_73"
	run(old, finished, `printf '`+original+`\n'`)
	beforeSource, err := bot.Load(srv.Root, old)
	if err != nil {
		t.Fatal(err)
	}
	to, err := beforeSource.Count()
	if err != nil || to < 1 {
		t.Fatal("finished original source range missing")
	}
	originalRows, err := beforeSource.Slice(1, to)
	if err != nil {
		t.Fatal(err)
	}
	originalToolSeq := 0
	for i, entry := range originalRows {
		if entry.Message.Role == provider.RoleTool && entry.Message.CallID() == finished+"_0" && strings.Contains(entry.Message.Text(), original) {
			originalToolSeq = i + 1
		}
	}
	if originalToolSeq == 0 {
		t.Fatal("finished range does not contain its actual original Native Shell result")
	}
	// Three real completed owner/Shell turns leave the default raw tail intact.
	for i := 0; i < 3; i++ {
		run(old, fmt.Sprintf("COMPACT_CURRENT_TAIL_%d", i), `printf 'keep this real completed source turn raw\n'`)
	}
	count, err := beforeSource.Count()
	if err != nil || count-to < 8 {
		t.Fatal("finished compact range does not leave the original default raw tail")
	}
	before, err := beforeSource.LoadState()
	if err != nil || len(before.Blocks) != 0 {
		t.Fatal("original source range unexpectedly already compacted")
	}
	// Retire the observer's old-path store before the real mover runs; all
	// post-model observations load the actual current source from its new path.
	bot.CloseStore(beforeSource)

	const cue = "COMPACT_CURRENT_PREPARED_B83"
	command := `printf '%s\n' "$XBOT_CALL_ID" > compact-original-call.txt` + "\n" +
		`"$XBOT_BIN" compact --self --from 1 --to ` + strconv.Itoa(to) + ` --focus ` + shellQuote(focus) + ` --json`
	model.base.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "Keep the original compact CLI call running while its real provider waits", "command": command}))
	originalStream := srv.BeginSend(old, cue)
	select {
	case <-model.entered:
	case <-originalStream.done:
		t.Fatal("original source turn ended before compact.LLM reached its real HTTP provider")
	case <-time.After(10 * time.Second):
		t.Fatal("original compact.LLM did not enter its real HTTP provider")
	}
	// XBOT_CALL_ID is the actual running Native Shell job id (sh_*), not the
	// provider tool_call label. CallContext rejects anything else.
	callRaw, err := os.ReadFile(filepath.Join(srv.Root, "work", old, "compact-original-call.txt"))
	if err != nil || !strings.HasPrefix(strings.TrimSpace(string(callRaw)), "sh_") {
		t.Fatal("blocked compact did not preserve its actual original Native Shell CallID")
	}
	model.mu.Lock()
	requests := append([]cliBotForkArchiveRequest(nil), model.requests...)
	model.mu.Unlock()
	if len(requests) != 1 || !requests[0].Stream || len(requests[0].Messages) != 2 || len(requests[0].Tools) != 0 || requests[0].Messages[0].Role != "system" || !strings.HasPrefix(requests[0].Messages[0].Content, compactionPrefix) || requests[0].Messages[1].Role != "user" || !strings.Contains(requests[0].Messages[1].Content, original) || !strings.Contains(requests[0].Messages[1].Content, focus) {
		t.Fatal("held compact was not exactly one original two-message/no-tools HTTP provider request with the finished source evidence")
	}
	// The authenticated operator endpoint invokes the existing live profile
	// change/mover. It adds no owner text and cannot steal the scripted turn.
	status, moveRaw := cliFullAPI(t, srv, http.MethodPatch, "/api/bots/"+old, map[string]any{"name": current, "timezone": "Asia/Tokyo", "description": description})
	var move cliFullProfile
	if status != http.StatusOK || json.Unmarshal(moveRaw, &move) != nil || move.Name != current || move.PreviousName != old || !move.Moved || !move.Applied || move.Timezone != "Asia/Tokyo" || move.Description != description || move.Error != "" {
		t.Fatal("actual operator profile rename did not finish while the original compact provider was held")
	}
	select {
	case <-originalStream.done:
		t.Fatal("original source turn ended before its compact provider was released")
	case <-model.canceled:
		t.Fatal("operator rename canceled the original compact provider call")
	default:
	}
	model.mu.Lock()
	heldCount := len(model.requests)
	model.mu.Unlock()
	results := model.base.results(cue)
	if heldCount != 1 || len(results) != 1 || results[0] != "" {
		t.Fatal("operator rename replaced the original compact request or published a premature Shell result")
	}
	movedCall, err := os.ReadFile(filepath.Join(srv.Root, "work", current, "compact-original-call.txt"))
	if err != nil || string(movedCall) != string(callRaw) {
		t.Fatal("actual original Native Shell workspace did not move with its live source")
	}
	close(release)
	released = true
	select {
	case <-originalStream.done:
	case <-time.After(10 * time.Second):
		t.Fatal("original compact source turn did not finish after its real provider was released")
	}
	originalStream.Wait()
	results = model.base.results(cue)
	if len(results) != 1 || !strings.HasPrefix(results[0], "exit 0\n") || len(commandOutput(results[0])) > 32<<10 {
		t.Fatal("original compact CLI did not return its bounded successful acknowledgement after the source move")
	}
	var reply struct {
		Text  string `json:"text,omitempty"`
		Error string `json:"error,omitempty"`
	}
	if json.Unmarshal([]byte(commandOutput(results[0])), &reply) != nil || reply.Error != "" || reply.Text != fmt.Sprintf("compacted 1-%d", to) {
		t.Fatal("source rename changed the original compact acknowledgement")
	}
	actual, err := bot.Load(srv.Root, current)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { bot.CloseStore(actual) })
	state, err := actual.LoadState()
	if err != nil || len(state.Blocks) != 1 || state.Blocks[0].From != 1 || state.Blocks[0].To != to || state.Blocks[0].Text != summary || state.Blocks[0].Focus != focus || !state.MemSnapSet || !strings.Contains(state.MemSnap, filepath.Join(srv.Root, "bots", current, "memory")) || strings.Contains(state.MemSnap, filepath.Join(srv.Root, "bots", old)) {
		t.Fatal("original compact Block/current memory snapshot was not saved in the renamed source State")
	}
	afterCount, err := actual.Count()
	if err != nil {
		t.Fatal(err)
	}
	rows, err := actual.Slice(1, afterCount)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) < len(originalRows) || !reflect.DeepEqual(rows[:len(originalRows)], originalRows) {
		t.Fatal("current-source compact changed the original raw transcript range")
	}
	paths, err := actual.Paths()
	if err != nil {
		t.Fatal(err)
	}
	basisSum := sha256.Sum256([]byte(state.MemSnap + "\n1:" + strconv.Itoa(to)))
	basis := hex.EncodeToString(basisSum[:6])
	profileSum := sha256.Sum256([]byte(actual.ProfileText()))
	profile := hex.EncodeToString(profileSum[:6])
	ownerSeq, ownerCount, ackSeq, snapshotSeq, timeSeq := 0, 0, 0, 0, 0
	for i, entry := range rows {
		seq, text := i+1, entry.Message.Text()
		if entry.Message.Role == provider.RoleUser && !entry.IsInternal() && strings.Contains(text, cue) {
			ownerSeq, ownerCount = seq, ownerCount+1
		}
		if entry.Message.Role == provider.RoleTool && entry.Message.CallID() == cue+"_0" && strings.Contains(text, reply.Text) {
			ackSeq = seq
		}
		if seq <= count || !entry.IsInternal() || !strings.HasPrefix(text, bot.EnvironmentOpen) || !strings.HasSuffix(text, bot.EnvironmentClose) {
			continue
		}
		// The first full environment publication carries the new timezone clock;
		// later turns republish the same announcement.
		if timeSeq == 0 && strings.Contains(text, "当前时间：") && strings.Contains(text, "(Asia/Tokyo)") {
			timeSeq = seq
		}
		var env struct {
			Snapshot string                     `json:"snapshot"`
			Set      map[string]json.RawMessage `json:"set"`
		}
		body := strings.TrimSuffix(strings.TrimPrefix(text, bot.EnvironmentOpen), bot.EnvironmentClose)
		if snapshotSeq != 0 || json.Unmarshal([]byte(body), &env) != nil || env.Snapshot != basis {
			continue
		}
		for _, key := range []string{"paths", "reference", "persona", "rules", "skills", "profile", "overlay", "tools", "tool_index", "notes", "source"} {
			if env.Set[key] == nil {
				t.Fatalf("original compact did not publish the complete environment field %q under its current saved State basis", key)
			}
		}
		var envPaths bot.TracePaths
		var envProfile string
		var source struct {
			Name     string `json:"name"`
			SenderID string `json:"sender_id"`
		}
		var tools []string
		if json.Unmarshal(env.Set["paths"], &envPaths) != nil || envPaths != paths || json.Unmarshal(env.Set["profile"], &envProfile) != nil || envProfile != profile || json.Unmarshal(env.Set["source"], &source) != nil || source.Name != "web" || source.SenderID == "" || !slices.Contains(actual.Config().Owners, source.SenderID) || env.Set["person:"+source.SenderID] == nil || json.Unmarshal(env.Set["tools"], &tools) != nil || !slices.Contains(tools, "sh") {
			t.Fatal("post-model full environment lost the current source paths/profile/tools or actual authenticated owner")
		}
		snapshotSeq = seq
	}
	// This checks the original publication-before-ACK order and saved basis;
	// it makes no additional transaction or rename atomicity guarantee.
	if ownerCount != 1 || ownerSeq <= count || timeSeq <= ownerSeq || snapshotSeq <= timeSeq || ackSeq <= snapshotSeq {
		t.Fatalf("original current environment was not fully published before its compact acknowledgement: owner=%d time=%d snapshot=%d ack=%d", ownerSeq, timeSeq, snapshotSeq, ackSeq)
	}
	traces, err := actual.Traces()
	if err != nil {
		t.Fatal(err)
	}
	compactCount := 0
	for _, trace := range traces {
		if trace.Call == bot.TraceCallCompact {
			compactCount++
			if trace.Kind != bot.TraceUsage || trace.Turn != ownerSeq || trace.Prompt != 20 || trace.Completion != 10 || trace.Estimated {
				t.Fatal("original compact usage lost its exact provider20/10 or actual prepared owner-turn sequence at the current source")
			}
		}
	}
	model.mu.Lock()
	requestCount := len(model.requests)
	model.mu.Unlock()
	if compactCount != 1 || requestCount != 1 {
		t.Fatal("original compact request/usage was not preserved exactly once after operator rename")
	}
	var expanded cliTranscriptReadReply
	cliBotDecode(t, run(current, "COMPACT_CURRENT_RAW_READ_C93", `"$XBOT_BIN" transcript expand --self --from `+strconv.Itoa(originalToolSeq)+` --to `+strconv.Itoa(originalToolSeq)+` --json`), &expanded)
	if expanded.Error != "" || expanded.File != "" || !strings.Contains(expanded.Text, fmt.Sprintf("# seq %d tool", originalToolSeq)) || !strings.Contains(expanded.Text, originalRows[originalToolSeq-1].Message.Text()) {
		t.Fatal("current source CLI could not read the original compacted Native Shell result at its unchanged sequence")
	}
	for _, path := range []string{filepath.Join(srv.Root, "bots", old), filepath.Join(srv.Root, "work", old)} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("post-model compact/state/usage/readback recreated an old Bot or workspace directory")
		}
	}
}
