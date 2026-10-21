package prompts

import (
	"strings"
	"testing"
)

func TestDeepSeekExecutionOverlaysMatchRoutedModels(t *testing.T) {
	for _, id := range []string{"deepseek-flash", "deepseek-chat", "group/auto-deepseek-v4-1-flash", "deepseek-v4.1-flash"} {
		text, err := Load("", "deepseek", id)
		if err != nil || !strings.Contains(text, "Act once") || !strings.Contains(text, "待确认") {
			t.Fatalf("%s: %q %v", id, text, err)
		}
		for _, phrase := range []string{"copied production snapshot", "do not edit", "make that handoff now"} {
			if !strings.Contains(text, phrase) {
				t.Fatalf("%s lost boundary %q", id, phrase)
			}
		}
		if len(strings.Split(text, "\n")) > 12 {
			t.Fatalf("%s: longer than 12 lines", id)
		}
	}
	for _, pair := range [][2]string{{"openai", "gpt-5"}, {"anthropic", "claude-sonnet"}} {
		text, err := Load("", pair[0], pair[1])
		if err != nil || text != "" {
			t.Fatalf("strong model got overlay: %q %v", text, err)
		}
	}
}
