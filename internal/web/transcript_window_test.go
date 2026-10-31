package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/pipe/limits"
	"xbot/internal/provider"
)

type windowResponse struct {
	Messages []apiRow       `json:"messages"`
	Window   bot.WindowMeta `json:"window"`
}

func TestTranscriptWindowHTTP(t *testing.T) {
	root := t.TempDir()
	s := &Server{Root: root, Owner: "reader"}
	token, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := bot.Create(root, "history", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer bot.CloseStore(b)
	if err := b.SetOwners([]string{"reader"}); err != nil {
		t.Fatal(err)
	}
	b.SetSegmentMax(256 << 10)
	var legacy strings.Builder
	for i := 1; i <= 4460; i++ {
		text := fmt.Sprintf("record %04d %s", i, strings.Repeat("conversation ", 42))
		if i == 111 {
			text += " uniquehistoryneedle"
		}
		raw, _ := json.Marshal(bot.Entry{Time: time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC), Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent(text)}})
		legacy.Write(raw)
		legacy.WriteByte('\n')
	}
	if err := os.WriteFile(filepath.Join(bot.Dir(root, b.Name()), "transcript.jsonl"), []byte(legacy.String()), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Count(); err != nil {
		t.Fatal(err)
	}
	h := muxOf(s)
	get := func(path, tag string, auth bool) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("GET", "/api/bots/history/transcript"+path, nil)
		if auth {
			r.AddCookie(&http.Cookie{Name: cookieName, Value: token})
		}
		r.Header.Set("If-None-Match", tag)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	decode := func(w *httptest.ResponseRecorder) windowResponse {
		t.Helper()
		if w.Code != 200 {
			t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
		}
		var out windowResponse
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		for i, m := range out.Messages {
			if i > 0 && m.Seq != out.Messages[i-1].Seq+1 {
				t.Fatal("window has a gap")
			}
		}
		return out
	}
	first := get("", "", true)
	tail := decode(first)
	if first.Body.Len() > limits.MaxBody || first.Body.Len() > legacy.Len()/10 {
		t.Fatalf("tail too large: %d full=%d cap=%d", first.Body.Len(), legacy.Len(), limits.MaxBody)
	}
	if len(tail.Messages) != 60 || tail.Messages[0].Seq != 4401 || tail.Window.TailSeq != 4460 || !tail.Window.HasMoreBefore || tail.Window.HasMoreAfter {
		t.Fatalf("default window: %+v", tail.Window)
	}
	tag := first.Header().Get("ETag")
	if get("", tag, true).Code != 304 {
		t.Fatal("idle default poll lost validator")
	}
	olderRes := get("?before=4401", tag, true)
	older := decode(olderRes)
	if older.Window.TailSeq != 4400 || older.Window.HeadSeq != 4341 || olderRes.Header().Get("ETag") == tag {
		t.Fatalf("older window/validator: %+v", older.Window)
	}
	next := decode(get("?after=4400", "", true))
	if next.Window.HeadSeq != 4401 || next.Window.TailSeq != 4460 {
		t.Fatalf("forward: %+v", next.Window)
	}
	around := decode(get("?around=111", "", true))
	if around.Window.HeadSeq > 111 || around.Window.TailSeq < 111 || !around.Window.HasMoreAfter {
		t.Fatalf("around: %+v", around.Window)
	}
	for _, query := range []string{"?before=1&after=2", "?limit=0", "?limit=201", "?before=-1", "?before=", "?after=abc", "?limit=1&limit=2"} {
		if w := get(query, "", true); w.Code != 400 {
			t.Fatalf("%s: %d", query, w.Code)
		}
	}
	if get("?around=99999", "", true).Code != 404 {
		t.Fatal("missing around target not 404")
	}
	if get("", tag, false).Code != 401 || get("/search?q=uniquehistoryneedle", "", false).Code != 401 {
		t.Fatal("history endpoint bypassed auth")
	}
	var hits struct {
		Matches []struct {
			Seq     int    `json:"seq"`
			Preview string `json:"preview"`
		} `json:"matches"`
	}
	search := get("/search?q=uniquehistoryneedle", "", true)
	if search.Code != 200 {
		t.Fatalf("search: %d %s", search.Code, search.Body.String())
	}
	if err := json.Unmarshal(search.Body.Bytes(), &hits); err != nil {
		t.Fatal(err)
	}
	seqs, err := b.Search("uniquehistoryneedle", 20)
	if err != nil {
		t.Fatal(err)
	}
	var got []int
	for _, v := range hits.Matches {
		got = append(got, v.Seq)
	}
	if !reflect.DeepEqual(got, seqs) || len(got) != 1 || got[0] != 111 || hits.Matches[0].Preview == "" {
		t.Fatalf("search not Bot.Search: %+v vs %v", hits, seqs)
	}
	if w := get("/search?q=absentneedle", "", true); w.Code != 200 || !strings.Contains(w.Body.String(), `"matches":[]`) {
		t.Fatalf("empty search: %s", w.Body.String())
	}
	for _, query := range []string{"/search?q=x&limit=0", "/search?q=x&limit=101", "/search?q=" + strings.Repeat("x", 1025)} {
		if get(query, "", true).Code != 400 {
			t.Fatal("bad search accepted")
		}
	}
	latencies := make([]time.Duration, 40)
	for i := range latencies {
		start := time.Now()
		decode(get("", "", true))
		latencies[i] = time.Since(start)
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	t.Logf("4460 records: full storage=%d bytes, default HTTP body=%d bytes (cap=%d), warm handler p95=%s", legacy.Len(), first.Body.Len(), limits.MaxBody, latencies[37])
	// Already indexed, sealed old history can be unreadable without affecting
	// default tail. This would fail if the GET still used Entries then slice.
	files, err := b.SegmentFiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 3 {
		t.Fatal("fixture did not cross segments")
	}
	if err := os.Chmod(files[0], 0600); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(files[0], []byte(strings.Repeat("x", len(raw))), 0600); err != nil {
		t.Fatal(err)
	}
	decode(get("", "", true))
	if get("?around=1", "", true).Code != 500 {
		t.Fatal("selected corrupt history silently swallowed")
	}
}

func TestTranscriptWindowByteCapAndDelivery(t *testing.T) {
	root := t.TempDir()
	s := &Server{Root: root, Owner: "u"}
	token, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := bot.Create(root, "ordinary", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer bot.CloseStore(b)
	if err := b.SetOwners([]string{"u"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 70; i++ {
		if err := b.Append(bot.Entry{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent(strings.Repeat("<", 5000))}}); err != nil {
			t.Fatal(err)
		}
	}
	h := muxOf(s)
	get := func(query string) windowResponse {
		t.Helper()
		r := httptest.NewRequest("GET", "/api/bots/ordinary/transcript"+query, nil)
		r.AddCookie(&http.Cookie{Name: cookieName, Value: token})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 || w.Body.Len() > limits.MaxBody {
			t.Fatalf("window body=%d cap=%d code=%d", w.Body.Len(), limits.MaxBody, w.Code)
		}
		var out windowResponse
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	tail := get("")
	if tail.Window.TailSeq != 70 || !tail.Window.HasMoreBefore {
		t.Fatal("byte cap lost tail or cursor")
	}
	older := get(fmt.Sprintf("?before=%d", tail.Window.NextBefore))
	if older.Window.TailSeq != tail.Window.HeadSeq-1 {
		t.Fatal("byte trimming broke pagination")
	}
	call := provider.ToolCall{ID: "delivery", Type: "function"}
	call.Function.Name = "send_message_to_user"
	call.Function.Arguments = `{"text":"` + strings.Repeat("long delivery ", 10000) + `"}`
	if err := b.Append(bot.Entry{Message: provider.Message{Role: provider.RoleAssistant, Content: []provider.ContentPart{{Type: provider.PartToolCall, ToolCall: &call}}}}); err != nil {
		t.Fatal(err)
	}
	if err := b.Append(bot.Entry{Message: provider.Message{Role: provider.RoleTool, Name: "send_message_to_user", Content: provider.ToolResult("delivery", "sent to the owner's chat")}}); err != nil {
		t.Fatal(err)
	}
	one := get("?around=71&limit=1")
	if len(one.Messages) != 1 || one.Messages[0].Seq != 71 || !one.Messages[0].Truncated || len(one.Messages[0].ToolCalls) != 1 || !reflect.DeepEqual(one.Messages[0].Delivered, []string{"delivery"}) {
		t.Fatalf("isolated oversized delivery lost public preview: %+v", one.Messages)
	}
}
