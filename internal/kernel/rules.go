package kernel

import (
	"regexp"
	"strings"
)

func (k *Kernel) botRules() (string, error) {
	return k.Bot.MigrateRules(botRuleSupplements)
}

// A recognizable default paragraph belongs to the kernel. An edited sentence
// mixed into that paragraph is intentionally discarded, not guessed back out.
// Standalone bot paragraphs retain their text and order. No historical catalog.
var ruleParagraphBreak = regexp.MustCompile(`\r?\n[ \t]*\r?\n`)

func botRuleSupplements(text string) string {
	defaults := strings.Split(KernelRules(), "\n\n")
	defaults = append(defaults, legacyVerifyRule, OwnAskActionRule, EventMemoryRule, MemoryWriteRule, DispatchRule)
	var kept []string
	for _, paragraph := range ruleParagraphBreak.Split(strings.TrimSpace(text), -1) {
		paragraph = strings.TrimSpace(paragraph)
		if paragraph == "" {
			continue
		}
		known := false
		for _, rule := range defaults {
			// The opening clause identifies a default whose later wording changed.
			opening := rule
			if i := strings.IndexAny(opening, ".:;\n"); i >= 0 {
				opening = opening[:i]
			}
			if len(opening) > 64 {
				opening = opening[:64]
			}
			if strings.HasPrefix(paragraph, opening) {
				known = true
				break
			}
		}
		if !known {
			// Remove standalone defaults on their own lines, never quoted substrings.
			lines := strings.Split(paragraph, "\n")
			var own []string
			for _, line := range lines {
				exact := false
				for _, rule := range defaults {
					if strings.TrimSpace(line) == rule {
						exact = true
						break
					}
				}
				if !exact {
					own = append(own, line)
				}
			}
			if p := strings.TrimSpace(strings.Join(own, "\n")); p != "" {
				kept = append(kept, p)
			}
		}
	}
	return strings.Join(kept, "\n\n")
}
