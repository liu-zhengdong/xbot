//go:build smoke

package smoke

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// Real binary, login, HTTP, transcript and model transport; only the model and
// clock offset are scripted. Compaction/fitting coverage lives at the kernel seam.
func TestEnvironmentPromptBinary(t *testing.T) {
	var mu sync.Mutex
	var views [][]string
	calls := 0
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			http.Error(w, "bad request", 400)
			return
		}
		var texts []string
		for _, m := range req.Messages {
			var text string
			if json.Unmarshal(m.Content, &text) != nil {
				text = string(m.Content)
			}
			texts = append(texts, m.Role+":"+text)
		}
		mu.Lock()
		views = append(views, texts)
		calls++
		n := calls
		mu.Unlock()
		msg := map[string]any{"role": "assistant", "content": "SILENT"}
		reason := "stop"
		if n%2 == 1 {
			msg["content"] = ""
			msg["tool_calls"] = []any{map[string]any{"id": fmt.Sprintf("reply-%d", n), "type": "function", "function": map[string]any{"name": "send_message_to_user", "arguments": `{"text":"收到"}`}}}
			reason = "tool_calls"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"index": 0, "message": msg, "finish_reason": reason}}})
	}))
	defer backend.Close()
	srv := Start(t, &Stub{srv: backend})
	srv.CreateBot("ada", "Be brief.")
	srv.PatchBot("ada", map[string]any{"timezone": "UTC"})
	// Start near the beginning of a minute, leaving ample room for two local calls.
	srv.Advance(time.Until(time.Now().UTC().Truncate(time.Minute).Add(5 * time.Second)))
	var prefix string
	for i, want := range []int{1, 1, 2, 3} {
		if i == 2 {
			srv.Advance(time.Minute)
		}
		if i == 3 {
			srv.Restart()
		}
		started := time.Now()
		result := srv.Send("ada", fmt.Sprintf("clock %d", i))
		if result.Error != "" {
			t.Fatal(result.Error)
		}
		mu.Lock()
		view := append([]string(nil), views[len(views)-1]...)
		mu.Unlock()
		text := strings.Join(view, "\n")
		if got := strings.Count(text, "当前时间："); got != want {
			t.Fatalf("step %d: announcements=%d want=%d", i, got, want)
		}
		if strings.Contains(text, `"time":`) {
			t.Fatal("time JSON set reached model")
		}
		if i == 0 {
			prefix = view[0]
		} else if view[0] != prefix {
			t.Fatal("stable prefix changed")
		}
		t.Logf("step=%d announcements=%d HTTP round-trip=%s", i, want, time.Since(started))
	}
}
