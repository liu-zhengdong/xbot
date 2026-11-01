//go:build smoke

package smoke

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"xbot/internal/bot"
	"xbot/internal/memory"
	"xbot/internal/peer"
	"xbot/internal/provider"
)

func cliBotForkPartialSource(t *testing.T, model *cliBotForkModel, source string) *Server {
	t.Helper()
	srv := Start(t, &Stub{srv: model.server})
	srv.CreateBot(source, "FORK_PARTIAL_SOURCE_PERSONA: retain real project context in the actual handoff.")
	primaryRaw, err := os.ReadFile(filepath.Join(srv.Root, "primary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var primary struct{ Name string }
	if err := json.Unmarshal(primaryRaw, &primary); err != nil || primary.Name == "" || primary.Name == source {
		t.Fatal("partial fork fixture must use the real API-created nonprimary source")
	}
	actual, err := bot.Load(srv.Root, source)
	if err != nil {
		t.Fatal(err)
	}
	if len(actual.Config().Owners) == 0 || actual.Config().Owners[0] == "" {
		t.Fatal("actual source lacks its authenticated API owner")
	}
	at := time.Now().UTC()
	for i, message := range []provider.Message{
		{Role: provider.RoleUser, Content: provider.TextContent("FORK_PARTIAL_COVERED_HISTORY")},
		{Role: provider.RoleAssistant, Content: provider.TextContent("FORK_PARTIAL_UNCOVERED_RAW_DECISION")},
	} {
		if err := actual.Append(bot.Entry{Time: at.Add(time.Duration(i) * time.Millisecond), Message: message}); err != nil {
			t.Fatal(err)
		}
	}
	if err := actual.SaveState(bot.State{Blocks: []bot.SummaryBlock{{From: 1, To: 1, Text: "FORK_PARTIAL_SAVED_SUMMARY"}}}); err != nil {
		t.Fatal(err)
	}
	if err := memory.Open(srv.Root).Remember(context.Background(), memory.Fact{
		Key: "partial-source-work", Scope: memory.ScopeBot, Tier: memory.TierProfile, Kind: memory.KindWork,
		BotName: source, WrittenBy: source, Text: "FORK_PARTIAL_SOURCE_BOT_FACT", Time: at,
	}); err != nil {
		t.Fatal(err)
	}
	binding := actual.Config()
	binding.ContextLimit = 24000
	if err := actual.InheritBinding(binding); err != nil {
		t.Fatal(err)
	}
	if err := actual.WriteRules("FORK_PARTIAL_SOURCE_RULES"); err != nil {
		t.Fatal(err)
	}
	// The caller finishes all fixture writes (including any future-child index
	// conflict) before restarting this private serve and capturing resources.
	return srv
}

func cliBotForkPartialCall(t *testing.T, srv *Server, model *cliBotForkModel, source, child, role, cue string) cliBotForkReply {
	t.Helper()
	model.base.add(cue, mcpRuntimeTool("sh", map[string]string{
		"description": "Observe actual late handoff memory failure through the original source Shell job",
		"command":     `"$XBOT_BIN" bot-fork ` + child + ` --self --role ` + shellQuote(role) + ` --group 'Partial group must wait' --json`,
	}))
	cliSkillSend(t, srv, source, cue)
	results := model.base.results(cue)
	if len(results) != 1 || results[0] == "" || len(commandOutput(results[0])) > 32<<10 {
		t.Fatal("actual partial fork did not return one bounded native Shell result")
	}
	exit, _, ok := strings.Cut(results[0], "\n")
	if !ok || !exitLine.MatchString(exit) || exit == "exit 0" {
		t.Fatal("actual late memory failure did not produce a nonzero CLI exit")
	}
	// The typed partial is emitted before any stderr diagnostic. Do not demand
	// stderr form one JSON document or guess its literal text.
	var reply cliBotForkReply
	if err := json.NewDecoder(strings.NewReader(commandOutput(results[0]))).Decode(&reply); err != nil {
		t.Fatal("partial fork lost its first complete typed receipt")
	}
	if reply.Name != child || !reply.Created || reply.CreateStage != "" || reply.Stage != "handoff_memory" || reply.Error == "" || reply.GroupID != "" || !slices.Equal(reply.Confirmed, []string{"owners", "binding", "rules"}) || reply.UncertainMemory == nil || reply.UncertainMemory.Bot != child || reply.UncertainMemory.Key == "" {
		t.Fatal("partial fork hid actual child creation/confirmed setup or claimed later stages")
	}
	model.mu.Lock()
	archive := append([]cliBotForkArchiveRequest(nil), model.archive...)
	model.mu.Unlock()
	if len(archive) != 1 || len(archive[0].Tools) != 0 || archive[0].Model == "" {
		t.Fatal("partial fork did not use one original nonstream no-tools compaction Chat request")
	}
	var input strings.Builder
	for _, message := range archive[0].Messages {
		input.WriteString(message.Content)
		input.WriteByte('\n')
	}
	for _, wanted := range []string{role, "FORK_PARTIAL_SOURCE_PERSONA", "FORK_PARTIAL_SAVED_SUMMARY", "FORK_PARTIAL_UNCOVERED_RAW_DECISION", "FORK_PARTIAL_SOURCE_BOT_FACT"} {
		if !strings.Contains(input.String(), wanted) {
			t.Fatal("actual compaction request omitted focus/persona/summary/raw history/Bot facts")
		}
	}
	return reply
}

func cliBotForkPartialChild(t *testing.T, srv *Server, source, name, role string) {
	t.Helper()
	original, err := bot.Load(srv.Root, source)
	if err != nil {
		t.Fatal(err)
	}
	child, err := bot.Load(srv.Root, name)
	if err != nil {
		t.Fatal("created partial child config was removed or never physically committed")
	}
	src, cfg := original.Config(), child.Config()
	if cfg.Name != name || cfg.Persona != role || !slices.Equal(cfg.Owners, src.Owners) || !slices.Equal(cfg.Links, src.Links) || cfg.Provider != src.Provider || cfg.Model != src.Model || cfg.ContextLimit != src.ContextLimit || cfg.Role != "" || cfg.BackgroundMaxTime != 0 {
		t.Fatal("created partial child lost confirmed owners/links/three binding fields or inherited source runtime role")
	}
	persona, err := os.ReadFile(filepath.Join(srv.Root, "bots", name, "persona.md"))
	if err != nil || !bytes.Equal(persona, []byte(role+"\n")) {
		t.Fatal("created partial child did not retain its complete explicit role persona")
	}
	childRules, err := os.ReadFile(filepath.Join(srv.Root, "bots", name, "rules.md"))
	if err != nil {
		t.Fatal(err)
	}
	sourceRules, err := os.ReadFile(filepath.Join(srv.Root, "bots", source, "rules.md"))
	if err != nil || !bytes.Equal(childRules, sourceRules) || !bytes.Contains(childRules, []byte("FORK_PARTIAL_SOURCE_RULES")) {
		t.Fatal("partial child lost the actual confirmed source rules")
	}
	state, err := child.LoadState()
	if err != nil || !reflect.DeepEqual(state, bot.State{}) {
		t.Fatal("partial child copied source summary/state or started model work")
	}
	entries, err := child.Entries()
	if err != nil || len(entries) != 0 {
		t.Fatal("partial child copied transcript or was started before the handoff failure")
	}
	// Source memory is a valid actual index; reading these bytes avoids a Recall
	// repairing or masking the deliberate child index failure in the other case.
	sourceIndex, err := os.ReadFile(filepath.Join(srv.Root, "bots", source, "memory", "memory.index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var facts []memory.Fact
	if err := json.Unmarshal(sourceIndex, &facts); err != nil {
		t.Fatal(err)
	}
	for _, fact := range facts {
		if fact.Key == "fork-"+name {
			t.Fatal("late source acquaintance note ran after handoff memory failure")
		}
	}
	groups, err := (&peer.Bus{Root: srv.Root}).ListGroups()
	if err != nil || len(groups) != 0 {
		t.Fatal("requested late group was created despite earlier memory failure")
	}
}

func TestCLIBotForkRuntimeMemoryCapacityPartial(t *testing.T) {
	cliSkillIsolation(t)
	const source, child, role = "ForkCapacitySource", "ForkCapacityChild", "Review the actual project handoff without losing its full memory payload."
	const head, tail = "CAPACITY_HANDOFF_BEGIN_", "_CAPACITY_HANDOFF_END"
	handoff := head + strings.Repeat("x", (8<<10)-len(head)-len(tail)) + tail
	if len(handoff) != 8<<10 || !utf8.ValidString(handoff) || strings.TrimSpace(handoff) != handoff {
		t.Fatal("capacity handoff must contain exactly8192 complete single-line UTF-8 bytes")
	}
	model := newCLIBotForkModel(t, handoff)
	srv := cliBotForkPartialSource(t, model, source)
	srv.Restart()
	reply := cliBotForkPartialCall(t, srv, model, source, child, role, "CLI_FORK_CAPACITY_PARTIAL")
	if reply.HandoffTotal < 9 || reply.HandoffParts <= 0 || reply.HandoffParts >= reply.HandoffTotal {
		t.Fatal("original4KiB capacity failure lost its acknowledged partial handoff count or clipped the8KiB total")
	}
	key := fmt.Sprintf("handoff-%d", reply.HandoffParts+1)
	if reply.UncertainMemory.Key != key {
		t.Fatal("uncertain memory does not identify the actual next failed handoff attempt")
	}
	cliBotForkPartialChild(t, srv, source, child, role)
	dir := filepath.Join(srv.Root, "bots", child, "memory")
	indexRaw, err := os.ReadFile(filepath.Join(dir, "memory.index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var persisted []memory.Fact
	if err := json.Unmarshal(indexRaw, &persisted); err != nil || len(persisted) != reply.HandoffParts {
		t.Fatal("physical handoff index does not match original Remember acknowledgements")
	}
	byKey := make(map[string]memory.Fact)
	for _, fact := range persisted {
		if _, duplicate := byKey[fact.Key]; duplicate || fact.Kind != memory.KindWork || fact.Scope != memory.ScopeBot || fact.Tier != memory.TierProfile || fact.BotName != child || fact.WrittenBy != source || fact.Time.IsZero() {
			t.Fatal("partial handoff changed actual persisted fact identity/scope/provenance")
		}
		byKey[fact.Key] = fact
	}
	for i := 0; i < reply.HandoffParts; i++ {
		key := "handoff"
		if i > 0 {
			key = fmt.Sprintf("handoff-%d", i+1)
		}
		fact, found := byKey[key]
		// This fixture has no whitespace, so the documented960-byte split has
		// unambiguous boundaries. The number of accepted parts is not fixed.
		start := i * 960
		if !found || start >= len(handoff) || fact.Text != handoff[start:min(start+960, len(handoff))] {
			t.Fatal("physical acknowledged handoff part was clipped or changed")
		}
	}
	md, err := os.ReadFile(filepath.Join(dir, "memory.md"))
	if err != nil || len(md) == 0 || len(md) > 4<<10 || !utf8.Valid(md) {
		t.Fatal("physical partial profile exceeded its original4KiB cap or lost valid complete Markdown")
	}
	for _, file := range []string{"memory.md", "memory.index.json"} {
		info, err := os.Stat(filepath.Join(dir, file))
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
			t.Fatal("partial memory did not retain its original complete0600 files")
		}
	}
}

func TestCLIBotForkRuntimeMemoryIndexPartial(t *testing.T) {
	cliSkillIsolation(t)
	const source, child, role = "ForkIndexSource", "ForkIndexChild", "Review the physical Markdown handoff when its index cannot be committed."
	const handoff = "INDEX_HANDOFF_FULL_BODY: retain the actual Markdown write even though Remember cannot acknowledge its later index write."
	model := newCLIBotForkModel(t, handoff)
	srv := cliBotForkPartialSource(t, model, source)
	childDir := filepath.Join(srv.Root, "bots", child)
	indexPath := filepath.Join(childDir, "memory", "memory.index.json")
	// Only the future index pathname is occupied. Original Create checks the
	// absent config; migration skips absent memory.md/profile.md; first Remember
	// loads an empty tier then commits MD before the index rename really fails.
	if err := os.MkdirAll(indexPath, 0700); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"config.json", "state.json", "persona.md", filepath.Join("memory", "memory.md")} {
		if _, err := os.Stat(filepath.Join(childDir, file)); !os.IsNotExist(err) {
			t.Fatal("index conflict fixture precreated child business data before actual runtime Create")
		}
	}
	srv.Restart()
	reply := cliBotForkPartialCall(t, srv, model, source, child, role, "CLI_FORK_INDEX_PARTIAL")
	if reply.HandoffTotal != 1 || reply.HandoffParts != 0 || reply.UncertainMemory.Key != "handoff" {
		t.Fatal("index failure falsely acknowledged the first Remember or lost its uncertain handoff identity")
	}
	cliBotForkPartialChild(t, srv, source, child, role)
	mdPath := filepath.Join(childDir, "memory", "memory.md")
	md, err := os.ReadFile(mdPath)
	if err != nil || !utf8.Valid(md) || !bytes.HasSuffix(md, []byte{'\n'}) || !bytes.Contains(md, []byte(handoff)) || len(md) > 4<<10 {
		t.Fatal("Remember index error hid or rolled back its actual complete first Markdown handoff write")
	}
	entryCount := 0
	for _, line := range strings.Split(string(md), "\n") {
		if strings.HasPrefix(line, "- (") {
			entryCount++
			if !memory.ValidMemoryLine(line) || !strings.HasSuffix(line, handoff) {
				t.Fatal("actual committed Markdown is not one complete valid handoff fact")
			}
		}
	}
	if entryCount != 1 {
		t.Fatal("index failure retried the unacknowledged fact or added later acquaintance notes")
	}
	info, err := os.Stat(mdPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		t.Fatal("actual committed handoff Markdown lost0600")
	}
	indexInfo, err := os.Stat(indexPath)
	if err != nil || !indexInfo.IsDir() {
		t.Fatal("failed index commit overwrote or removed its real filesystem conflict")
	}
}
