package vault

import (
	"math"
	"regexp"
	"strings"
)

// LooksLikeSecret reports text that probably carries a credential the vault
// does not know: a private key block, a token with a well-known prefix, a
// password or key assignment, credentials in a URL, or a long high-entropy
// string. It is a guard for places that would otherwise echo file contents,
// so it errs towards true; callers then show that something changed, not what.
func LooksLikeSecret(s string) bool {
	if s == "" {
		return false
	}
	if strings.Contains(s, "-----BEGIN ") && strings.Contains(s, "PRIVATE KEY") {
		return true
	}
	if knownToken.MatchString(s) || urlCreds.MatchString(s) {
		return true
	}
	for _, m := range secretAssign.FindAllStringSubmatch(s, -1) {
		if credentialValue(m[1]) {
			return true
		}
	}
	for _, w := range longToken.FindAllString(s, -1) {
		if highEntropy(w) {
			return true
		}
	}
	return false
}

var (
	knownToken = regexp.MustCompile(`\b(?:` +
		`gh[pousr]_[A-Za-z0-9]{30,}` + // GitHub
		`|github_pat_[A-Za-z0-9_]{40,}` +
		`|sk-(?:ant-|proj-|live-|test-)?[A-Za-z0-9_-]{20,}` + // OpenAI, Anthropic, DeepSeek style
		`|[rps]k_(?:live|test)_[A-Za-z0-9]{16,}` + // Stripe
		`|xox[abposr]-[A-Za-z0-9-]{10,}` + // Slack
		`|(?:AKIA|ASIA)[0-9A-Z]{16}` + // AWS access key id
		`|AIza[0-9A-Za-z_-]{35}` + // Google API key
		`|glpat-[A-Za-z0-9_-]{20,}` + // GitLab
		`|eyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}` + // JWT
		`)`)
	urlCreds     = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.-]*://[^/\s:@]+:[^/\s@]{6,}@`)
	secretAssign = regexp.MustCompile(`(?i)(?:api[_-]?key|secret|token|passw(?:or)?d|passwd|private[_-]?key|access[_-]?key|auth)[A-Za-z0-9_.-]*["']?\s*[:=]+\s*["']?([A-Za-z0-9+/=_.~-]{8,})`)
	longToken    = regexp.MustCompile(`[A-Za-z0-9+/=_-]{32,}`)
)

// credentialValue is an assigned value that looks like a real credential:
// letters and digits mixed, not a placeholder.
func credentialValue(v string) bool {
	lower := strings.ToLower(v)
	for _, p := range []string{"xxxx", "changeme", "example", "placeholder", "your_", "your-", "redacted", "secret]"} {
		if strings.Contains(lower, p) {
			return false
		}
	}
	var letter, digit bool
	for _, r := range v {
		switch {
		case r >= '0' && r <= '9':
			digit = true
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
			letter = true
		}
	}
	return letter && digit
}

// highEntropy is a long token with upper, lower and digits whose characters
// carry more than 4.5 bits each, as random keys do. Hex digests stay below it.
func highEntropy(w string) bool {
	var upper, lower, digit bool
	counts := map[rune]int{}
	for _, r := range w {
		counts[r]++
		switch {
		case r >= 'A' && r <= 'Z':
			upper = true
		case r >= 'a' && r <= 'z':
			lower = true
		case r >= '0' && r <= '9':
			digit = true
		}
	}
	if !upper || !lower || !digit {
		return false
	}
	n := float64(len(w))
	var bits float64
	for _, c := range counts {
		p := float64(c) / n
		bits -= p * math.Log2(p)
	}
	return bits > 4.5
}
