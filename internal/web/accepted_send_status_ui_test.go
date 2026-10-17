package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/link"
)

func TestAcceptedSendStatusRecovery(t *testing.T) {
	root := t.TempDir()
	b, err := bot.Create(root, "ada", "persona", []string{link.NameWeb})
	if err != nil {
		t.Fatal(err)
	}
	dir := b.AgentDir("failed-task")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	meta := filepath.Join(dir, "meta.json")
	if err := os.WriteFile(meta, []byte(`{"id":"failed-task","status":"failed","summary":"provider: request timeout after 3m0s"}`), 0600); err != nil {
		t.Fatal(err)
	}
	ended := time.Date(2026, 10, 18, 1, 2, 3, 0, time.UTC)
	if err := os.Chtimes(meta, ended, ended); err != nil {
		t.Fatal(err)
	}
	s := &Server{Root: root, Owner: "u1"}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	token, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/api/bots/ada", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
	res := httptest.NewRecorder()
	muxOf(s).ServeHTTP(res, req)
	var panel struct {
		Agents []struct {
			FinishedAt time.Time `json:"finished_at"`
			Summary    string    `json:"summary"`
		} `json:"agents"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &panel); err != nil {
		t.Fatal(err)
	}
	if len(panel.Agents) != 1 || !panel.Agents[0].FinishedAt.Equal(ended) || panel.Agents[0].Summary != "provider: request timeout after 3m0s" {
		t.Fatalf("failed task lost its last error or end time: %+v", panel)
	}
	raw, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"function zhError(msg)", "function taskStatus(status)", "function errorDetails(host, raw, shown)",
		"pending.keepFailed = true", "(row.queued || row.keepFailed) && !queuedRecorded(row)",
		"pending.baseline = ownerSends(text)", `event === "queued"`, `payload.status === "queued"`,
		"await readSSE(res, selection, prose, markQueued)", "没送到，原文还在输入框里",
		`field.addEventListener("input", () => { go.disabled = !field.value.trim(); })`,
		`clip(plainText(ag.task || ag.id), 80)`, "没有留下失败原因",
		"clip(plainText(raw), 72)", "errorDetails(drawer, ag.summary, shown)",
	} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("missing send/status UI regression contract: %s", want)
		}
	}
}
