package kernel

import (
	"encoding/json"
	"testing"
)

func TestAskUserRequiresRecommendedOption(t *testing.T) {
	for _, args := range []string{
		`{"question":"下一步？","options":["A","B"]}`,
		`{"question":"下一步？","options":["A","B"],"recommended":"C"}`,
		`{"question":"下一步？","options":["A","B"],"recommended":1}`,
	} {
		if _, _, err := (askTool{}).Confirm(json.RawMessage(args)); err == nil {
			t.Fatalf("accepted %s", args)
		}
	}
	if _, _, err := (askTool{}).Confirm(json.RawMessage(`{"question":"下一步？","options":["A","B"],"recommended":"B"}`)); err != nil {
		t.Fatal(err)
	}
}
