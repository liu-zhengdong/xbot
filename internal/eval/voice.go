package eval

import (
	"fmt"
	"regexp"
	"strings"

	"xbot/internal/bot"
	"xbot/internal/provider"
)

// VoiceStats counts occurrences, not a verdict on whether a technical detail
// was useful. Code counts inline spans and fenced blocks, once per pair.
type VoiceStats struct {
	Paths     int `json:"paths"`
	Internals int `json:"internals"`
	Code      int `json:"code"`
}

// VoiceCheck limits noise for a case whose owner needs no technical details.
// Reference is an example of a good reply, not an exact-match model grader.
type VoiceCheck struct {
	MaxPaths     int    `yaml:"max_paths"`
	MaxInternals int    `yaml:"max_internals"`
	MaxCode      int    `yaml:"max_code"`
	Reference    string `yaml:"reference"`
}

var voicePaths = regexp.MustCompile(`(?:[A-Za-z]:[\\/]|~/|/)[\p{L}\p{N}_.~\\/-]+|[\p{L}\p{N}_.-]+(?:/[\p{L}\p{N}_.-]+)+/?|[\p{L}\p{N}_.-]+/|[\p{L}\p{N}_-]+\.(?:md|go|js|css|html|json|yaml|yml|txt)\b`)
var voiceIDs = regexp.MustCompile(`\b(?:w[0-9]+|(?:bg|sa|sc)_[A-Za-z0-9_-]+)\b`)

func CountVoice(text string, toolNames []string) VoiceStats {
	v := VoiceStats{Paths: len(voicePaths.FindAllString(text, -1)), Internals: len(voiceIDs.FindAllString(text, -1))}
	// Deduplicate names supplied by repeated model requests.
	seen := map[string]bool{}
	for _, name := range append([]string{"read_file", "write_file", "edit_file", "dispatch", "spawn", "read_subagent", "notify_owner", "ask_user", "propose_profile"}, toolNames...) {
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		re := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`)
		v.Internals += len(re.FindAllString(text, -1))
	}
	for i := 0; i < len(text); {
		if text[i] != '`' {
			i++
			continue
		}
		end := i
		for end < len(text) && text[end] == '`' {
			end++
		}
		mark := text[i:end]
		if close := strings.Index(text[end:], mark); close >= 0 {
			v.Code++
			i = end + close + len(mark)
		} else {
			i = end
		}
	}
	return v
}
func voiceReplies(entries []bot.Entry, names []string) []VoiceStats {
	var out []VoiceStats
	for _, e := range entries {
		if e.Message.Role == provider.RoleAssistant && !e.IsInternal() && strings.TrimSpace(e.Message.Content) != "" {
			out = append(out, CountVoice(e.Message.Content, names))
		}
	}
	return out
}
func gradeVoice(want *VoiceCheck, replies []VoiceStats) []string {
	if want == nil {
		return nil
	}
	if len(replies) == 0 {
		return []string{"voice: no reply"}
	}
	var problems []string
	for i, v := range replies {
		if v.Paths > want.MaxPaths || v.Internals > want.MaxInternals || v.Code > want.MaxCode {
			problems = append(problems, fmt.Sprintf("voice reply %d: paths=%d internals=%d code=%d", i+1, v.Paths, v.Internals, v.Code))
		}
	}
	return problems
}
