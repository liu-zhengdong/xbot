package kernel

import (
	"encoding/json"
	"testing"

	"xbot/internal/provider"
)

func TestFoldDropsOnlySupersededToolIndex(t *testing.T) {
	old := encodeEnvironment(environmentMessage{Snapshot: "old", Set: map[string]json.RawMessage{"tool_index": json.RawMessage(`{"memory":["recall"]}`), "source": json.RawMessage(`{"name":"terminal"}`)}})
	msgs := []provider.Message{{Role: provider.RoleUser, Internal: true, Content: provider.TextContent(old)}, {Role: provider.RoleAssistant, Content: provider.TextContent("earlier")}, {Role: provider.RoleUser, Content: provider.TextContent("latest")}}
	f := newTurnFit(1, nil)
	f.cut = 2
	if view := f.view(msgs); view[0].Text() != old {
		t.Fatal("a caller without environment refresh lost the index")
	}
	f.refreshEnvironment = true
	f.summary = "earlier work"
	view := f.view(msgs)
	env, ok := decodeEnvironment(view[0].Text())
	if !ok {
		t.Fatal("environment lost")
	}
	if _, ok := env.Set["tool_index"]; ok {
		t.Fatal("obsolete index retained")
	}
	if string(env.Set["source"]) != `{"name":"terminal"}` || env.Snapshot != "old" {
		t.Fatalf("unrelated environment changed: %+v", env)
	}
	fresh := encodeEnvironment(environmentMessage{Snapshot: "new", Set: map[string]json.RawMessage{"tool_index": json.RawMessage(`{"memory":["recall"],"browser":["browser_open"]}`), "source": json.RawMessage(`{"name":"terminal"}`)}})
	view = append(view, provider.Message{Role: provider.RoleUser, Internal: true, Content: provider.TextContent(fresh)})
	state := replayEnvironment(view, "")
	if string(state.Fields["tool_index"]) != `{"memory":["recall"],"browser":["browser_open"]}` || state.Basis != "new" {
		t.Fatalf("fresh index not replayed: %+v", state)
	}
	if msgs[0].Text() != old {
		t.Fatal("original conversation changed")
	}
}
