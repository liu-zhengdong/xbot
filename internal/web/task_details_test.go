package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"xbot/internal/bot"
	"xbot/internal/link"
)

func TestTaskDetails(t *testing.T) {
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
	dir := b.AgentDir("failed-task")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	summary := strings.Repeat("错😀", 1100)
	raw, err := json.Marshal(map[string]any{"id": "failed-task", "status": "failed", "task": "检查任务", "summary": summary})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/api/bots/ada", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
	res := httptest.NewRecorder()
	muxOf(s).ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d", res.Code)
	}
	var panel struct {
		Agents []struct {
			Summary string `json:"summary"`
		} `json:"agents"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &panel); err != nil {
		t.Fatal(err)
	}
	if len(panel.Agents) != 1 || panel.Agents[0].Summary != clip(summary, 2000) || !utf8.ValidString(panel.Agents[0].Summary) {
		t.Fatal("task API must return a rune-clipped result or error summary")
	}
	for file, wants := range map[string][]string{
		"static/app.js":  {`const row = el("button", "recent-task")`, `row.type = "button"`, `row.setAttribute("aria-expanded", "true")`, `openDetail.drawer.remove()`, `没有留下结果摘要。`, `if (ag.finished_at)`},
		"static/app.css": {"position: sticky; top: 0; z-index: 3; background: var(--panel)", ".routine { align-items: center; }", ".routine .meta { font-family: var(--font); font-variant-numeric: tabular-nums; }", ".recent-detail .failed { color: var(--err); }"},
	} {
		raw, err := staticFS.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range wants {
			if !strings.Contains(string(raw), want) {
				t.Errorf("%s missing %s", file, want)
			}
		}
	}
}
