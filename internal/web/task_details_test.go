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
	raw, err := json.Marshal(map[string]any{"id": "failed-task", "status": "failed", "task": "检查任务", "summary": summary, "started": "2026-10-21T08:00:00Z", "finished_at": "2026-10-21T08:03:00Z", "current_step": "读取目录", "steps": 3})
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
			Summary     string `json:"summary"`
			Started     string `json:"started_at"`
			Finished    string `json:"finished_at"`
			CurrentStep string `json:"current_step"`
			Steps       int    `json:"steps"`
		} `json:"agents"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &panel); err != nil {
		t.Fatal(err)
	}
	if len(panel.Agents) != 1 || panel.Agents[0].Summary != clip(summary, 2000) || !utf8.ValidString(panel.Agents[0].Summary) {
		t.Fatal("task API must return a rune-clipped result or error summary")
	}
	ag := panel.Agents[0]
	if ag.Started != "2026-10-21T08:00:00Z" || ag.Finished != "2026-10-21T08:03:00Z" || ag.CurrentStep != "读取目录" || ag.Steps != 3 {
		t.Fatalf("task status fields: %+v", ag)
	}

}
