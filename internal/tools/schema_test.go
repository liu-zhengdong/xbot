package tools

import (
	"encoding/json"
	"testing"
)

func TestObjectSchemaRequiredNeverNull(t *testing.T) {
	raw, err := json.Marshal(objectSchema(map[string]any{}, nil))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if string(m["required"]) != "[]" {
		t.Fatalf("required=%s, want []", m["required"])
	}
}
