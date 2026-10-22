package kernel

import (
	"strings"
	"testing"
	"time"
	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/provider"
)

func TestRequestContextSkipsForkMarkers(t *testing.T) {
	b := testBot(t)
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	k := &Kernel{Bot: b}
	first, err := k.receive(link.TerminalEvent("u1", "opening", time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	start, err := b.AppendFork("bg_marker", first, "background")
	if err != nil {
		t.Fatal(err)
	}
	end, err := b.AppendForkEnd("bg_marker", bot.ForkDone, first)
	if err != nil {
		t.Fatal(err)
	}
	_, err = k.receive(link.TerminalEvent("u1", "later words", time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	r := &turnRun{k: k, assembled: []provider.Message{{Role: provider.RoleUser, Content: "opening", Seq: first}}}
	req, err := r.requestContext(provider.Request{Messages: r.assembled})
	if err != nil {
		t.Fatal(err)
	}
	later := 0
	for _, msg := range req.Messages {
		if msg.Seq == start || msg.Seq == end || (msg.Role == provider.RoleUser && strings.TrimSpace(msg.Content) == "") {
			t.Fatalf("fork marker leaked: %+v", msg)
		}
		if msg.Content == "later words" {
			later++
		}
	}
	if later != 1 {
		t.Fatalf("normal new input lost/duplicated: %v", req.Messages)
	}
}
