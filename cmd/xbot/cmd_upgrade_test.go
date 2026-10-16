package main

import (
	"strings"
	"testing"

	"xbot/internal/version"
)

func TestVersionIsStamped(t *testing.T) {
	if strings.TrimSpace(version.Version) == "" {
		t.Fatal("version.Version is empty")
	}
}

func TestUpgradeUsageRejectsExtraArgs(t *testing.T) {
	// reorderFlags keeps positional args; cmdUpgrade must reject them.
	if err := cmdUpgrade("", []string{"extra"}); err == nil {
		t.Fatal("upgrade accepted a positional argument")
	}
}

func TestUsageMentionsVersionAndUpgrade(t *testing.T) {
	for _, want := range []string{"xbot version", "xbot [--root DIR] upgrade [--check]"} {
		if !strings.Contains(usageText, want) {
			t.Fatalf("usage missing %q", want)
		}
	}
}
