package main

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"xbot/internal/memory"
	"xbot/internal/node"
	"xbot/internal/nodesync"
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
	if err != nil || len(got) != 1 || got[0].Text != "Shanghai" {
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
