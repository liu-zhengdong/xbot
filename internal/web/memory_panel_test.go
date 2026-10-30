package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/experience"
)

// Exercise the information-panel assembly, not Recall's fact projection.
func TestMemoryPanelWholeDocumentsAndDatedExperiences(t *testing.T) {
	root := t.TempDir()
	s := &Server{Root: root, Owner: "u1"}
	token, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	mustWebBot(t, root, "ada", "u1")
	mustWebBot(t, root, "other", "u2")
	personPath := filepath.Join(root, "people", "u1", "memory", "memory.md")
	botPath := filepath.Join(bot.Dir(root, "ada"), "memory", "memory.md")
	personMD := "# 关于用户\n\n## 索引\n完整的 **Markdown** 与 [链接](https://example.com)。\n" + strings.Repeat("- (2026-10-30) 不能截在第四十条\n", 45)
	botMD := "# Bot 记忆\n\n## 索引\n项目约定\n\n## 常驻\n- (2026-10-30) 保持整篇\n"
	for path, body := range map[string]string{personPath: personMD, botPath: botMD} {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	logPath := filepath.Join(filepath.Dir(botPath), "log", time.Now().Format("2006-01")+".md")
	if err := os.MkdirAll(filepath.Dir(logPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, []byte("流水正文不应装进主列"), 0600); err != nil {
		t.Fatal(err)
	}
	store := experience.At(bot.Dir(root, "ada"))
	// Insert out of chronological order, including equal times at a page boundary.
	var entries []experience.Entry
	for i := 0; i < 43; i++ {
		at := time.Date(2026, 10, 30, 8, 0, 0, 0, time.UTC).Add(time.Duration((i+7)%42) * time.Minute)
		if i == 42 {
			at = time.Date(2026, 10, 30, 8, 2, 0, 0, time.UTC)
		}
		e, err := store.Add(experience.Entry{CreatedAt: at, Summary: fmt.Sprintf("经历 %d", i), Focus: "关注点", Segment: "提纲\n一段真实经历"})
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, e)
	}
	srv := httptest.NewServer(muxOf(s))
	defer srv.Close()
	get := func(path string, auth bool) (int, []byte) {
		t.Helper()
		req, _ := http.NewRequest("GET", srv.URL+path, nil)
		if auth {
			req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
		}
		res, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var raw json.RawMessage
		if err := json.NewDecoder(res.Body).Decode(&raw); err != nil {
			t.Fatal(err)
		}
		return res.StatusCode, raw
	}
	code, raw := get("/api/bots/ada", true)
	if code != 200 {
		t.Fatalf("panel: %d %s", code, raw)
	}
	var panel struct {
		Memory struct {
			Person struct {
				Path, Markdown string
				Empty          bool
			}
			Bot struct {
				Path, Markdown string
				Empty          bool
			}
			Experiences struct {
				Items []struct {
					ID                      int64
					CreatedAt               time.Time `json:"created_at"`
					Summary, Focus, Segment string
				}
				Next int64 `json:"next_before"`
			}
			MonthLog struct {
				Path  string
				Empty bool
			} `json:"month_log"`
		}
	}
	if err := json.Unmarshal(raw, &panel); err != nil {
		t.Fatalf("panel needs a memory DTO: %v", err)
	}
	m := panel.Memory
	if m.Person.Path != personPath || m.Person.Markdown != personMD || m.Person.Empty {
		t.Fatalf("person document changed: %+v", m.Person)
	}
	if m.Bot.Path != botPath || m.Bot.Markdown != botMD || m.Bot.Empty {
		t.Fatalf("bot document changed: %+v", m.Bot)
	}
	if m.MonthLog.Path != logPath || m.MonthLog.Empty {
		t.Fatalf("month log: %+v", m.MonthLog)
	}
	if strings.Contains(string(raw), "流水正文") {
		t.Fatal("log body leaked into panel")
	}
	if len(m.Experiences.Items) != 40 || m.Experiences.Next == 0 {
		t.Fatalf("first page: %+v", m.Experiences)
	}
	seen := map[int64]bool{}
	for i, e := range m.Experiences.Items {
		if e.CreatedAt.IsZero() || e.Summary == "" || e.Focus != "关注点" || e.Segment != "提纲\n一段真实经历" {
			t.Fatalf("lost experience fields: %+v", e)
		}
		if i > 0 {
			prev := m.Experiences.Items[i-1]
			if prev.CreatedAt.Before(e.CreatedAt) || (prev.CreatedAt.Equal(e.CreatedAt) && prev.ID < e.ID) {
				t.Fatal("experiences not newest first")
			}
		}
		seen[e.ID] = true
	}
	code, raw = get(fmt.Sprintf("/api/bots/ada/experiences?before=%d", m.Experiences.Next), true)
	if code != 200 {
		t.Fatalf("older page: %d %s", code, raw)
	}
	var older struct {
		Items []struct{ ID int64 }
		Next  int64 `json:"next_before"`
	}
	if err := json.Unmarshal(raw, &older); err != nil {
		t.Fatal(err)
	}
	if len(older.Items) != 3 || older.Next != 0 {
		t.Fatalf("older page: %+v", older)
	}
	for _, e := range older.Items {
		if seen[e.ID] {
			t.Fatal("duplicate page entry")
		}
		seen[e.ID] = true
	}
	if len(seen) != len(entries) {
		t.Fatal("missing experience")
	}
	for _, tc := range []struct {
		path string
		auth bool
		want int
	}{
		{"/api/bots/ada/experiences", false, 401}, {"/api/bots/other/experiences", true, 404}, {"/api/bots/ada/experiences?before=bad", true, 400},
	} {
		if code, _ := get(tc.path, tc.auth); code != tc.want {
			t.Fatalf("%s: %d want %d", tc.path, code, tc.want)
		}
	}
	durations := make([]time.Duration, 30)
	payloadBytes := 0
	for i := range durations {
		start := time.Now()
		code, body := get("/api/bots/ada", true)
		durations[i] = time.Since(start)
		if code != 200 {
			t.Fatalf("panel sample: %d", code)
		}
		payloadBytes = len(body)
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	t.Logf("panel HTTP: n=%d p95=%s payload=%d bytes", len(durations), durations[28], payloadBytes)

	// Missing files are empty documents with their canonical paths, never fake facts.
	if err := os.Remove(personPath); err != nil {
		t.Fatal(err)
	}
	code, raw = get("/api/bots/ada", true)
	if code != 200 {
		t.Fatal(code)
	}
	if err := json.Unmarshal(raw, &panel); err != nil {
		t.Fatal(err)
	}
	if !panel.Memory.Person.Empty || panel.Memory.Person.Markdown != "" || panel.Memory.Person.Path != personPath {
		t.Fatalf("missing person: %+v", panel.Memory.Person)
	}
}

func TestMemoryPanelEmptyAndReadFailures(t *testing.T) {
	root := t.TempDir()
	s := &Server{Root: root, Owner: "u1"}
	token, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	mustWebBot(t, root, "ada", "u1")
	mux := muxOf(s)
	call := func() *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest("GET", "/api/bots/ada", nil)
		req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
		out := httptest.NewRecorder()
		mux.ServeHTTP(out, req)
		return out
	}
	res := call()
	if res.Code != 200 {
		t.Fatalf("empty panel: %d %s", res.Code, res.Body)
	}
	var panel struct {
		Memory struct {
			Person, Bot struct {
				Path, Markdown string
				Empty          bool
			}
			Experiences struct{ Items []json.RawMessage }
			MonthLog    struct{ Empty bool } `json:"month_log"`
		}
	}
	if err := json.Unmarshal(res.Body.Bytes(), &panel); err != nil {
		t.Fatal(err)
	}
	if !panel.Memory.Person.Empty || !panel.Memory.Bot.Empty || !panel.Memory.MonthLog.Empty || panel.Memory.Experiences.Items == nil || len(panel.Memory.Experiences.Items) != 0 {
		t.Fatalf("empty contract: %s", res.Body)
	}
	if _, err := os.Stat(filepath.Join(bot.Dir(root, "ada"), experience.FileName)); !os.IsNotExist(err) {
		t.Fatalf("reading empty panel created experience database: %v", err)
	}
	path := panel.Memory.Bot.Path
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if res := call(); res.Code != 500 {
		t.Fatalf("read failure shown as empty: %d", res.Code)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("a", memoryDocumentLimit+1)), 0600); err != nil {
		t.Fatal(err)
	}
	if res := call(); res.Code != 500 {
		t.Fatalf("oversize doc silently truncated: %d", res.Code)
	}
}

