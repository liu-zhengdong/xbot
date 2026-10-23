package mockopenai

import (
	"encoding/json"
	"strings"
	"xbot/internal/bot"
)

// MemoryPathInText lets scripted models read headings in either the fixed
// memory prefix or the canonical JSON values of an environment publication.
// It decodes the value before splitting lines, as a model does when reading it.
func MemoryPathInText(content, scope string) string {
	texts := []string{content}
	const open = bot.EnvironmentOpen
	const close = bot.EnvironmentClose
	if strings.HasPrefix(content, open) && strings.HasSuffix(content, close) {
		var p struct {
			Set map[string]json.RawMessage `json:"set"`
		}
		if json.Unmarshal([]byte(strings.TrimSuffix(strings.TrimPrefix(content, open), close)), &p) == nil {
			texts = nil
			for _, value := range p.Set {
				var text string
				if json.Unmarshal(value, &text) == nil {
					texts = append(texts, text)
				}
			}
		}
	}
	prefix := "### " + scope + " — "
	for _, text := range texts {
		for _, line := range strings.Split(text, "\n") {
			if strings.HasPrefix(line, prefix) {
				return strings.SplitN(strings.TrimPrefix(line, prefix), ";", 2)[0]
			}
		}
	}
	return ""
}
