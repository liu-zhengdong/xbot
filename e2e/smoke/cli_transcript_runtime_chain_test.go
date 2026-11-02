//go:build smoke

package smoke

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"xbot/internal/bot"
)

func TestCLITranscriptRuntimeSourceConsumers(t *testing.T) {
	cliSkillIsolation(t)
	model := newCLITranscriptModel(t)
	srv := Start(t, &Stub{srv: model.server})
	const source = "TranscriptChainSource"
	srv.CreateBot(source, "Recover original recorded tool evidence by stable transcript sequence without executing the tool again.")
	var primary struct{ Name string }
	raw, err := os.ReadFile(filepath.Join(srv.Root, "primary.json"))
	if err != nil || json.Unmarshal(raw, &primary) != nil || primary.Name == "" || primary.Name == source {
		t.Fatal("transcript chain source must be actually nonprimary")
	}
	const warm = "CTX_SOURCE_WARM_A13"
	model.base.add(warm, mcpRuntimeTool("sh", map[string]string{"description": "Establish the actual original source workspace before private inputs", "command": `printf 'transcript-source-ready\n'`}))
	cliSkillSend(t, srv, source, warm)
	if got := model.base.results(warm); len(got) != 1 || !strings.HasPrefix(got[0], "exit 0\n") {
		t.Fatal("original source Native Shell workspace was not established")
	}
	workspace := filepath.Join(srv.Root, "work", source)
	const needle = "ctxpair7zeta"
	payload := needle + "\n" + strings.Repeat("The original paired evidence must be recovered without re-executing this command. ", 64) + "\nPAIRED_ORIGINAL_RESULT_END\n"
	if len(payload) <= 4096 || len(payload)+1024 >= 8<<10 {
		t.Fatal("source payload must exercise actual transcript spilling while fitting original expanded semantic cap")
	}
	for name, body := range map[string]string{"transcript-payload.txt": payload, "transcript-query.txt": needle, "transcript-execution-count.txt": "0\n"} {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	const execute = "CTX_ORIGINAL_TOOL_B26"
	originalCommand := `transcript_count=$(cat transcript-execution-count.txt)` + "\n" + `printf '%s\n' "$((transcript_count + 1))" > transcript-execution-count.txt` + "\n" + `cat transcript-payload.txt`
	model.base.add(execute, mcpRuntimeTool("sh", map[string]string{"description": "Execute the unique original evidence-producing tool exactly once", "command": originalCommand}))
	cliSkillSend(t, srv, source, execute)
	produced := model.base.results(execute)
	if len(produced) != 1 || !strings.HasPrefix(produced[0], "exit 0\n") || !strings.Contains(produced[0], strings.TrimSuffix(payload, "\n")) {
		t.Fatal("actual original Native Shell did not return its complete unique payload")
	}
	counter := func() {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(workspace, "transcript-execution-count.txt"))
		if err != nil || strings.TrimSpace(string(raw)) != "1" {
			t.Fatal("the original evidence-producing tool did not execute exactly once")
		}
	}
	counter()
	const search = "CTX_SEARCH_PRESEEDED_C39"
	// The needle exists only in the private query file and original payload.
	// Neither this command nor the owner cue adds a new FTS match for it.
	searchCommand := `env -u XBOT_CALL_ID XBOT_BOT=` + primary.Name + ` "$XBOT_BIN" transcript search --self --query-file transcript-query.txt --json`
	if strings.Contains(searchCommand, needle) {
		t.Fatal("query command polluted the actual FTS needle")
	}
	model.base.add(search, mcpRuntimeTool("sh", map[string]string{"description": "Find the original real tool-result sequence through source FTS without echoing its query into the transcript", "command": searchCommand}))
	cliSkillSend(t, srv, source, search)
	var found cliTranscriptReadReply
	searchResults := model.base.results(search)
	if len(searchResults) != 1 {
		t.Fatal("source did not answer the actual search")
	}
	cliBotDecode(t, searchResults[0], &found)
	if found.Error != "" || found.File != "" || found.Text == "" || len(found.Text) > 4<<10 {
		t.Fatal("source search lost its original bounded semantic text")
	}
	toolSeq := 0
	for _, line := range strings.Split(found.Text, "\n") {
		parts := strings.SplitN(line, " ", 3)
		if len(parts) == 3 && parts[1] == "tool:" && strings.Contains(parts[2], needle) {
			seq, err := strconv.Atoi(parts[0])
			if err == nil && seq > 0 {
				if toolSeq != 0 {
					t.Fatal("unique original tool result was ambiguously selected")
				}
				toolSeq = seq
			}
		}
	}
	if toolSeq == 0 {
		t.Fatalf("actual source search did not expose the matching RoleTool seq: %.900q", found.Text)
	}
	actual, err := bot.Load(srv.Root, source)
	if err != nil {
		t.Fatal(err)
	}
	part, err := actual.Slice(toolSeq, toolSeq)
	if err != nil || len(part) != 1 || part[0].Message.Role != "tool" || part[0].Message.CallID() != execute+"_0" || part[0].Blob == "" {
		t.Fatal("returned seq was not the original spilled tool result paired to the unique source call")
	}
	pairedCall := false
	if err := actual.Walk(1, toolSeq, func(_ int, e bot.Entry) error {
		for _, call := range e.Message.Calls() {
			if call.ID == execute+"_0" && call.Function.Name == "sh" {
				var args struct{ Command string }
				if json.Unmarshal([]byte(call.Function.Arguments), &args) == nil && args.Command == originalCommand {
					pairedCall = true
				}
			}
		}
		return nil
	}); err != nil || !pairedCall {
		t.Fatal("original transcript did not retain the exact matching source tool call and arguments")
	}
	const expand = "CTX_EXPAND_PAIRED_D42"
	model.base.add(expand, mcpRuntimeTool("sh", map[string]string{"description": "Expand exactly the returned original tool-result seq without executing it again", "command": `env -u XBOT_CALL_ID "$XBOT_BIN" transcript expand --self --from ` + strconv.Itoa(toolSeq) + ` --to ` + strconv.Itoa(toolSeq) + ` --json`}))
	cliSkillSend(t, srv, source, expand)
	expandedResults := model.base.results(expand)
	if len(expandedResults) != 1 {
		t.Fatal("source did not answer the actual paired expansion")
	}
	var expanded cliTranscriptReadReply
	cliBotDecode(t, expandedResults[0], &expanded)
	if expanded.Error != "" || expanded.File != "" || !strings.HasPrefix(expanded.Text, "# seq "+strconv.Itoa(toolSeq)+" tool\n") || !strings.Contains(expanded.Text, strings.TrimSuffix(payload, "\n")) || len(expanded.Text) > 8<<10 {
		t.Fatal("paired expansion did not hydrate the complete original tool result within its original8KiB semantics")
	}
	counter()
	if after, err := os.ReadFile(filepath.Join(workspace, "transcript-payload.txt")); err != nil || string(after) != payload {
		t.Fatal("reading the recorded transcript changed the original payload file")
	}
}
