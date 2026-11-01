//go:build smoke

package smoke

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/memory"
	"xbot/internal/peer"
	"xbot/internal/provider"
)

func TestCLIBotForkRuntimeNonemptyHandoff(t *testing.T) {
	cliSkillIsolation(t)
	const source, child = "ForkHandoffSource", "ForkHandoffChild"
	const role = "Review public project decisions and ask the source for details with peer-message."
	const rawMarker, summaryMarker, botMarker, personMarker = "SOURCE_RAW_PRIVATE_DECISION", "SOURCE_EXISTING_SUMMARY", "SOURCE_BOT_PROFILE_METHOD", "SHARED_PERSON_PROFILE_TEA"
	handoff := "SAFE_ALLOWED_HANDOFF_BEGIN " + strings.Repeat("Review public acceptance decisions only. ", 40) + " SAFE_ALLOWED_HANDOFF_END"
	model := newCLIBotForkModel(t, handoff)
	srv := Start(t, &Stub{srv: model.server})
	srv.CreateBot(source, "Source project duties; project X public review handoff.")
	primaryRaw, err := os.ReadFile(filepath.Join(srv.Root, "primary.json"))
	var primary struct{ Name string }
	if err != nil || json.Unmarshal(primaryRaw, &primary) != nil || primary.Name == "" || primary.Name == source {
		t.Fatal("actual fork source must be nonprimary")
	}
	cfgPath := filepath.Join(srv.Root, "bots", source, "config.json")
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		t.Fatal("invalid authenticated source config")
	}
	fields["role"] = json.RawMessage(`"dev"`)
	fields["background_max_time"] = json.RawMessage(`7200`)
	fields["context_limit"] = json.RawMessage(`48000`)
	raw, err = json.MarshalIndent(fields, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	src, err := bot.Load(srv.Root, source)
	if err != nil {
		t.Fatal(err)
	}
	sourceCfg := src.Config()
	if len(sourceCfg.Owners) == 0 {
		t.Fatal("actual source owner absent")
	}
	owner := sourceCfg.Owners[0]
	if err := src.WriteRules("FORK_INHERITED_PUBLIC_RULE\n"); err != nil {
		t.Fatal(err)
	}
	for _, m := range []provider.Message{{Role: provider.RoleUser, Content: provider.TextContent("SOURCE_COVERED_RAW")}, {Role: provider.RoleAssistant, Content: provider.TextContent(rawMarker)}} {
		if err := src.Append(bot.Entry{Time: time.Now().UTC(), Message: m}); err != nil {
			t.Fatal(err)
		}
	}
	if err := src.SaveState(bot.State{Blocks: []bot.SummaryBlock{{From: 1, To: 1, Text: summaryMarker}}}); err != nil {
		t.Fatal(err)
	}
	mem := memory.Open(srv.Root)
	for _, fact := range []memory.Fact{{Key: "source-method", Kind: memory.KindWork, Scope: memory.ScopeBot, Tier: memory.TierProfile, BotName: source, WrittenBy: source, Text: botMarker, Time: time.Now().UTC()}, {Key: "person-drink", Kind: memory.KindAttribute, Scope: memory.ScopePerson, Tier: memory.TierProfile, PersonID: owner, WrittenBy: source, Text: personMarker, Time: time.Now().UTC()}} {
		if err := mem.Remember(context.Background(), fact); err != nil {
			t.Fatal(err)
		}
	}
	personPath := filepath.Join(srv.Root, "people", owner, "memory", "memory.md")
	personBefore, err := os.ReadFile(personPath)
	if err != nil {
		t.Fatal(err)
	}
	// All durable input data completes before actual source Resources capture.
	srv.Restart()
	const cue = "CLI_FORK_NONEMPTY_ARCHIVE"
	command := `XBOT_BOT=` + primary.Name + ` "$XBOT_BIN" bot-fork ` + child + ` --self --role ` + shellQuote(role) + ` --group 'Fork public review' --json`
	model.base.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "Fork from real nonempty summary/raw/Bot facts through the original prepared source", "command": command}))
	cliSkillSend(t, srv, source, cue)
	results := model.base.results(cue)
	if len(results) != 1 || len(commandOutput(results[0])) > 32<<10 {
		t.Fatal("actual fork did not return one bounded short ACK")
	}
	var reply cliBotForkReply
	cliBotDecode(t, results[0], &reply)
	if reply.Name != child || !reply.Created || reply.CreateStage != "" || !slices.Equal(reply.Confirmed, []string{"owners", "binding", "rules", "handoff", "child_note", "source_note", "group"}) || reply.HandoffParts < 2 || reply.HandoffParts != reply.HandoffTotal || reply.UncertainMemory != nil || reply.GroupID == "" || reply.Stage != "complete" || reply.Error != "" {
		t.Fatal("real source fork lost confirmed child/memory/group completion facts")
	}
	model.mu.Lock()
	archives := append([]cliBotForkArchiveRequest(nil), model.archive...)
	model.mu.Unlock()
	if len(archives) != 1 || len(archives[0].Tools) != 0 || len(archives[0].Messages) != 2 {
		t.Fatal("nonempty source did not make exactly one original NoTools compact.LLM Chat")
	}
	archiveBody := archives[0].Messages[1].Content
	for _, marker := range []string{rawMarker, summaryMarker, botMarker, role, "Source persona:", "Focus:", "<transcript>"} {
		if !strings.Contains(archiveBody, marker) {
			t.Fatal("original focused archive lost raw/summary/Bot facts/persona/role input")
		}
	}
	if archives[0].Model != sourceCfg.Model {
		t.Fatal("archive did not use actual source provider/model binding")
	}
	created, err := bot.Load(srv.Root, child)
	if err != nil {
		t.Fatal(err)
	}
	childCfg := created.Config()
	if childCfg.Provider != sourceCfg.Provider || childCfg.Model != sourceCfg.Model || childCfg.ContextLimit != sourceCfg.ContextLimit || !reflect.DeepEqual(childCfg.Owners, sourceCfg.Owners) || !reflect.DeepEqual(childCfg.Links, sourceCfg.Links) || childCfg.Role == bot.RoleDev || childCfg.BackgroundMaxTime != 0 || childCfg.Working().BackgroundMaxTime != 45*time.Minute || len(childCfg.Webhooks) != 0 || len(childCfg.Watches) != 0 {
		t.Fatal("new child binding/owners/links/defaultchat45min are not the original inheritance shape")
	}
	persona, err := created.ReadPersona()
	if err != nil || strings.TrimSpace(persona) != role {
		t.Fatal("role duties did not become actual child persona")
	}
	rules, err := created.ReadRules("")
	if err != nil || strings.TrimSpace(rules) != "FORK_INHERITED_PUBLIC_RULE" {
		t.Fatal("actual source rules were not inherited")
	}
	entries, err := created.Entries()
	if err != nil || len(entries) != 0 {
		t.Fatal("fork copied or woke a child transcript")
	}
	state, err := created.LoadState()
	if err != nil || len(state.Blocks) != 0 || state.Summary != "" || state.Covered != 0 {
		t.Fatal("fork copied source summary blocks")
	}
	for _, name := range []string{source, child} {
		inboxRaw, err := os.ReadFile(filepath.Join(srv.Root, "bots", name, "inbox.jsonl"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range bytes.Split(bytes.TrimSpace(inboxRaw), []byte{'\n'}) {
			if len(line) == 0 {
				continue
			}
			var record struct{ Op string }
			if json.Unmarshal(line, &record) != nil || record.Op == "post" {
				t.Fatal("fork/group meta posted a synthetic source or child event")
			}
		}
	}
	groups, err := (&peer.Bus{Root: srv.Root}).ListGroups()
	if err != nil || len(groups) != 1 || groups[0].ID != reply.GroupID || !slices.Equal(groups[0].Members, []string{child, source}) {
		t.Fatal("optional group did not contain only source and actual child")
	}
	if _, err := os.Stat(filepath.Join(srv.Root, "groups", reply.GroupID, "transcript.jsonl")); !os.IsNotExist(err) {
		t.Fatal("optional group creation posted a message")
	}
	childFacts, err := mem.Recall(context.Background(), memory.Query{Scope: memory.ScopeBot, BotName: child, Tier: memory.TierProfile})
	if err != nil {
		t.Fatal(err)
	}
	var joined strings.Builder
	handoffParts := 0
	childNote := false
	for _, fact := range childFacts {
		joined.WriteString(fact.Text)
		joined.WriteByte('\n')
		if strings.HasPrefix(fact.Key, "handoff") {
			handoffParts++
		}
		if fact.Key == "fork-source" && fact.WrittenBy == source && strings.Contains(fact.Text, source) && strings.Contains(fact.Text, "peer-message") {
			childNote = true
		}
	}
	if handoffParts != reply.HandoffParts || !childNote || !strings.Contains(joined.String(), "SAFE_ALLOWED_HANDOFF_END") {
		t.Fatal("real child facts lost full generated handoff or privacy follow-up note")
	}
	for _, marker := range []string{rawMarker, summaryMarker, botMarker, personMarker} {
		if strings.Contains(joined.String(), marker) {
			t.Fatal("child Bot memory copied source raw/summary/facts or shared person store instead of chosen handoff")
		}
	}
	sourceFacts, err := mem.Recall(context.Background(), memory.Query{Scope: memory.ScopeBot, BotName: source, Tier: memory.TierProfile})
	if err != nil {
		t.Fatal(err)
	}
	sourceNote := false
	for _, fact := range sourceFacts {
		if fact.Key == "fork-"+child && fact.BotName == source && fact.WrittenBy == source && strings.Contains(fact.Text, child) && strings.Contains(fact.Text, "peer-message") {
			sourceNote = true
		}
	}
	if !sourceNote {
		t.Fatal("original source lacks its actual reciprocal follow-up note")
	}
	personAfter, err := os.ReadFile(personPath)
	if err != nil || !bytes.Equal(personBefore, personAfter) {
		t.Fatal("fork copied or rewrote shared person memory")
	}
	if _, err := os.Stat(filepath.Join(srv.Root, "bots", child, "people")); !os.IsNotExist(err) {
		t.Fatal("fork created a private person-store copy")
	}
	panel := srv.Memory(child)
	if !strings.Contains(panel.Bot.Markdown, "SAFE_ALLOWED_HANDOFF_END") || !strings.Contains(panel.Person.Markdown, personMarker) {
		t.Fatal("existing child memory panel did not compose own handoff and original shared person view")
	}
	const wake = "CLI_FORK_CHILD_NEXT_WAKE"
	model.base.add(wake)
	cliSkillSend(t, srv, child, wake)
	model.mu.Lock()
	captured := append([][]byte(nil), model.raw...)
	model.mu.Unlock()
	composed := false
	for _, raw := range captured {
		var req stubRequest
		if json.Unmarshal(raw, &req) != nil || !strings.Contains(lastUser(req), wake) {
			continue
		}
		var text strings.Builder
		for _, m := range req.Messages {
			text.WriteString(m.Content)
			text.WriteByte('\n')
		}
		if strings.Contains(text.String(), role) && strings.Contains(text.String(), "SAFE_ALLOWED_HANDOFF_END") && strings.Contains(text.String(), "peer-message") && !strings.Contains(text.String(), rawMarker) && !strings.Contains(text.String(), botMarker) {
			composed = true
		}
	}
	if !composed {
		t.Fatal("actual next child wake did not compose real persona/handoff/privacy follow-up memory")
	}
}
