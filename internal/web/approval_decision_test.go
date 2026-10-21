package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/provider"
)

func TestApprovalDecisionPanel(t *testing.T) {
	root := t.TempDir()
	s := &Server{Root: root, Owner: "u1"}
	token, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := bot.Create(root, "ada", "persona", []string{link.NameWeb})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ id, state, status string }{
		{"bg_denied", bot.ApprovalDeclined, "done"},
		{"bg_allowed", bot.ApprovalApproved, "done"},
		{"bg_expired", "no_decision:expired", "done"},
		{"bg_failed", bot.ApprovalDeclined, "failed"},
		{"bg_running", bot.ApprovalDeclined, "running"},
	} {
		dir := b.AgentDir(tc.id)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(map[string]string{"id": tc.id, "mode": "fork-turn", "status": tc.status, "task": "发送消息"})
		if err := os.WriteFile(filepath.Join(dir, "meta.json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
		// The pending card predates the fork marker; replay must assign its branch.
		entries, _ := b.Entries()
		from := len(entries) + 1
		for _, e := range []bot.Entry{
			{Message: provider.Message{Role: provider.RoleUser, Content: bot.ApprovalCardText(tc.id, "shell", "对外发送", "在本机上执行", "curl https://example.invalid", bot.ApprovalPending)}},
			{Mark: &bot.ForkMark{Kind: "fork", Branch: tc.id, From: from}},
			{Branch: tc.id, Message: provider.Message{Role: provider.RoleUser, Content: bot.ApprovalCardText(tc.id, "shell", "对外发送", "在本机上执行", "curl https://example.invalid", tc.state)}},
		} {
			if err := b.Append(e); err != nil {
				t.Fatal(err)
			}
		}
	}
	req := httptest.NewRequest("GET", "/api/bots/ada", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
	res := httptest.NewRecorder()
	muxOf(s).ServeHTTP(res, req)
	if res.Code != 200 {
		t.Fatalf("status %d", res.Code)
	}
	var panel struct{ Agents []struct{ ID, Status string } }
	if err := json.Unmarshal(res.Body.Bytes(), &panel); err != nil {
		t.Fatal(err)
	}
	wants := map[string]string{"bg_denied": "denied", "bg_allowed": "done", "bg_expired": "done", "bg_failed": "failed", "bg_running": "running"}
	for _, ag := range panel.Agents {
		if ag.Status != wants[ag.ID] {
			t.Errorf("%s: status %s, want %s", ag.ID, ag.Status, wants[ag.ID])
		}
		delete(wants, ag.ID)
	}
	if len(wants) > 0 {
		t.Errorf("missing tasks: %v", wants)
	}
	for file, fragments := range map[string][]string{
		"static/app.js":  {"发到 ", "blockquote", "查看命令", "正在发送…", "正在处理…", "已过期，没有执行", "已发送", "没发", "已允许", "已拒绝", "ICONS.spinner"},
		"static/app.css": {".approval-line.no .st { color: var(--muted); }", ".approval-chip[hidden]", ".approval-spinner { animation: none; }"},
	} {
		raw, err := staticFS.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, fragment := range fragments {
			if !strings.Contains(string(raw), fragment) {
				t.Errorf("%s missing %q", file, fragment)
			}
		}
	}
}

func TestApprovalPresentationLineContinuations(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	raw, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	start := strings.Index(source, "  function approvalPresentation(")
	end := strings.Index(source[start:], "  function approvalLine(")
	script := source[start:start+end] + `
 for (const newline of ["\n", "\r\n"]) {
   const command = "curl https://example.invalid -d " + "\\" + newline + "'" + JSON.stringify({text: "hello"}) + "'";
   const view = approvalPresentation({kind: "outbound", command});
   if (view.body !== "hello" || view.command !== command) throw new Error(JSON.stringify(view));
 }
 `
	cmd := exec.Command(node, "-e", script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
}

func TestApprovalDecisionPanelSkipsUnneededTranscript(t *testing.T) {
	for _, tc := range []struct{ name, status, mode string }{
		{"no agents", "", ""},
		{"running fork", "running", "fork-turn"},
		{"failed fork", "failed", "fork-turn"},
		{"completed agent", "done", "agent"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			s := &Server{Root: root, Owner: "u1"}
			token, _, err := s.EnsureToken()
			if err != nil {
				t.Fatal(err)
			}
			b, err := bot.Create(root, "ada", "persona", []string{link.NameWeb})
			if err != nil {
				t.Fatal(err)
			}
			if err := b.SetOwners([]string{"u1"}); err != nil {
				t.Fatal(err)
			}
			if tc.status != "" {
				dir := b.AgentDir("task")
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				raw, _ := json.Marshal(map[string]string{"id": "task", "status": tc.status, "mode": tc.mode})
				if err := os.WriteFile(filepath.Join(dir, "meta.json"), raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
			// Reading the transcript migrates this legacy file, even when empty.
			legacy := filepath.Join(bot.Dir(root, "ada"), "transcript.jsonl")
			if err := os.WriteFile(legacy, nil, 0600); err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest("GET", "/api/bots/ada", nil)
			req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
			res := httptest.NewRecorder()
			muxOf(s).ServeHTTP(res, req)
			if res.Code != http.StatusOK {
				t.Fatalf("status %d: %s", res.Code, res.Body.String())
			}
			if _, err := os.Stat(legacy); err != nil {
				t.Fatalf("panel read an unneeded transcript: %v", err)
			}
		})
	}
}
