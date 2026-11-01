//go:build smoke

package smoke

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/fsutil"
	"xbot/internal/memory"
)

func TestCLIBotForkRuntimeLateSourceAndGroup(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires unprivileged Unix directory permissions")
	}
	cliSkillIsolation(t)
	const source, firstChild, secondChild = "ForkLateSource", "ForkLateNoteChild", "ForkLateGroupChild"
	const firstRole, secondRole = "Review the first actual late source-note failure.", "Review the later optional-group failure."
	const handoff = "LATE_HANDOFF_PUBLIC: this complete short handoff must survive both later filesystem failures."
	model := newCLIBotForkModel(t, handoff)
	srv := cliBotForkPartialSource(t, model, source)
	memoryDir := filepath.Join(srv.Root, "bots", source, "memory")
	groupsDir := filepath.Join(srv.Root, "groups")
	if err := os.MkdirAll(groupsDir, 0700); err != nil {
		t.Fatal(err)
	}
	// Finish persistent fixture preparation before the first current-resource
	// capture. Both later faults are mode changes inside the real source Shell.
	srv.Restart()
	memoryInfo, err := os.Stat(memoryDir)
	if err != nil {
		t.Fatal(err)
	}
	groupsInfo, err := os.Stat(groupsDir)
	if err != nil {
		t.Fatal(err)
	}
	memoryMode, groupMode := memoryInfo.Mode().Perm(), groupsInfo.Mode().Perm()
	if !memoryInfo.IsDir() || !groupsInfo.IsDir() || memoryMode&0200 == 0 || groupMode&0200 == 0 {
		t.Fatal("actual source memory and groups fixture must start as writable directories")
	}
	memoryPath := filepath.Join(memoryDir, "memory.md")
	indexPath := filepath.Join(memoryDir, "memory.index.json")
	mdInfo, err := os.Stat(memoryPath)
	if err != nil {
		t.Fatal(err)
	}
	idxInfo, err := os.Stat(indexPath)
	if err != nil || idxInfo.ModTime().Before(mdInfo.ModTime()) {
		t.Fatal("source Recall must be able to read the current index without an earlier repair write")
	}
	lockInfo, err := os.Stat(filepath.Join(memoryDir, ".lock"))
	if err != nil || !lockInfo.Mode().IsRegular() || lockInfo.Mode().Perm()&0200 == 0 {
		t.Fatal("actual source memory must already have its writable lock file")
	}
	beforeMD, err := os.ReadFile(memoryPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeIndex, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	readFacts := func(name string) map[string]memory.Fact {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(srv.Root, "bots", name, "memory", "memory.index.json"))
		if err != nil {
			t.Fatal(err)
		}
		var all []memory.Fact
		if err := json.Unmarshal(raw, &all); err != nil {
			t.Fatal(err)
		}
		facts := make(map[string]memory.Fact)
		for _, fact := range all {
			if _, exists := facts[fact.Key]; exists {
				t.Fatal("actual memory index duplicated a fact key")
			}
			facts[fact.Key] = fact
		}
		return facts
	}
	checkChild := func(name, role string) {
		t.Helper()
		src, err := bot.Load(srv.Root, source)
		if err != nil {
			t.Fatal(err)
		}
		child, err := bot.Load(srv.Root, name)
		if err != nil {
			t.Fatal("late error removed the actual already-created child")
		}
		parentConfig, cfg := src.Config(), child.Config()
		if cfg.Name != name || cfg.Persona != role || !slices.Equal(cfg.Owners, parentConfig.Owners) || !slices.Equal(cfg.Links, parentConfig.Links) || cfg.Provider != parentConfig.Provider || cfg.Model != parentConfig.Model || cfg.ContextLimit != parentConfig.ContextLimit {
			t.Fatal("late failure lost confirmed child owners, links, binding or explicit role")
		}
		persona, err := child.ReadPersona()
		if err != nil || strings.TrimSpace(persona) != role {
			t.Fatal("late failure changed the committed child persona")
		}
		childRules, err := os.ReadFile(filepath.Join(srv.Root, "bots", name, "rules.md"))
		if err != nil {
			t.Fatal(err)
		}
		parentRules, err := os.ReadFile(filepath.Join(srv.Root, "bots", source, "rules.md"))
		if err != nil || !bytes.Equal(childRules, parentRules) {
			t.Fatal("late failure lost confirmed child rules")
		}
		state, err := child.LoadState()
		if err != nil || !reflect.DeepEqual(state, bot.State{}) {
			t.Fatal("late failure copied source state or started child model work")
		}
		entries, err := child.Entries()
		if err != nil || len(entries) != 0 {
			t.Fatal("late failure copied source history or appended a child conversation")
		}
		facts := readFacts(name)
		if len(facts) != 2 || facts["handoff"].Text != handoff || facts["fork-source"].Text == "" || !strings.Contains(facts["fork-source"].Text, "You cannot read "+source+"'s transcript.") {
			t.Fatal("late failure lost complete handoff or the child's persisted source/privacy acquaintance note")
		}
		for _, fact := range facts {
			if fact.Scope != memory.ScopeBot || fact.Tier != memory.TierProfile || fact.Kind != memory.KindWork || fact.BotName != name || fact.WrittenBy != source || fact.Time.IsZero() {
				t.Fatal("late failure changed actual child memory scope or provenance")
			}
		}
		if err := fsutil.ReadJSONL(child.InboxPath(), func(raw []byte) error {
			var record struct{ Op string }
			if err := json.Unmarshal(raw, &record); err != nil {
				return err
			}
			if record.Op == "post" {
				t.Fatal("late fork stages posted an unsolicited event to the new child")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	assertNoGroup := func() {
		t.Helper()
		entries, err := os.ReadDir(groupsDir)
		if err != nil || len(entries) != 0 {
			t.Fatal("failed or not-yet-reached group stage persisted group metadata")
		}
	}
	run := func(cue, child, role, path string, originalMode os.FileMode) cliBotForkReply {
		t.Helper()
		restore := fmt.Sprintf("chmod %04o ", originalMode) + shellQuote(path)
		// The trap covers abnormal shell exit; normal execution explicitly
		// restores the exact original mode before returning the BIN exit code.
		command := "trap " + shellQuote(restore) + " 0\n" +
			"chmod 0500 " + shellQuote(path) + " || exit $?\n" +
			`"$XBOT_BIN" bot-fork ` + child + ` --self --role ` + shellQuote(role) + ` --group 'Late actual group' --json` + "\n" +
			"fork_status=$?\n" + restore + " || exit $?\ntrap - 0\nexit \"$fork_status\""
		model.base.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "Apply and restore an actual directory permission fault around the source fork CLI", "command": command}))
		cliSkillSend(t, srv, source, cue)
		results := model.base.results(cue)
		if len(results) != 1 || results[0] == "" || len(commandOutput(results[0])) > 32<<10 {
			t.Fatal("late fork did not produce one bounded actual native Shell result")
		}
		exit, _, ok := strings.Cut(results[0], "\n")
		if !ok || !exitLine.MatchString(exit) || exit == "exit 0" {
			t.Fatal("actual late filesystem failure did not return nonzero")
		}
		var reply cliBotForkReply
		if err := json.NewDecoder(strings.NewReader(commandOutput(results[0]))).Decode(&reply); err != nil {
			t.Fatal("late failure lost its first complete typed JSON partial before stderr")
		}
		if reply.Name != child || !reply.Created || reply.CreateStage != "" || reply.HandoffParts != 1 || reply.HandoffTotal != 1 || reply.GroupID != "" || reply.Error == "" {
			t.Fatal("late failure hid committed child/complete handoff facts or claimed a group")
		}
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() || info.Mode().Perm() != originalMode {
			t.Fatal("actual native Shell did not restore the exact original faulted directory mode")
		}
		return reply
	}
	assertNoGroup()
	first := run("CLI_FORK_LATE_SOURCE_NOTE", firstChild, firstRole, memoryDir, memoryMode)
	if first.Stage != "source_note" || !slices.Equal(first.Confirmed, []string{"owners", "binding", "rules", "handoff", "child_note"}) || first.UncertainMemory == nil || first.UncertainMemory.Bot != source || first.UncertainMemory.Key != "fork-"+firstChild {
		t.Fatal("source-note partial did not preserve confirmed child work/current uncertain source-memory identity")
	}
	checkChild(firstChild, firstRole)
	firstSourceFacts := readFacts(source)
	if _, exists := firstSourceFacts["fork-"+firstChild]; exists {
		t.Fatal("source-note permission failure nevertheless persisted a reciprocal source note")
	}
	afterMD, err := os.ReadFile(memoryPath)
	if err != nil || !bytes.Equal(beforeMD, afterMD) {
		t.Fatal("source-note failure changed physical source Markdown before its failed atomic write")
	}
	afterIndex, err := os.ReadFile(indexPath)
	if err != nil || !bytes.Equal(beforeIndex, afterIndex) {
		t.Fatal("source-note failure changed the source index")
	}
	assertNoGroup()
	second := run("CLI_FORK_LATE_GROUP_META", secondChild, secondRole, groupsDir, groupMode)
	if second.Stage != "group" || !slices.Equal(second.Confirmed, []string{"owners", "binding", "rules", "handoff", "child_note", "source_note"}) || second.UncertainMemory != nil {
		t.Fatal("group failure lost completed reciprocal memory facts or fabricated uncertainty")
	}
	checkChild(secondChild, secondRole)
	checkChild(firstChild, firstRole)
	finalSourceFacts := readFacts(source)
	if note, exists := finalSourceFacts["fork-"+secondChild]; !exists || !strings.Contains(note.Text, secondChild) || !strings.Contains(note.Text, "They cannot read your transcript.") || note.WrittenBy != source || note.BotName != source || note.Scope != memory.ScopeBot {
		t.Fatal("group failure lost the actually committed source reciprocal/privacy note")
	}
	if _, exists := finalSourceFacts["fork-"+firstChild]; exists {
		t.Fatal("later group failure retried the earlier failed source acquaintance note")
	}
	assertNoGroup()
	model.mu.Lock()
	archive := append([]cliBotForkArchiveRequest(nil), model.archive...)
	model.mu.Unlock()
	if len(archive) != 2 {
		t.Fatal("two real late-failing forks did not use exactly two original handoff model calls")
	}
	for _, request := range archive {
		if len(request.Tools) != 0 || request.Model == "" {
			t.Fatal("late forks bypassed original no-tools compaction Chat")
		}
	}
}
