package bot

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestLogDecisionWritesLocalOffset(t *testing.T) {
	saved := time.Local
	time.Local = time.FixedZone("TEST", 8*60*60)
	defer func() { time.Local = saved }()

	root := t.TempDir()
	b, err := Create(root, "ada", "Be brief.", nil)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 16, 1, 30, 0, 0, time.UTC)
	if err := b.LogDecision(DecisionAllowed, "shell", "rm x", at); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(b.DecisionPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "2026-10-16T09:30:00+08:00") {
		t.Fatalf("decision time not local with offset: %s", raw)
	}
	if strings.HasSuffix(strings.TrimSpace(string(raw)), "Z") || strings.Contains(string(raw), `"time":"2026-10-16T01:30:00Z"`) {
		t.Fatalf("decision time written as UTC: %s", raw)
	}
}
