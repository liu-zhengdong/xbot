//go:build smoke

package smoke

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/experience"
)

// The shipped serve assembles whole documents and dated experiences from its
// real data root. No model call is needed to read the information panel.
func TestMemoryPanelWhole(t *testing.T) {
	srv := Start(t, StartStub(t))
	srv.CreateBot("ada", "记忆面板冒烟")
	initial := srv.Memory("ada")
	personMD := "# 用户记忆\n\n## 索引\n用户的约定\n\n## 常驻\n- (2026-10-30) 完整的 **中文** 正文\n"
	botMD := "# Bot 记忆\n\n## 索引\n开发规则\n\n## 常驻\n- (2026-10-30) 整篇保留\n"
	for path, body := range map[string]string{initial.Person.Path: personMD, initial.Bot.Path: botMD} {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	logPath := filepath.Join(filepath.Dir(initial.Bot.Path), "log", time.Now().Format("2006-01")+".md")
	if err := os.MkdirAll(filepath.Dir(logPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, []byte("流水原文只通过文件入口读取"), 0600); err != nil {
		t.Fatal(err)
	}
	store := experience.At(bot.Dir(srv.Root, "ada"))
	for i := 0; i < 43; i++ {
		_, err := store.Add(experience.Entry{CreatedAt: time.Date(2026, 10, 30, 0, i, 0, 0, time.UTC), Summary: fmt.Sprintf("经历 %d", i), Segment: "可展开提纲"})
		if err != nil {
			t.Fatal(err)
		}
	}
	status, raw := srv.do(http.MethodGet, "/api/bots/ada", nil)
	if status != 200 {
		t.Fatalf("panel: %d %s", status, raw)
	}
	var out struct {
		Memory struct {
			Person, Bot MemoryDocument
			Experiences struct {
				Items []struct {
					ID               int64
					CreatedAt        time.Time `json:"created_at"`
					Summary, Segment string
				}
				Before int64 `json:"next_before"`
			}
			MonthLog struct {
				Path  string
				Empty bool
			} `json:"month_log"`
		}
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	m := out.Memory
	if m.Person.Markdown != personMD || m.Bot.Markdown != botMD || m.Person.Empty || m.Bot.Empty {
		t.Fatalf("whole documents: %s", raw)
	}
	if len(m.Experiences.Items) != 40 || m.Experiences.Before != 4 {
		t.Fatalf("experience page: %s", raw)
	}
	first := m.Experiences.Items[0]
	if first.ID != 43 || first.CreatedAt.IsZero() || first.Summary != "经历 42" || first.Segment != "可展开提纲" {
		t.Fatalf("experience fields: %+v", first)
	}
	if m.MonthLog.Path != logPath || m.MonthLog.Empty || strings.Contains(string(raw), "流水原文") {
		t.Fatalf("log footer contract: %s", raw)
	}
	status, raw = srv.do(http.MethodGet, "/api/bots/ada/experiences?before=4", nil)
	var older struct {
		Items  []struct{ ID int64 }
		Before int64 `json:"next_before"`
	}
	if err := json.Unmarshal(raw, &older); err != nil {
		t.Fatal(err)
	}
	if status != 200 || len(older.Items) != 3 || older.Items[0].ID != 3 || older.Items[2].ID != 1 || older.Before != 0 {
		t.Fatalf("older page: %d %s", status, raw)
	}
	status, raw = srv.do(http.MethodGet, "/api/bots/ada/memory-files/person", nil)
	if status != 200 || string(raw) != personMD {
		t.Fatalf("original file: %d %s", status, raw)
	}
}
