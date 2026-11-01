//go:build smoke

package smoke

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Only the upstream model is scripted. Inputs live in the real workspace and
// every operation goes through native sh, the shipped BIN and its source ref.
func TestCLISkillRuntimeBodyBudgets(t *testing.T) {
	cliSkillIsolation(t)
	model := newMCPRuntimeModel(t)
	srv := Start(t, &Stub{srv: model.server})
	const original = "SkillBudgetWorker"
	const current = "SkillBudgetMoved"
	srv.CreateBot(original, "Independent runtime skill body budget acceptance.")
	workspace := filepath.Join(srv.Root, "work", original)
	if err := os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}

	inlineBody := "INLINE_BEGIN\n" + strings.Repeat("complete inline body line\n", 900) + "INLINE_TAIL_AFTER_16K"
	inlineInput := " \r\n" + strings.ReplaceAll(inlineBody, "\n", "\r\n") + "\r\n\t "
	if len(inlineBody) <= 16<<10 || len(inlineBody) >= 28<<10 {
		t.Fatal("inline fixture must cross the old 16KiB limit and fit native Shell output")
	}
	if err := os.WriteFile(filepath.Join(workspace, "inline-body.md"), []byte(inlineInput), 0600); err != nil {
		t.Fatal(err)
	}

	// The body itself fits 64KiB; its mandatory JSON escaping pushes the final
	// reply beyond 64KiB, so deciding from unescaped body length is incorrect.
	largeBody := "ARTIFACT_BEGIN\n" + strings.Repeat("\"\\\t\n", 12000) + "ARTIFACT_FULL_TAIL"
	encodedBody, err := json.Marshal(largeBody)
	if err != nil {
		t.Fatal(err)
	}
	if len(largeBody) >= 64<<10 || len(encodedBody) <= 64<<10 {
		t.Fatal("artifact fixture must cross the serialized JSON budget only")
	}
	if err := os.WriteFile(filepath.Join(workspace, "artifact-body.md"), []byte("\r\n"+largeBody+"\r\n"), 0600); err != nil {
		t.Fatal(err)
	}

	const cue = "CLI_SKILL_BODY_BUDGETS"
	model.add(cue,
		mcpRuntimeTool("sh", map[string]string{"description": "Write the complete body from a workspace file", "command": `"$XBOT_BIN" skill write budget-inline --title 'Inline budget' --description 'Complete inline fixture' --body-file ./inline-body.md --scope self --self --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Read beyond the old routine body limit", "command": `"$XBOT_BIN" skill read budget-inline --self --json`}),
		// Establish a moved current workspace without asserting old profile
		// behavior: the next skill operation must use this same live source ref.
		mcpRuntimeTool("sh", map[string]string{"description": "Move the current source workspace for the skill artifact", "command": `"$XBOT_BIN" profile set --name ` + current + ` --self --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Write a skill body through real stdin", "command": `"$XBOT_BIN" skill write budget-artifact --title 'Artifact budget' --description 'Complete artifact fixture' --body-file - --scope self --self --json < ./artifact-body.md`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Project the large read into the current workspace", "command": `XBOT_BOT=` + shellQuote(original) + ` "$XBOT_BIN" skill read budget-artifact --self --json > ./artifact-reply.json`}),
	)
	cliSkillSend(t, srv, original, cue)
	results := model.results(cue)
	var written cliSkillWriteReply
	cliSkillDecode(t, results[0], &written)
	if !written.Written || written.Slug != "budget-inline" || written.Error != "" {
		t.Fatal("body-file write did not report the actual successful skill write")
	}
	var inline cliSkillReadReply
	cliSkillDecode(t, results[1], &inline)
	if inline.Slug != "budget-inline" || inline.Name != "budget-inline" || inline.Title != "Inline budget" || inline.Description != "Complete inline fixture" || inline.Error != "" {
		t.Fatal("inline read lost the written skill metadata")
	}
	if inline.Body == nil || *inline.Body != inlineBody || inline.File != "" {
		t.Fatal("inline read did not preserve the normalized complete body and tail beyond 16KiB")
	}
	var moved map[string]any
	cliSkillDecode(t, results[2], &moved)
	cliSkillDecode(t, results[3], &written)
	if !written.Written || written.Slug != "budget-artifact" || written.Error != "" {
		t.Fatal("stdin write did not report the actual successful skill write")
	}
	// Shell caps its transcript output at 32KiB. Observe the exact CLI stdout
	// file so a valid projection up to the contract's 64KiB is not clipped.
	if !strings.HasPrefix(results[4], "exit 0\n") {
		t.Fatal("large runtime read did not finish with a successful actual CLI exit")
	}
	stdout, err := os.ReadFile(filepath.Join(srv.Root, "work", current, "artifact-reply.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(stdout) > 64<<10 {
		t.Fatalf("actual CLI JSON exceeded 64KiB: %d bytes", len(stdout))
	}
	var large cliSkillReadReply
	if err := json.Unmarshal(stdout, &large); err != nil {
		t.Fatalf("actual large-read CLI stdout must be one JSON reply: %v", err)
	}
	if large.Slug != "budget-artifact" || large.Name != "budget-artifact" || large.Title != "Artifact budget" || large.Description != "Complete artifact fixture" || large.Error != "" {
		t.Fatal("artifact read lost the written skill metadata")
	}
	if large.Access != "local" || large.File == "" || filepath.IsAbs(large.File) || large.Preview == "" || large.Bytes != int64(len(largeBody)) {
		t.Fatal("large read did not return a local artifact, bounded preview and complete body byte count")
	}
	if large.Body != nil && *large.Body != "" {
		t.Fatal("artifact read presented a clipped inline body as a complete body")
	}
	rel := filepath.Clean(filepath.FromSlash(large.File))
	if !strings.HasPrefix(rel, "results"+string(filepath.Separator)) {
		t.Fatal("skill artifact was not stored in the existing workspace results directory")
	}
	artifactPath := filepath.Join(srv.Root, "work", current, rel)
	artifact, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatalf("read actual artifact in the current workspace: %v", err)
	}
	if !bytes.Equal(artifact, []byte(largeBody)) || int64(len(artifact)) != large.Bytes {
		t.Fatalf("artifact lost or changed the normalized full body: got %d bytes, want %d", len(artifact), len(largeBody))
	}
	if _, err := os.Stat(filepath.Join(srv.Root, "work", original, rel)); !os.IsNotExist(err) {
		t.Fatal("skill artifact used or recreated the previous source workspace")
	}
}

