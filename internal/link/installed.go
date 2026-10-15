package link

import (
	"fmt"
	"strings"
)

// InstalledCap is the byte budget for the installed-link block in the system prompt.
const InstalledCap = 1024

// InstalledRef is one installed link: a catalog id or mcp:<server>.
type InstalledRef struct {
	ID      string
	Summary string
}

// InstalledSection renders one line per link. Past InstalledCap it lists ids only,
// so summaries stay available through links search instead of the prompt.
func InstalledSection(refs []InstalledRef) string {
	if len(refs) == 0 {
		return ""
	}
	full := renderInstalled(refs, true)
	if len(full) <= InstalledCap {
		return full
	}
	return renderInstalled(refs, false)
}

func renderInstalled(refs []InstalledRef, summaries bool) string {
	var b strings.Builder
	b.WriteString("## Installed\n")
	for _, r := range refs {
		id := strings.TrimSpace(r.ID)
		if id == "" {
			continue
		}
		if summaries {
			summary := oneLine(r.Summary)
			if summary != "" {
				fmt.Fprintf(&b, "- %s: %s\n", id, summary)
				continue
			}
		}
		fmt.Fprintf(&b, "- %s\n", id)
	}
	return strings.TrimRight(b.String(), "\n")
}

func oneLine(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return strings.Join(strings.Fields(s), " ")
}
