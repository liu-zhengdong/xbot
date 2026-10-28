package kernel

import (
	"strings"
	"testing"
)

func TestSilenceRuleDefersToRoutineFallback(t *testing.T) {
	if !strings.Contains(SilenceRule, "不回 SILENT") {
		t.Fatalf("SilenceRule does not defer to a routine fallback: %q", SilenceRule)
	}
	if !strings.Contains(SilenceRule, "工具被拒不算什么也没发现") {
		t.Fatalf("SilenceRule lets a denied tool count as nothing: %q", SilenceRule)
	}
}
