package kernel

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/browser"
	"xbot/internal/code"
	"xbot/internal/desktop"
	"xbot/internal/link"
	"xbot/internal/provider"
	"xbot/internal/review"
	"xbot/internal/tools"
	"xbot/internal/vault"
)

// fakeDangerTool claims it does not need confirmation. The kernel must still
// ask, because the tool name and arguments say it deletes a file.
type fakeDangerTool struct{ ran *bool }

func (fakeDangerTool) Name() string        { return "delete_file" }
func (fakeDangerTool) Description() string { return "" }
func (fakeDangerTool) Parameters() any     { return map[string]any{} }
func (fakeDangerTool) Confirm(json.RawMessage) (bool, string, error) {
	return false, "delete notes.txt", nil
}
func (t fakeDangerTool) Run(context.Context, json.RawMessage) (string, error) {
	if t.ran != nil {
		*t.ran = true
	}
	return "deleted", nil
}

type fakeLink struct{ tool tools.Tool }

func (fakeLink) Name() string          { return "fakedanger" }
func (l fakeLink) Tools() []tools.Tool { return []tools.Tool{l.tool} }

func TestUntrustedShellMemoryWriteRequiresApproval(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	dir := filepath.Join(b.Root, "people", "owner", "memory")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	k := &Kernel{Bot: b}
	k.ensureTurn().turnSender = "owner"
	args, err := json.Marshal(map[string]string{
		"command": "cd " + filepath.Dir(dir) + " && printf x >> memory/memory.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	tool := reviewedTool{Tool: &tools.Shell{Dir: b.Root}, k: k}
	need, _, err := tool.Confirm(args)
	if err != nil || !need {
		t.Fatalf("untrusted shell memory write: approval=%v err=%v, want approval=true", need, err)
	}
	read, _ := json.Marshal(map[string]string{"command": "cat " + filepath.Join(dir, "memory.md")})
	if need, _, err := tool.Confirm(read); err != nil || need {
		t.Fatalf("untrusted read-only shell: approval=%v err=%v, want approval=false", need, err)
	}
	k.ensureTurn().turnTrusted = true
	need, _, err = tool.Confirm(args)
	if err != nil || need {
		t.Fatalf("trusted shell memory write: approval=%v err=%v, want approval=false", need, err)
	}
}

func TestReviewedToolForcesDangerousConfirm(t *testing.T) {
	need, summary, err := reviewedTool{Tool: fakeDangerTool{}}.Confirm(json.RawMessage(`{"path":"notes.txt"}`))
	if err != nil || !need || summary != "delete notes.txt" {
		t.Fatalf("delete need=%v summary=%q err=%v", need, summary, err)
	}
	need, _, err = reviewedTool{Tool: fakeDangerTool{}}.Confirm(nil)
	if err != nil || !need {
		t.Fatalf("missing args need=%v err=%v", need, err)
	}
	read := reviewedTool{Tool: &tools.ReadFile{}}
	need, _, err = read.Confirm(json.RawMessage(`{"path":"notes.txt"}`))
	if err != nil || need {
		t.Fatalf("read need=%v err=%v", need, err)
	}
}

func TestKernelReviewForcesAndLogsAllowed(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	var ran bool
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, Content: provider.ToolContent([]provider.ToolCall{{
			ID: "c1", Type: "function",
			Function: provider.FunctionCall{Name: "delete_file", Arguments: `{"path":"notes.txt"}`},
		}})},
		{Role: provider.RoleAssistant, Content: provider.TextContent("done")},
	}}
	var gotTool, gotSummary string
	k := &Kernel{
		Bot: b, Provider: p, MaxSteps: 4,
		Links: []link.Link{link.Terminal{}, fakeLink{fakeDangerTool{ran: &ran}}},
		Approve: func(_ context.Context, tool, summary string) (bool, error) {
			gotTool, gotSummary = tool, summary
			return true, nil
		},
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "delete", time.Unix(1, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	if !ran {
		t.Fatal("dangerous tool did not run after approval")
	}
	if gotTool != "delete_file" || gotSummary != "delete notes.txt" {
		t.Fatalf("approval tool=%q summary=%q", gotTool, gotSummary)
	}
	decisions := readDecisions(t, b)
	if len(decisions) != 1 {
		t.Fatalf("decisions %+v", decisions)
	}
	d := decisions[0]
	if d.State != bot.DecisionAllowed || d.Tool != "delete_file" || d.Summary != "delete notes.txt" || d.Time.IsZero() {
		t.Fatalf("decision %+v", d)
	}
	info, err := os.Stat(b.DecisionPath())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("decision file mode %v", info.Mode().Perm())
	}
}

