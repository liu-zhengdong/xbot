package kernel

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/tools"
)

// IdleGap is how long a bot must sit between wakes before the same hour is reported again.
const IdleGap = 30 * time.Minute

// StablePrefix is the system prompt. It depends only on persona, rules, and granted link names.
// Tool schemas, memory, time, and the sender are not included, so provider prefix caches can hit.
func StablePrefix(persona, rules string, links []string) string {
	persona = strings.TrimSpace(persona)
	if persona == "" {
		persona = bot.DefaultPersona
	}
	rules = strings.TrimSpace(rules)
	if rules == "" {
		rules = DefaultRules()
	}
	names := append([]string(nil), links...)
	sort.Strings(names)
	var b strings.Builder
	b.WriteString(persona)
	b.WriteString("\n\n")
	b.WriteString(rules)
	if len(names) > 0 {
		b.WriteString("\n\n## Links\n")
		for _, name := range names {
			fmt.Fprintf(&b, "- %s: %s\n", name, link.Blurb(name))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// DiffEnv returns one compact environment update, or empty when nothing the model needs has changed.
// The first snapshot is a short baseline. Later wakes list only changed fields.
// The hour is included when the hour bucket changes or the previous wake was at least IdleGap ago.
func DiffEnv(prev, next bot.EnvRecord) string {
	if prev.At.IsZero() {
		return baseline(next)
	}
	var lines []string
	if prev.Sender != next.Sender {
		lines = append(lines, "sender: "+next.Sender)
	}
	if !sameStrings(prev.Tools, next.Tools) {
		lines = append(lines, "tools: "+toolChange(prev.Tools, next.Tools))
	}
	if prev.Notes != next.Notes {
		lines = append(lines, fmt.Sprintf("notes: %d", next.Notes))
	}
	if prev.Persona != next.Persona {
		lines = append(lines, "persona: updated")
	}
	if prev.Profile != next.Profile && (prev.Profile != "" || next.Profile != "") {
		lines = append(lines, "profile: updated")
	}
	if prev.Rules != next.Rules {
		lines = append(lines, "rules: updated")
	}
	if prev.Memory != next.Memory {
		lines = append(lines, "memory: updated")
	}
	if prev.Skills != next.Skills && (prev.Skills != "" || next.Skills != "") {
		lines = append(lines, "skills: updated")
	}
	if prev.Hour != next.Hour || next.At.Sub(prev.At) >= IdleGap {
		lines = append(lines, "time: "+next.Hour)
	}
	if next.Now != "" && prev.Now != next.Now {
		lines = append(lines, "now: "+next.Now)
	}
	if len(lines) == 0 {
		return ""
	}
	return "<environment_update>\n" + strings.Join(lines, "\n") + "\n</environment_update>"
}

func baseline(e bot.EnvRecord) string {
	body := fmt.Sprintf("time: %s\n", e.Hour)
	if e.Now != "" {
		body += "now: " + e.Now + "\n"
	}
	body += fmt.Sprintf("sender: %s\ntools: %s\nnotes: %d\npersona: %s\nrules: %s\nmemory: %s\n",
		e.Sender, strings.Join(e.Tools, ","), e.Notes, e.Persona, e.Rules, e.Memory)
	if e.Skills != "" {
		body += "skills: " + e.Skills + "\n"
	}
	if e.Profile != "" {
		body += "profile: " + e.Profile + "\n"
	}
	return "<environment_update>\n" + body + "</environment_update>"
}

func snapshotText(e bot.EnvRecord) string {
	body := fmt.Sprintf("time: %s\n", e.Hour)
	if e.Now != "" {
		body += "now: " + e.Now + "\n"
	}
	body += fmt.Sprintf("sender: %s\ntools: %s\nnotes: %d\npersona: %s\nrules: %s\nmemory: %s\n",
		e.Sender, strings.Join(e.Tools, ","), e.Notes, e.Persona, e.Rules, e.Memory)
	if e.Skills != "" {
		body += "skills: " + e.Skills + "\n"
	}
	if e.Profile != "" {
		body += "profile: " + e.Profile + "\n"
	}
	return "<environment_snapshot>\n" + body + "</environment_snapshot>"
}

func toolChange(prev, next []string) string {
	have := map[string]bool{}
	for _, n := range prev {
		have[n] = true
	}
	want := map[string]bool{}
	for _, n := range next {
		want[n] = true
	}
	var parts []string
	for _, n := range next {
		if !have[n] {
			parts = append(parts, "+"+n)
		}
	}
	for _, n := range prev {
		if !want[n] {
			parts = append(parts, "-"+n)
		}
	}
	sort.Strings(parts)
	return strings.Join(parts, " ")
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:6])
}

func (k *Kernel) now() time.Time {
	if k.Now != nil {
		return k.Now().UTC()
	}
	return time.Now().UTC()
}

// localStamp is the owner's clock to the minute, for the environment block only.
// It is not part of the stable prefix.
func (k *Kernel) localStamp() string {
	utc := k.now()
	name := "UTC"
	loc := time.UTC
	if k != nil && k.Bot != nil {
		if tz := strings.TrimSpace(k.Bot.Config().Timezone); tz != "" {
			loaded, err := time.LoadLocation(tz)
			if err == nil {
				loc = loaded
				name = tz
			}
		} else if time.Local != nil {
			loc = time.Local
			name = loc.String()
		}
	}
	local := utc.In(loc).Truncate(time.Minute)
	return local.Format(time.RFC3339) + " (" + name + ")"
}

func (k *Kernel) linkNames() []string {
	seen := map[string]bool{}
	var names []string
	for _, l := range k.Links {
		if l == nil || seen[l.Name()] {
			continue
		}
		seen[l.Name()] = true
		names = append(names, l.Name())
	}
	sort.Strings(names)
	return names
}

func toolNames(list []tools.Tool) []string {
	seen := map[string]bool{}
	var names []string
	for _, t := range list {
		if t == nil || seen[t.Name()] {
			continue
		}
		seen[t.Name()] = true
		names = append(names, t.Name())
	}
	sort.Strings(names)
	return names
}
