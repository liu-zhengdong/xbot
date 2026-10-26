package prompts

import "testing"

func TestDeepSeekUsesSharedPrinciplesWithoutExecutionOverlay(t *testing.T) {
	for _, id := range []string{"deepseek-flash", "deepseek-chat", "group/auto-deepseek-v4-1-flash", "deepseek-v4.1-flash"} {
		text, err := Load("", "deepseek", id)
		if err != nil || text != "" {
			t.Fatalf("%s: %q %v", id, text, err)
		}
	}
	for _, pair := range [][2]string{{"openai", "gpt-5"}, {"anthropic", "claude-sonnet"}} {
		text, err := Load("", pair[0], pair[1])
		if err != nil || text != "" {
			t.Fatalf("strong model got overlay: %q %v", text, err)
		}
	}
}