func TestKernelReviewLogsDeclined(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	var ran bool
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, Content: provider.ToolContent([]provider.ToolCall{{
			ID: "c1", Type: "function",
			Function: provider.FunctionCall{Name: "delete_file", Arguments: `{"path":"notes.txt"}`},
		}})},
		{Role: provider.RoleAssistant, Content: provider.TextContent("stopped")},
	}}
	k := &Kernel{
		Bot: b, Provider: p, MaxSteps: 4,
		Links:   []link.Link{link.Terminal{}, fakeLink{fakeDangerTool{ran: &ran}}},
		Approve: func(context.Context, string, string) (bool, error) { return false, nil },
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "delete", time.Unix(1, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	if ran {
		t.Fatal("declined tool ran")
	}
	decisions := readDecisions(t, b)
	if len(decisions) != 1 || decisions[0].State != bot.DecisionDeclined {
		t.Fatalf("decisions %+v", decisions)
	}
}

func TestKernelReadOnlyDoesNotAsk(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/notes.txt"
	if err := os.WriteFile(path, []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}
	b := testBot(t, link.NameTerminal, link.NameTools)
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, Content: provider.ToolContent([]provider.ToolCall{{
			ID: "c1", Type: "function",
			Function: provider.FunctionCall{Name: "read_file", Arguments: `{"path":"notes.txt"}`},
		}})},
		{Role: provider.RoleAssistant, Content: provider.TextContent("read")},
	}}
	asked := 0
	k := &Kernel{
		Bot: b, Provider: p, MaxSteps: 4,
		Links:   []link.Link{link.Terminal{}, link.NewToolsLinkAt(dir, "")},
		Approve: func(context.Context, string, string) (bool, error) { asked++; return true, nil },
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "read", time.Unix(1, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	if asked != 0 {
		t.Fatalf("read-only asked %d times", asked)
	}
	if decisions := readDecisions(t, b); len(decisions) != 0 {
		t.Fatalf("read-only logged %+v", decisions)
	}
}

func TestRecordDecisionRedactsSecrets(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	const secret = "super-secret-value"
	k := &Kernel{Bot: b, Redact: func(s string) string {
		return strings.ReplaceAll(s, secret, "[secret]")
	}}
	k.RecordDecision(bot.DecisionAllowed, "http_request", "GET https://x/?k="+secret)
	decisions := readDecisions(t, b)
	if len(decisions) != 1 {
		t.Fatalf("decisions %+v", decisions)
	}
	if strings.Contains(decisions[0].Summary, secret) || !strings.Contains(decisions[0].Summary, "[secret]") {
		t.Fatalf("summary %q", decisions[0].Summary)
	}
}

func TestReviewedToolHTTPReview(t *testing.T) {
	tool := reviewedTool{Tool: &tools.HTTPRequest{}}
	if need, _, err := tool.Confirm(json.RawMessage(`{"url":"https://example.com"}`)); err != nil || need {
		t.Fatalf("plain GET need=%v err=%v", need, err)
	}
	if need, _, err := tool.Confirm(json.RawMessage(`{"method":"POST","url":"https://example.com","body":"x"}`)); err != nil || !need {
		t.Fatalf("POST need=%v err=%v", need, err)
	}
	if need, _, err := tool.Confirm(json.RawMessage(`{"url":"https://example.com/?k={{secret:TOKEN}}"}`)); err != nil || !need {
		t.Fatalf("secret GET need=%v err=%v", need, err)
	}
}

// fakePeerTool stands in for message_bot: it never asks on its own, and the
// kernel must not force a card either, because the message stays inside the
// owner's own bots.
type fakePeerTool struct{}

func (fakePeerTool) Name() string                                         { return "message_bot" }
func (fakePeerTool) Description() string                                  { return "" }
func (fakePeerTool) Parameters() any                                      { return map[string]any{} }
func (fakePeerTool) Confirm(json.RawMessage) (bool, string, error)        { return false, "", nil }
func (fakePeerTool) Run(context.Context, json.RawMessage) (string, error) { return "sent", nil }

func TestReviewedToolDoesNotForceBotToBot(t *testing.T) {
	need, _, err := reviewedTool{Tool: fakePeerTool{}}.Confirm(json.RawMessage(`{"bot":"bob","text":"hi"}`))
	if err != nil || need {
		t.Fatalf("message_bot need=%v err=%v", need, err)
	}
}

func TestRoutineSelfEditsDoNotAsk(t *testing.T) {
	for _, tool := range []tools.Tool{routineCreate{}, routineUpdate{}, routineDelete{}} {
		need, _, err := reviewedTool{Tool: tool}.Confirm(json.RawMessage(`{"id":"r1","name":"n","prompt":"p","enabled":false}`))
		if err != nil || need {
			t.Fatalf("%s need=%v err=%v", tool.Name(), need, err)
		}
	}
}

func TestPersonaAndProfileSelfEditsStillAsk(t *testing.T) {
	persona := reviewedTool{Tool: personaTool{}}
	if need, _, err := persona.Confirm(json.RawMessage(`{"text":"new persona"}`)); err != nil || !need {
		t.Fatalf("propose_persona need=%v err=%v", need, err)
	}
	profile := reviewedTool{Tool: profileTool{}}
	if need, _, err := profile.Confirm(json.RawMessage(`{"description":"notes"}`)); err != nil || !need {
		t.Fatalf("propose_profile need=%v err=%v", need, err)
	}
}

// A direct write_file or edit_file to this bot's own persona or profile file
// still asks in allow mode, while an ordinary path does not. The tool only
// reports the path; the kernel review path decides.
func TestWriteEditSelfEditStillAsks(t *testing.T) {
	b := testBot(t, link.NameTerminal, link.NameTools)
	k := &Kernel{
		Bot: b, Links: []link.Link{link.NewToolsLink()},
		Machine: func() (string, error) { return "allow", nil },
	}
	list, err := k.modelTools()
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]tools.Tool{}
	for _, tool := range list {
		byName[tool.Name()] = tool
	}
	dir := bot.Dir(b.Root, b.Name())
	for _, tc := range []struct {
		tool string
		args string
	}{
		{"write_file", `{"path":"` + b.PersonaPath() + `","content":"new"}`},
		{"write_file", `{"path":"` + filepath.Join(dir, "config.json") + `","content":"{}"}`},
		{"edit_file", `{"path":"` + b.PersonaPath() + `","old_string":"a","new_string":"b"}`},
	} {
		tool := byName[tc.tool]
		if tool == nil {
			t.Fatalf("%s missing", tc.tool)
		}
		if need, _, err := tool.Confirm(json.RawMessage(tc.args)); err != nil || !need {
			t.Fatalf("%s %s need=%v err=%v", tc.tool, tc.args, need, err)
		}
	}
	normal := json.RawMessage(`{"path":"` + filepath.Join(t.TempDir(), "note.txt") + `","content":"x"}`)
	if need, _, err := byName["write_file"].Confirm(normal); err != nil || need {
		t.Fatalf("normal write need=%v err=%v", need, err)
	}
}

