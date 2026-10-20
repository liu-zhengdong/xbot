package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/link"
)

// TestPanelOrdersAgentsByState pins the panel's agent order: live work first
// (running, awaiting, queued), finished last. The ids are chosen so directory
// order is the reverse of the wanted order, so the old unsorted list fails.
func TestPanelOrdersAgentsByState(t *testing.T) {
	root := t.TempDir()
	s := &Server{Root: root, Owner: "u1"}
	tok, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := bot.Create(root, "ada", "p", []string{link.NameTerminal, link.NameWeb})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	for _, ag := range []struct{ id, status string }{
		{"sa_a", "queued"},
		{"sa_b", "running"},
		{"sa_c", "awaiting"},
		{"sa_d", "queued"},
		{"sa_e", "done"},
	} {
		dir := b.AgentDir(ag.id)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		raw := fmt.Sprintf(`{"id":%q,"status":%q,"task":"t","mode":"brief","steps":1}`, ag.id, ag.status)
		if err := os.WriteFile(filepath.Join(dir, "meta.json"), []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
	}

	srv := httptest.NewServer(muxOf(s))
	defer srv.Close()
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/bots/ada", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: cookieName, Value: tok})
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("panel: %d", res.StatusCode)
	}
	var out struct {
		Agents []struct {
			ID string `json:"id"`
		} `json:"agents"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, ag := range out.Agents {
		got = append(got, ag.ID)
	}
	want := []string{"sa_b", "sa_c", "sa_a", "sa_d", "sa_e"}
	if len(got) != len(want) {
		t.Fatalf("agents = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("agents = %v, want %v (running, awaiting, then queued in directory order, then finished)", got, want)
		}
	}
}
