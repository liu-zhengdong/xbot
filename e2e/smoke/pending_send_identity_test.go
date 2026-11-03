//go:build smoke

package smoke

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// The real binary must preserve independent submission identities through the
// owner ingress, transcript, chat projection, and a process restart.
func TestPendingSendIdentityThroughServe(t *testing.T) {
	cliSkillIsolation(t)
	stub := StartStub(t)
	srv := Start(t, stub)
	srv.CreateBot("identity", "Keep independent owner messages in the same conversation.")
	ids := []string{"web_" + strings.Repeat("a", 32), "web_" + strings.Repeat("b", 32)}
	const text = "SMOKE_HELLO_1"
	for _, id := range ids {
		status, raw := srv.do(http.MethodPost, "/api/bots/identity/messages", map[string]string{"text": text, "client_id": id})
		if status != http.StatusOK || !strings.Contains(string(raw), "event: done") || strings.Contains(string(raw), "event: error") {
			t.Fatalf("independent submission did not finish: status=%d body=%.1000s", status, raw)
		}
	}
	check := func() {
		t.Helper()
		status, raw := srv.do(http.MethodGet, "/api/bots/identity/chat?limit=200", nil)
		if status != http.StatusOK {
			t.Fatalf("chat: status=%d body=%.1000s", status, raw)
		}
		var page struct {
			Messages []struct {
				ID   int    `json:"id"`
				Kind string `json:"kind"`
				Text string `json:"text"`
				Meta struct {
					EventID string `json:"event_id"`
				} `json:"meta"`
			} `json:"messages"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			t.Fatal(err)
		}
		seen := make(map[string]int)
		for _, row := range page.Messages {
			if row.Kind == "owner" && row.Text == text {
				seen[row.Meta.EventID]++
			}
		}
		if len(seen) != len(ids) {
			t.Fatalf("same-text submissions lost separate identities: %v", seen)
		}
		for _, id := range ids {
			if seen[id] != 1 {
				t.Fatalf("submission %s has %d owner records, want one", id, seen[id])
			}
		}
	}
	check()
	srv.Restart()
	check()
}
