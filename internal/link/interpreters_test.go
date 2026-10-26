package link

import (
	"encoding/json"
	"testing"
	"xbot/internal/review"
	"xbot/internal/tools"
)

func TestLocalToolCatalogUsesInstalledInterpreters(t *testing.T) {
	got := map[string]tools.Tool{}
	for _, tool := range NewToolsLink().Tools() {
		got[tool.Name()] = tool
	}
	if got["shell"] != nil {
		t.Fatal("generic shell is reachable")
	}
	for _, sh := range tools.NewShells() {
		tool := got[sh.Name()]
		if tool == nil {
			t.Fatalf("missing %s", sh.Name())
		}
		if review.ClassOf(sh.Name()) != review.Shell {
			t.Fatal("unclassified interpreter")
		}
		raw, _ := json.Marshal(tool.Parameters())
		var schema struct {
			Required   []string
			Properties map[string]any
		}
		json.Unmarshal(raw, &schema)
		found := false
		for _, name := range schema.Required {
			if name == "description" {
				found = true
			}
		}
		if !found || schema.Properties["description"] == nil {
			t.Fatalf("missing purpose: %s", raw)
		}
	}
}
