package kernel

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/agent"
	"xbot/internal/exec"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/notable"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

func TestDispatchReportsRealScopedOutsideWrite(t *testing.T) {
	hold := make(chan struct{})
	model := &roleModel{hold: hold,
		fore: []provider.Message{assistantCall("d", "", "dispatch", `{"type":"dev","task":"整理测试目录"}`), {Role: provider.RoleAssistant, Content: provider.TextContent("交给子代理了。")}},
		sub:  []provider.Message{assistantCall("s1", "", "sh", `{"description":"Verify the scenario behavior","command":"cd sub"}`), assistantCall("s2", "", "write_file", `{"path":"out/result.txt","content":"ok"}`), {Role: provider.RoleAssistant, Content: provider.TextContent("文件写好了。")}}}
	k, b, box := dispatchKernel(t, model)
	k.Permissive = true
	outside := t.TempDir()
	if err := os.Mkdir(filepath.Join(k.WorkDir, "sub"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(k.WorkDir, "sub", "out")); err != nil {
		t.Fatal(err)
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "整理目录", time.Now())); err != nil {
		t.Fatal(err)
	}
	close(hold)
	ev := waitEvent(t, box, exec.EvDone)
	if raw, err := os.ReadFile(filepath.Join(outside, "result.txt")); err != nil || string(raw) != "ok" {
		t.Fatalf("scoped write: %q, %v", raw, err)
	}
	meta, err := exec.ReadMeta(b.AgentDir(ev.Sender))
	if err != nil {
		t.Fatal(err)
	}
	if len(meta.Notable) != 1 || meta.Notable[0].Kind != "write_outside" || !strings.Contains(ev.Content, notable.Line(meta.Notable)) {
		t.Fatalf("meta/report: %+v / %s", meta.Notable, ev.Content)
	}
	model.mu.Lock()
	model.fore = append(model.fore, provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("文件写好了。\n\n" + notable.Line(meta.Notable))})
	model.mu.Unlock()
	if err := k.Handle(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	model.mu.Lock()
	defer model.mu.Unlock()
	req := model.foreReq[len(model.foreReq)-1]
	found, hint := false, false
	for _, msg := range req.Messages {
		if strings.Contains(msg.Text(), notable.Line(meta.Notable)) {
			found = true
		}
		if strings.Contains(msg.Text(), VoiceRule) {
			hint = true
		}
	}
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	ownerReply := false
	for _, entry := range entries {
		if entry.Message.Role == provider.RoleAssistant && entry.Message.Text() == "文件写好了。\n\n"+notable.Line(meta.Notable) {
			ownerReply = true
		}
		if strings.HasPrefix(entry.Message.Text(), "<subagent_report>") && !entry.IsInternal() {
			t.Fatal("notable hint exposed as owner transcript")
		}
	}
	if !found || !hint || !ownerReply {
		t.Fatalf("owner reply did not receive kernel facts/hint: facts=%v hint=%v", found, hint)
	}
}

type readOnlyNotableTool struct{}

func (readOnlyNotableTool) Name() string                                         { return "mcp_docs_read" }
func (readOnlyNotableTool) Description() string                                  { return "read docs" }
func (readOnlyNotableTool) Parameters() any                                      { return map[string]any{"type": "object"} }
func (readOnlyNotableTool) Confirm(json.RawMessage) (bool, string, error)        { return false, "read", nil }
func (readOnlyNotableTool) ReadOnlyHint() bool                                   { return true }
func (readOnlyNotableTool) Run(context.Context, json.RawMessage) (string, error) { return "data", nil }

func TestReadonlyMCPIsNotNotableThroughReview(t *testing.T) {
	k := &Kernel{Bot: testBot(t), Permissive: true}
	p := &scripted{resps: []provider.Message{assistantCall("r", "", "mcp_docs_read", `{}`)}}
	var log notable.Log
	if _, err := agent.Run(notable.With(context.Background(), t.TempDir(), &log), p, []tools.Tool{reviewedTool{Tool: readOnlyNotableTool{}, k: k}}, nil, agent.Options{OneRound: true}); err != nil {
		t.Fatal(err)
	}
	if len(log.Items()) != 0 {
		t.Fatalf("read-only MCP reported as external send: %+v", log.Items())
	}
}

func TestRolledBackMemoryWriteIsNotNotable(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	k := &Kernel{Bot: b, Memory: memory.Open(b.Root), WorkDir: t.TempDir()}
	path := filepath.Join(b.Root, "bots", b.Name(), "memory", "memory.md")
	old := "- (2026-10-18) old\n"
	if err := guardWrite(t, k, path, old); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]string{"path": path, "content": strings.Repeat("x", memory.ProfileBytes+1)})
	p := &scripted{resps: []provider.Message{assistantCall("w", "", "write_file", string(raw))}}
	var log notable.Log
	out, err := agent.Run(notable.With(context.Background(), k.WorkDir, &log), p, []tools.Tool{memoryGuarded{&tools.WriteFile{}, k}}, nil, agent.Options{OneRound: true})
	if err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(path)
	if err != nil || string(restored) != old {
		t.Fatalf("memory write not rolled back: %q %v", restored, err)
	}
	if len(out) < 2 || !strings.HasPrefix(out[1].Text(), "error:") {
		t.Fatalf("guard did not reject the write: %+v", out)
	}
	if len(log.Items()) != 0 {
		t.Fatalf("rejected, restored call reported as completed: %+v", log.Items())
	}
}
