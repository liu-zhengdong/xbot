package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/kernel"
	"xbot/internal/memory"
	"xbot/internal/node"
	"xbot/internal/nodesync"
	"xbot/internal/provider"
)

func TestNodeDispatchSyncsWithToken(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()
	when := time.Date(2026, 10, 15, 13, 0, 0, 0, time.UTC)
	if err := memory.Open(a).Remember(context.Background(), memory.Fact{
		Key: "city", Scope: memory.ScopePerson, PersonID: "u1", Tier: memory.TierProfile,
		WrittenBy: "ada", Text: "Shanghai", Time: when,
	}); err != nil {
		t.Fatal(err)
	}
	info := node.Info{ID: "b", Name: "b", Caps: []string{"info", nodesync.Cap}}
	srv := httptest.NewServer(node.Handler("tok", info, nodeDispatch(b, info)))
	defer srv.Close()
	if err := node.Add(a, node.Node{Name: "b", URL: srv.URL, Token: "tok"}); err != nil {
		t.Fatal(err)
	}
	if err := nodesync.Once(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	got, err := memory.Open(b).Recall(context.Background(), memory.Query{Scope: memory.ScopePerson, PersonID: "u1"})
	if err != nil || len(got) != 1 || !strings.HasPrefix(got[0].Text, "Shanghai (untrusted, recorded by ada") || !got[0].Untrusted {
		t.Fatalf("%+v %v", got, err)
	}
	if err := node.Remove(a, "b"); err != nil {
		t.Fatal(err)
	}
	if err := node.Add(a, node.Node{Name: "b", URL: srv.URL, Token: "nope"}); err != nil {
		t.Fatal(err)
	}
	err = nodesync.Once(context.Background(), a)
	if err == nil || !strings.Contains(err.Error(), "unauthorized") {
		t.Fatal(err)
	}
}

func TestNodeDispatchStatusCap(t *testing.T) {
	root := t.TempDir()
	b, err := bot.Create(root, "ada", "p", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Append(bot.Entry{Time: time.Now(), Message: provider.Message{Role: provider.RoleAssistant, Content: "hi"}}); err != nil {
		t.Fatal(err)
	}
	info := node.Info{ID: "x", Name: "x", Caps: node.Caps()}
	text, err := nodeDispatch(root, info)(node.Request{Cap: "status"})
	if err != nil {
		t.Fatal(err)
	}
	var bots []kernel.BotStatus
	if err := json.Unmarshal([]byte(text), &bots); err != nil {
		t.Fatal(err)
	}
	if len(bots) != 1 || bots[0].Bot != "ada" || bots[0].State != kernel.StateIdle {
		t.Fatalf("bots %+v", bots)
	}
	if len(text) > 16<<10 {
		t.Fatalf("status %d bytes", len(text))
	}
	if _, err := nodeDispatch(root, info)(node.Request{Cap: "nope"}); err == nil {
		t.Fatal("unknown cap accepted")
	}
}
