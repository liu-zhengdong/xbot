package nodesync

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"xbot/internal/memory"
	"xbot/internal/node"
	"xbot/internal/person"
)

func TestSyncNewestAcrossNodes(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()
	if err := os.WriteFile(filepath.Join(a, "node-id"), []byte("home-a\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b, "node-id"), []byte("home-b\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(a, "bots", "ada"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a, "bots", "ada", "keep.txt"), []byte("stay"), 0600); err != nil {
		t.Fatal(err)
	}
	t1 := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Hour)
	remember(t, a, "city", "Shanghai", t1)
	remember(t, a, "food", "noodles", t1)
	remember(t, b, "city", "Beijing", t2)
	remember(t, b, "drink", "tea", t2)
	if err := person.Record(a, "ada", "home-a", t1, person.Person{ID: "u1", DisplayName: "Ada", Timezone: "Asia/Shanghai"}); err != nil {
		t.Fatal(err)
	}
	if err := person.Record(b, "bea", "home-b", t2, person.Person{ID: "u1", DisplayName: "Beatrice", Timezone: "Asia/Shanghai"}); err != nil {
		t.Fatal(err)
	}
	sb := serveSync(t, b, "token-b", nil)
	if err := node.Add(a, node.Node{Name: "b", URL: sb.URL, Token: "token-b"}); err != nil {
		t.Fatal(err)
	}
	if err := Once(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	wantName := map[string]string{a: "Ada", b: "Beatrice"}
	for _, root := range []string{a, b} {
		got := texts(t, root)
		if !factText(got["city"], "Beijing") || !factText(got["food"], "noodles") || !factText(got["drink"], "tea") {
			t.Fatalf("%s facts %+v", root, got)
		}
		p, err := person.Load(root, "u1")
		if err != nil || p.DisplayName != wantName[root] || p.Timezone != "Asia/Shanghai" {
			t.Fatalf("%s person %+v %v", root, p, err)
		}
	}
	if _, err := os.Stat(filepath.Join(b, "bots", "ada", "keep.txt")); !os.IsNotExist(err) {
		t.Fatalf("bot dir copied to peer: %v", err)
	}
	if raw, err := os.ReadFile(filepath.Join(a, "bots", "ada", "keep.txt")); err != nil || string(raw) != "stay" {
		t.Fatalf("home bot changed %q %v", raw, err)
	}
}

func TestSyncRejectsBadToken(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()
	t1 := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	remember(t, a, "city", "Shanghai", t1)
	remember(t, b, "city", "Beijing", t1.Add(time.Hour))
	var hits int32
	sb := serveSync(t, b, "token-b", &hits)
	if err := node.Add(a, node.Node{Name: "b", URL: sb.URL, Token: "wrong"}); err != nil {
		t.Fatal(err)
	}
	err := Once(context.Background(), a)
	if err == nil || !strings.Contains(err.Error(), "unauthorized") {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&hits) != 0 {
		t.Fatalf("peer accepted bad token: %d", hits)
	}
	if texts(t, a)["city"] != "Shanghai" || texts(t, b)["city"] != "Beijing" {
		t.Fatalf("data moved a=%v b=%v", texts(t, a), texts(t, b))
	}
}

func TestSyncOnChange(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()
	sb := serveSync(t, b, "token-b", nil)
	if err := node.Add(a, node.Node{Name: "b", URL: sb.URL, Token: "token-b"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	Start(ctx, a, time.Hour, 20*time.Millisecond)
	remember(t, a, "city", "Shanghai", time.Date(2026, 10, 15, 11, 0, 0, 0, time.UTC))
	waitFact(t, b, "city", "Shanghai")
}

func TestSyncPeriodic(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()
	remember(t, a, "city", "Shanghai", time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC))
	sb := serveSync(t, b, "token-b", nil)
	if err := node.Add(a, node.Node{Name: "b", URL: sb.URL, Token: "token-b"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	Start(ctx, a, 40*time.Millisecond, time.Hour)
	waitFact(t, b, "city", "Shanghai")
}

func TestRelayedMergeSyncsSoon(t *testing.T) {
	a := t.TempDir()
	c := t.TempDir()
	sc := serveSync(t, c, "token-c", nil)
	if err := node.Add(a, node.Node{Name: "c", URL: sc.URL, Token: "token-c"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	Start(ctx, a, time.Hour, 20*time.Millisecond)
	when := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	line := memory.SyncLine{
		Time: when, Bot: "bob", Device: "home-b", Op: "remember",
		Fact: memory.Fact{
			Key: "city", Scope: memory.ScopePerson, PersonID: "u1", Tier: memory.TierProfile,
			WrittenBy: "bob", Text: "Beijing", Time: when,
		},
	}
	if err := memory.MergeSync(a, []memory.SyncLine{line}); err != nil {
		t.Fatal(err)
	}
	waitFact(t, c, "city", "Beijing")
}

func TestAcceptRejectsOversize(t *testing.T) {
	root := t.TempDir()
	_, err := Accept(root, strings.Repeat("a", 1<<20))
	if err == nil || !strings.Contains(err.Error(), "1 MiB") {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "people")); !os.IsNotExist(statErr) {
		t.Fatalf("oversize bundle was applied: %v", statErr)
	}
}

func serveSync(t *testing.T, root, token string, hits *int32) *httptest.Server {
	t.Helper()
	info := node.Info{ID: token, Name: token, Caps: []string{"info", Cap}}
	srv := httptest.NewServer(node.Handler(token, info, func(req node.Request) (string, error) {
		if req.Cap != Cap {
			return "", fmt.Errorf("node: unknown cap %q", req.Cap)
		}
		if hits != nil {
			atomic.AddInt32(hits, 1)
		}
		return Accept(root, req.Command)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func remember(t *testing.T, root, key, text string, at time.Time) {
	t.Helper()
	err := memory.Open(root).Remember(context.Background(), memory.Fact{
		Key: key, Scope: memory.ScopePerson, PersonID: "u1", Tier: memory.TierProfile,
		WrittenBy: "ada", Text: text, Time: at,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func texts(t *testing.T, root string) map[string]string {
	t.Helper()
	got, err := memory.Open(root).Recall(context.Background(), memory.Query{Scope: memory.ScopePerson, PersonID: "u1"})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, f := range got {
		out[f.Key] = f.Text
	}
	return out
}

func factText(got, text string) bool {
	return got == text || strings.HasPrefix(got, text+" (untrusted,") || strings.HasPrefix(got, text+" (owner-recorded")
}

func waitFact(t *testing.T, root, key, text string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if factText(texts(t, root)[key], text) {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s=%s, have %+v", key, text, texts(t, root))
}