func TestMemoryPanelOpenFiles(t *testing.T) {
	root := t.TempDir()
	s := &Server{Root: root, Owner: "u1", App: true}
	token, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	b := mustWebBot(t, root, "ada", "u1")
	mustWebBot(t, root, "other", "u2")
	panel, err := s.memoryPanel(b)
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]string{"person": panel.Person.Path, "bot": panel.Bot.Path, "month-log": panel.MonthLog.Path}
	for _, path := range paths {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("# 原始文件\n\n完整正文\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var opened string
	s.OpenPath = func(path string) error { opened = path; return nil }
	mux := muxOf(s)
	call := func(method, path string, auth bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		if auth {
			req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
		}
		out := httptest.NewRecorder()
		mux.ServeHTTP(out, req)
		return out
	}
	for kind, path := range paths {
		url := "/api/bots/ada/memory-files/" + kind
		res := call("GET", url, true)
		if res.Code != 200 || res.Body.String() != "# 原始文件\n\n完整正文\n" || !strings.HasPrefix(res.Header().Get("Content-Type"), "text/plain") {
			t.Fatalf("browser file: %d %s", res.Code, res.Body)
		}
		res = call("POST", url+"/open", true)
		if res.Code != 200 || opened != path {
			t.Fatalf("native file: %d %q want %q", res.Code, opened, path)
		}
	}
	for _, tc := range []struct {
		method, path string
		auth         bool
		want         int
	}{
		{"GET", "/api/bots/ada/memory-files/person", false, 401},
		{"GET", "/api/bots/other/memory-files/person", true, 404},
		{"GET", "/api/bots/ada/memory-files/free-path", true, 404},
	} {
		if res := call(tc.method, tc.path, tc.auth); res.Code != tc.want {
			t.Fatalf("%s: %d", tc.path, res.Code)
		}
	}
	s.App = false
	if res := call("POST", "/api/bots/ada/memory-files/bot/open", true); res.Code != 404 {
		t.Fatal("browser opened native application")
	}
	if err := os.Remove(panel.Person.Path); err != nil {
		t.Fatal(err)
	}
	if res := call("GET", "/api/bots/ada/memory-files/person", true); res.Code != 404 {
		t.Fatal("missing file is not 404")
	}
}