func TestCLISkillRuntimeBodyRefusals(t *testing.T) {
	cliSkillIsolation(t)
	model := newMCPRuntimeModel(t)
	srv := Start(t, &Stub{srv: model.server})
	const worker = "SkillRefusalWorker"
	srv.CreateBot(worker, "Independent runtime skill body refusal acceptance.")
	srv.CreateBot("SkillRefusalPeer", "Observe unchanged peer defaults after rejected writes.")
	path := filepath.Join(srv.Root, "skills", "budget-refusal", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("---\nname: budget-refusal\ndescription: Prior body\n---\n\nPRIOR_BODY_MUST_BE_REMOVED\n"), 0600); err != nil {
		t.Fatal(err)
	}
	const emptyCue = "CLI_SKILL_EXPLICIT_EMPTY_BODY"
	model.add(emptyCue, mcpRuntimeTool("sh", map[string]string{"description": "Explicitly replace a skill with an empty body", "command": `"$XBOT_BIN" skill write budget-refusal --description 'Explicit empty body' --body '' --scope self --self --json`}))
	cliSkillSend(t, srv, worker, emptyCue)
	var written cliSkillWriteReply
	cliSkillDecode(t, model.results(emptyCue)[0], &written)
	if !written.Written || written.Slug != "budget-refusal" || written.Error != "" {
		t.Fatal("an explicit empty body was not accepted as an actual write")
	}
	emptyFile, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.SplitN(string(emptyFile), "\n---\n", 2)
	if !bytes.HasPrefix(emptyFile, []byte("---\n")) || len(parts) != 2 || strings.TrimSpace(parts[1]) != "" || bytes.Contains(emptyFile, []byte("PRIOR_BODY_MUST_BE_REMOVED")) {
		t.Fatal("explicit empty body did not replace the previous body with frontmatter only")
	}

	configs, err := filepath.Glob(filepath.Join(srv.Root, "bots", "*", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(configs) < 2 {
		t.Fatal("missing actual Bot configurations for unchanged-file acceptance")
	}
	before := map[string][]byte{path: emptyFile}
	for _, config := range configs {
		raw, err := os.ReadFile(config)
		if err != nil {
			t.Fatal(err)
		}
		before[config] = raw
	}
	// Refusal diagnostics stay bounded; never dump body, source stderr or
	// configuration contents. Exact disk bytes guard against partial writes.
	refusedUnchanged := func(result string) string {
		t.Helper()
		status, _, ok := strings.Cut(result, "\n")
		if !ok || !exitLine.MatchString(status) || status == "exit 0" {
			t.Fatal("invalid skill input did not produce a real nonzero CLI exit")
		}
		output := commandOutput(result)
		if strings.TrimSpace(output) == "" || len(output) > 64<<10 {
			t.Fatalf("refusal needs a bounded diagnostic: %d output bytes", len(output))
		}
		for file, want := range before {
			got, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("rejected operation changed %s", filepath.Base(file))
			}
		}
		return output
	}

	const missingCue = "CLI_SKILL_MISSING_BODY"
	model.add(missingCue, mcpRuntimeTool("sh", map[string]string{"description": "Reject a write without an explicit body source", "command": `"$XBOT_BIN" skill write budget-refusal --description 'Must not commit missing body' --scope self --self --json`}))
	cliSkillSend(t, srv, worker, missingCue)
	refusedUnchanged(model.results(missingCue)[0])

	// Raw input is below 1MiB, while mandatory quote/backslash escaping alone
	// exceeds it. This must be refused using the final serialized DTO size.
	escapedBody := strings.Repeat("\"\\", 300<<10)
	encodedBody, err := json.Marshal(escapedBody)
	if err != nil {
		t.Fatal(err)
	}
	if len(escapedBody) >= 1<<20 || len(encodedBody) <= 1<<20 {
		t.Fatal("write fixture must exceed the wire budget only after JSON escaping")
	}
	workspace := filepath.Join(srv.Root, "work", worker)
	if err := os.WriteFile(filepath.Join(workspace, "escaped-body.md"), []byte(escapedBody), 0600); err != nil {
		t.Fatal(err)
	}
	const wireCue = "CLI_SKILL_ESCAPED_WIRE_OVERFLOW"
	model.add(wireCue, mcpRuntimeTool("sh", map[string]string{"description": "Refuse a final JSON write larger than one MiB", "command": `"$XBOT_BIN" skill write budget-refusal --description 'Must not commit oversized wire' --body-file ./escaped-body.md --scope self --self --json`}))
	cliSkillSend(t, srv, worker, wireCue)
	refusedUnchanged(model.results(wireCue)[0])

	const rawLimit = 20 << 20
	header := "---\nname: budget-raw-overflow\ndescription: Raw file limit fixture\n---\n\n"
	huge := []byte(header + strings.Repeat("x", rawLimit+1-len(header)))
	hugePath := filepath.Join(srv.Root, "skills", "budget-raw-overflow", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(hugePath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hugePath, huge, 0600); err != nil {
		t.Fatal(err)
	}
	const rawCue = "CLI_SKILL_RAW_FILE_OVERFLOW"
	model.add(rawCue, mcpRuntimeTool("sh", map[string]string{"description": "Refuse a raw skill file at the twenty MiB limit plus one", "command": `"$XBOT_BIN" skill read budget-raw-overflow --self --json`}))
	cliSkillSend(t, srv, worker, rawCue)
	diagnostic := strings.ToLower(refusedUnchanged(model.results(rawCue)[0]))
	if !strings.Contains(diagnostic, "large") && !strings.Contains(diagnostic, "limit") && !strings.Contains(diagnostic, "exceed") && !strings.Contains(diagnostic, "size") && !strings.Contains(diagnostic, "20971520") && !strings.Contains(diagnostic, "too big") {
		t.Fatal("oversized raw skill refusal did not clearly identify a size or budget error")
	}
}

// Exercise the model's actual unredirected Shell result, whose budget is lower
// than the protocol's 64KiB ceiling. A successful read must retain a usable full
// body reference; a valid HTTP reply clipped by the Shell is not a usable result.
func TestCLISkillRuntimeShellOutputBudget(t *testing.T) {
	cliSkillIsolation(t)
	model := newMCPRuntimeModel(t)
	srv := Start(t, &Stub{srv: model.server})
	const worker, slug = "SkillShellBudgetWorker", "shell-output-budget"
	const tail = "SHELL_FULL_BODY_TAIL"
	body := strings.Repeat("a", (40<<10)-len(tail)) + tail
	path := filepath.Join(srv.Root, "skills", slug, "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("---\nname: "+slug+"\ntitle: Shell full body\ndescription: Actual model Shell result fixture\n---\n\n"+body+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	srv.CreateBot(worker, "Read a complete skill through the model's normal Shell result.")
	const cue = "CLI_SKILL_ACTUAL_SHELL_OUTPUT_BUDGET"
	model.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "Read the complete skill through the normal source Shell output", "command": `"$XBOT_BIN" skill read ` + slug + ` --self --json`}))
	cliSkillSend(t, srv, worker, cue)
	result := model.results(cue)[0]
	if !strings.HasPrefix(result, "exit 0\n") {
		t.Fatalf("actual model Shell read failed: %.256q", result)
	}
	raw := []byte(commandOutput(result))
	if len(raw) > 64<<10 {
		t.Fatalf("actual model skill JSON exceeded protocol64KiB: %d bytes", len(raw))
	}
	var reply cliSkillReadReply
	if err := json.Unmarshal(raw, &reply); err != nil {
		t.Fatalf("successful unredirected model Shell did not receive complete skill JSON: %v (output bytes=%d)", err, len(raw))
	}
	if reply.Slug != slug || reply.Error != "" || reply.Access != "local" || reply.File == "" || reply.Bytes != int64(len(body)) {
		t.Fatal("actual model read lost its complete-body local artifact reference")
	}
	rel := filepath.Clean(filepath.FromSlash(reply.File))
	if filepath.IsAbs(rel) || !strings.HasPrefix(rel, "results"+string(filepath.Separator)) {
		t.Fatal("model read artifact is not in the original current workspace results")
	}
	full, err := os.ReadFile(filepath.Join(srv.Root, "work", worker, rel))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(full, []byte(body)) || !bytes.HasSuffix(full, []byte(tail)) {
		t.Fatal("model read artifact lost complete body or its final tail")
	}
}
