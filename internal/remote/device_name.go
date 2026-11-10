package remote

import (
	"os"
	"strings"
)

// DefaultDeviceName returns a short display name for this computer.
// Saved names and bot@device addresses use their existing paths.
func DefaultDeviceName() string {
	host, _ := os.Hostname()
	return defaultDeviceName(host)
}

func defaultDeviceName(host string) string {
	host, _, _ = strings.Cut(strings.TrimSpace(host), ".")
	compact := strings.ToLower(strings.NewReplacer("-", "", "_", "", " ", "").Replace(host))
	for _, name := range [...]string{"MacBook Pro", "MacBook Air", "MacBook", "Mac mini", "Mac Studio", "iMac Pro", "Mac Pro", "iMac"} {
		model := strings.ToLower(strings.ReplaceAll(name, " ", ""))
		if strings.HasSuffix(compact, model) {
			return name
		}
	}
	if host == "" {
		return "这台电脑"
	}
	return host
}
