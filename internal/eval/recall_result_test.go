package eval

import (
	"testing"
	"xbot/internal/bot"
	"xbot/internal/provider"
)

func TestRecallResultRequired(t *testing.T) {
	cases, _, err := LoadSuite("", "proactivity")
	if err != nil {
		t.Fatal(err)
	}
	var c Case
	for _, one := range cases {
		if one.Name == "memory-recall" {
			c = one
		}
	}
	obs := Observation{Assistant: "violet-orbit-73 projects/x.md", Tools: []ToolUse{{Name: "recall"}}}
	for _, result := range []string{"no matches", "violet-orbit-73", "bot:projects/x.md:1", "violet-orbit-73 bot:projects/x.md:1"} {
		obs.entries = []bot.Entry{{Message: provider.Message{Role: "tool", Name: "recall", Content: provider.TextContent(result)}}}
		problems, _ := grade(c, obs, nil, t.TempDir(), nil)
		wantPass := result == "violet-orbit-73 bot:projects/x.md:1"
		if (len(problems) == 0) != wantPass {
			t.Fatalf("result %q: %v", result, problems)
		}
	}
}
