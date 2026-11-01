package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/experience"
)

func experienceSearchHTTP(t *testing.T) (string, func(string, url.Values, bool) *httptest.ResponseRecorder) {
	t.Helper()
	root := t.TempDir()
	s := &Server{Root: root, Owner: "u1"}
	token, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	mustWebBot(t, root, "ada", "u1")
	mustWebBot(t, root, "other", "u2")
	mux := muxOf(s)
	return root, func(name string, params url.Values, auth bool) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/bots/"+name+"/experiences?"+params.Encode(), nil)
		if auth {
			req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
		}
		out := httptest.NewRecorder()
		mux.ServeHTTP(out, req)
		return out
	}
}

func decodeExperienceSearchPage(t *testing.T, res *httptest.ResponseRecorder) experiencePage {
	t.Helper()
	if res.Code != http.StatusOK {
		t.Fatalf("search: %d %s", res.Code, res.Body)
	}
	var page experiencePage
	if err := json.Unmarshal(res.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Items == nil {
		t.Fatal("items must be an array, including when empty")
	}
	return page
}

func TestExperienceSearchHTTPPagination(t *testing.T) {
	root, call := experienceSearchHTTP(t)
	store := experience.At(bot.Dir(root, "ada"))
	base := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	var matches []experience.Entry
	for i := 0; i < 45; i++ {
		// Insert out of order, with equal timestamps at the search page boundary.
		at := base.Add(time.Duration((i+7)%44) * time.Minute)
		if i == 44 {
			at = base.Add(4 * time.Minute)
		}
		e, err := store.Add(experience.Entry{CreatedAt: at, Summary: fmt.Sprintf("旧经历 %d：TTS", i)})
		if err != nil {
			t.Fatal(err)
		}
		matches = append(matches, e)
		if _, err := store.Add(experience.Entry{CreatedAt: at.Add(time.Nanosecond), Summary: "不匹配的间隔记录"}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 42; i++ {
		if _, err := store.Add(experience.Entry{CreatedAt: base.Add(24*time.Hour + time.Duration(i)*time.Minute), Summary: "首屏新经历"}); err != nil {
			t.Fatal(err)
		}
	}
	initial := decodeExperienceSearchPage(t, call("ada", nil, true))
	if len(initial.Items) != 40 || initial.NextBefore == 0 {
		t.Fatalf("initial page: %+v", initial)
	}
	for _, e := range initial.Items {
		if e.Summary != "首屏新经历" {
			t.Fatalf("fixture must leave all matches outside initial page: %+v", e)
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].CreatedAt.Equal(matches[j].CreatedAt) {
			return matches[i].ID > matches[j].ID
		}
		return matches[i].CreatedAt.After(matches[j].CreatedAt)
	})
	params := url.Values{"q": {" \t tTs \n"}}
	first := decodeExperienceSearchPage(t, call("ada", params, true))
	if len(first.Items) != 40 || first.NextBefore != matches[39].ID {
		t.Fatalf("first search page: items=%d next_before=%d want %d", len(first.Items), first.NextBefore, matches[39].ID)
	}
	for i, e := range first.Items {
		if e.ID != matches[i].ID {
			t.Fatalf("search item %d: id=%d want old matching id=%d", i, e.ID, matches[i].ID)
		}
	}
	params.Set("before", fmt.Sprint(first.NextBefore))
	second := decodeExperienceSearchPage(t, call("ada", params, true))
	if len(second.Items) != 5 || second.NextBefore != 0 {
		t.Fatalf("last search page: %+v", second)
	}
	for i, e := range second.Items {
		if e.ID != matches[i+40].ID {
			t.Fatalf("continued search item %d: id=%d want %d", i, e.ID, matches[i+40].ID)
		}
	}
	// Empty and whitespace-only q keep ordinary pagination, including before.
	for _, q := range []string{"", " \t\n　"} {
		page := decodeExperienceSearchPage(t, call("ada", url.Values{"q": {q}}, true))
		if len(page.Items) != len(initial.Items) || page.NextBefore != initial.NextBefore {
			t.Fatalf("empty query changed first page: %+v", page)
		}
		for i, e := range page.Items {
			if e.ID != initial.Items[i].ID {
				t.Fatal("empty query changed first page order")
			}
		}
		older := decodeExperienceSearchPage(t, call("ada", url.Values{"q": {q}, "before": {fmt.Sprint(page.NextBefore)}}, true))
		if len(older.Items) != 40 || older.Items[0].Summary != "首屏新经历" || older.Items[0].ID == page.Items[39].ID {
			t.Fatalf("empty query did not preserve ordinary pagination: %+v", older)
		}
	}
}

func TestExperienceSearchHTTPLiteralMatch(t *testing.T) {
	root, call := experienceSearchHTTP(t)
	store := experience.At(bot.Dir(root, "ada"))
	entries := []experience.Entry{
		{Summary: "摘要：中文语音和 SuMmArY"},
		{Summary: "关注记录", Focus: "关注：中文关注和 FoCuS"},
		{Summary: "提纲记录", Segment: "提纲：中文提纲和 SeGmEnT"},
		{Summary: "100% 完成", Focus: "task_name", Segment: `C:\voice\tts and ' OR 1=1 --`},
		{Summary: "100x 完成", Focus: "taskXname", Segment: "普通提纲"},
	}
	for i, e := range entries {
		e.CreatedAt = time.Date(2026, 10, 1, 8, i, 0, 0, time.UTC)
		stored, err := store.Add(e)
		if err != nil {
			t.Fatal(err)
		}
		entries[i] = stored
	}
	for _, tc := range []struct {
		query string
		index int
	}{
		{"中文语音", 0}, {"summary", 0}, {"中文关注", 1}, {"FOCUS", 1},
		{"中文提纲", 2}, {"segment", 2}, {"%", 3}, {"_", 3}, {`\`, 3}, {"' OR 1=1 --", 3},
		{"不存在的经历", -1}, {"100_", -1},
	} {
		t.Run(tc.query, func(t *testing.T) {
			page := decodeExperienceSearchPage(t, call("ada", url.Values{"q": {tc.query}}, true))
			want := 1
			if tc.index < 0 {
				want = 0
			}
			if len(page.Items) != want || page.NextBefore != 0 {
				t.Fatalf("query %q: %+v", tc.query, page)
			}
			if want == 1 && page.Items[0].ID != entries[tc.index].ID {
				t.Fatalf("query %q matched wrong entry: %+v", tc.query, page.Items[0])
			}
		})
	}
}

func TestExperienceSearchHTTPValidationAndEmptyStore(t *testing.T) {
	root, call := experienceSearchHTTP(t)
	for _, q := range []string{"", "TTS", strings.Repeat("x", 512), " \t" + strings.Repeat("x", 512) + "\n ", strings.Repeat("　", 200)} {
		page := decodeExperienceSearchPage(t, call("ada", url.Values{"q": {q}}, true))
		if len(page.Items) != 0 || page.NextBefore != 0 {
			t.Fatalf("empty database: %+v", page)
		}
	}
	for _, tc := range []struct {
		name   string
		params url.Values
		auth   bool
		want   int
	}{
		{"ada", url.Values{"q": {strings.Repeat("x", 513)}}, true, 400},
		{"ada", url.Values{"q": {strings.Repeat("中", 171)}}, true, 400},
		{"ada", url.Values{"q": {"TTS"}, "before": {"bad"}}, true, 400},
		{"ada", url.Values{"q": {"TTS"}, "before": {"0"}}, true, 400},
		{"ada", url.Values{"q": {"TTS"}, "before": {"-1"}}, true, 400},
		{"ada", url.Values{"q": {"TTS"}}, false, 401},
		{"ada", url.Values{"q": {strings.Repeat("x", 513)}}, false, 401},
		{"other", url.Values{"q": {"TTS"}}, true, 404},
		{"other", url.Values{"q": {strings.Repeat("x", 513)}}, true, 404},
		{"missing", url.Values{"q": {"TTS"}}, true, 404},
	} {
		if res := call(tc.name, tc.params, tc.auth); res.Code != tc.want {
			t.Errorf("bot=%s q-bytes=%d before=%q auth=%t: %d want %d (%s)", tc.name, len(tc.params.Get("q")), tc.params.Get("before"), tc.auth, res.Code, tc.want, res.Body)
		}
	}
	for _, name := range []string{"ada", "other"} {
		if _, err := os.Stat(filepath.Join(bot.Dir(root, name), experience.FileName)); !os.IsNotExist(err) {
			t.Fatalf("search created empty database for %s: %v", name, err)
		}
	}
}
