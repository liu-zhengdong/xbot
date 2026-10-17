package kernel

import (
	"strings"
	"testing"
)

func TestSilenceRuleDefersToRoutineFallback(t *testing.T) {
	if !strings.Contains(SilenceRule, "instead of SILENT") {
		t.Fatalf("SilenceRule does not defer to a routine fallback: %q", SilenceRule)
	}
	if !strings.Contains(SilenceRule, "A denied tool is not nothing") {
		t.Fatalf("SilenceRule lets a denied tool count as nothing: %q", SilenceRule)
	}
}
