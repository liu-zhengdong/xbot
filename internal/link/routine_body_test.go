package link

import (
	"strings"
	"testing"
)

func TestRoutineBodyKeepsPromptAndFallbackPrecedence(t *testing.T) {
	body := RoutineBody("rt1", "Check mail", "没有内容就说今天还没聊")
	if !strings.Contains(body, "<routine id=\"rt1\" name=\"Check mail\">\n没有内容就说今天还没聊\n</routine>") {
		t.Fatalf("routine prompt not wrapped: %q", body)
	}
	if !strings.Contains(body, "If the routine says what to send when there is nothing") {
		t.Fatalf("missing empty-case precedence: %q", body)
	}
	if !strings.Contains(body, "do not reply SILENT") {
		t.Fatalf("fallback does not override SILENT: %q", body)
	}
	if !strings.Contains(body, "reply with exactly SILENT") {
		t.Fatalf("default SILENT lost: %q", body)
	}
}