// fakeMCPLink stands in for an MCP server: its tool name is dynamic and only
// covered by the mcp_* rule.
type fakeMCPLink struct{}

func (fakeMCPLink) Name() string        { return "mcp:fake" }
func (fakeMCPLink) Tools() []tools.Tool { return []tools.Tool{fakeMCPTool{}} }

type fakeMCPTool struct{}

func (fakeMCPTool) Name() string                                         { return "mcp_fake_echo" }
func (fakeMCPTool) Description() string                                  { return "" }
func (fakeMCPTool) Parameters() any                                      { return map[string]any{} }
func (fakeMCPTool) Confirm(json.RawMessage) (bool, string, error)        { return false, "", nil }
func (fakeMCPTool) Run(context.Context, json.RawMessage) (string, error) { return "", nil }

// TestEveryRegisteredToolHasATableEntryAndNeverConfirms builds a kernel with
// every link the product can mount, then fails if a registered tool has no
// explicit policy-table entry or if that tool's own Confirm asks for the owner.
func TestEveryRegisteredToolHasATableEntryAndNeverConfirms(t *testing.T) {
	b := testBot(t, link.NameTerminal, link.NameTools, link.NameTimer, link.NameMemory, link.NamePersona,
		link.NameContext, link.NameAgents, link.NameSkills, link.NameWeb, link.NamePeers,
		link.NameWebhook, link.NameWatch, link.NameTelegram, link.NameLark,
		link.NameCoding, link.NameDesktop, link.NameBrowser)
	if err := bot.SetPrimary(b.Root, b.Name()); err != nil {
		t.Fatal(err)
	}
	store, err := vault.Open(b.VaultDir(), b.VaultKeyPath())
	if err != nil {
		t.Fatal(err)
	}
	dir := bot.Dir(b.Root, b.Name())
	k := &Kernel{
		Bot:   b,
		Links: []link.Link{link.Terminal{}, &link.Timer{Dir: b.RoutineDir()}, link.Memory{}, link.Persona{}, link.Context{}, link.Agents{}, link.Skills{}, link.Web{}, link.Peers{}, link.Webhook{}, link.Watch{}, link.NewToolsLinkAt(t.TempDir(), "")},
		Vault: store,
	}
	k.Links = append(k.Links,
		link.NewConnector(dir, b.SecretPath(), link.NameTelegram),
		link.NewConnector(dir, b.SecretPath(), link.NameLark),
		code.Open(dir, b.InboxPath()),
		desktop.Open(b.Root),
		browser.Open(b.Root),
		fakeMCPLink{},
	)
	for _, name := range []string{link.NameWebhook, link.NameWatch, link.NameSkills, link.NameTelegram,
		link.NameLark, link.NameCoding, link.NameDesktop, link.NameBrowser, "mcp:fake"} {
		k.markLoaded(name)
	}
	list, err := k.modelTools()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, tool := range list {
		rt, ok := tool.(reviewedTool)
		if !ok {
			t.Fatalf("%s is not a reviewedTool", tool.Name())
		}
		name := rt.Tool.Name()
		seen[name] = true
		if !review.Known(name) {
			t.Fatalf("tool %q has no explicit policy-table entry", name)
		}
		// The browser link keeps its own local machine adapter because its
		// package tests are frozen. The kernel review path still overrides its
		// decision, so browser tools are covered by the table-entry check above
		// but not by the "Confirm never decides" check here.
		if strings.HasPrefix(name, "browser_") {
			continue
		}
		if need, _, _ := rt.Tool.Confirm(nil); need {
			t.Fatalf("tool %q answered need=true on its own; the table must decide", name)
		}
	}
	for _, want := range []string{"shell", "read_file", "write_file", "edit_file", "web_fetch", "web_search", "view_image", "http_request", "list_secrets",
		"links", "telegram_send", "lark_send", "code_start", "desktop_click", "browser_open",
		"create_bot", "delete_bot", "bot_status", "mcp_fake_echo"} {
		if !seen[want] {
			t.Fatalf("expected tool %q was not registered", want)
		}
	}
}

func readDecisions(t *testing.T, b *bot.Bot) []bot.Decision {
	t.Helper()
	out, err := b.Decisions()
	if err != nil {
		t.Fatal(err)
	}
	return out
}
