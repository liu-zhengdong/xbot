package kernel

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"xbot/internal/bot"
	"xbot/internal/clock"
	"xbot/internal/code"
	"xbot/internal/exec"
	"xbot/internal/link"
	"xbot/internal/tools"
)

const (
	personaSection = "### 职责\n这段写你是谁、负责什么、边界在哪；下面是正文，照着做，不要复述本说明。\n"
	rulesSection   = "### 内核规矩\nXBot 硬规矩，与职责和人设卡无关；冲突时本段优先。\n"
	modelSection   = "### 模型补丁\n仅针对当前模型的额外约束。\n"
	linksSection   = "### 能力框\n当前授权的连接/可发现能力；细节按需 load，别假装有未列出的。\n"
)

// StablePrefix is the system prompt. It depends only on persona, rules, the model overlay, and granted link names.
// Tool schemas, memory, time, and the sender are not included, so provider prefix caches can hit.
// An empty overlay leaves the prefix unchanged.
func StablePrefix(persona, rules, overlay string, links []string) string {
	persona = strings.TrimSpace(persona)
	if persona == "" {
		persona = bot.DefaultPersona
	}
	rules = strings.TrimSpace(rules)
	if rules == "" {
		rules = KernelRules()
	}
	overlay = strings.TrimSpace(overlay)
	linkBlock := linksPrefix(links)
	size := len(personaSection) + len(persona) + 2 + len(rulesSection) + len(rules)
	if overlay != "" {
		size += 2 + len(modelSection) + len(overlay)
	}
	if linkBlock != "" {
		size += 2 + len(linkBlock)
	}
	var b strings.Builder
	b.Grow(size)
	b.WriteString(personaSection)
	b.WriteString(persona)
	b.WriteString("\n\n")
	b.WriteString(rulesSection)
	b.WriteString(rules)
	if overlay != "" {
		b.WriteString("\n\n")
		b.WriteString(modelSection)
		b.WriteString(overlay)
	}
	if linkBlock != "" {
		b.WriteString("\n\n")
		b.WriteString(linkBlock)
	}
	return strings.TrimRight(b.String(), "\n")
}

func linksPrefix(links []string) string {
	names := append([]string(nil), links...)
	sort.Strings(names)
	var b strings.Builder
	if len(names) > 0 {
		b.WriteString(linksSection)
		for _, name := range names {
			fmt.Fprintf(&b, "- %s: %s\n", name, link.Blurb(name))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:6])
}

func (k *Kernel) now() time.Time {
	if k.Now != nil {
		return k.Now().UTC()
	}
	return clock.Now().UTC()
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
	for _, l := range k.snapshotLinks() {
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

// recentLogHeading starts the snapshot's recent-log section.
const recentLogHeading = "### Recent log"

// splitRecentLog splits rendered memory into the main part and the recent-log
// entries, without the section heading. The section always renders last.
func splitRecentLog(s string) (string, string) {
	i := strings.LastIndex(s, "\n"+recentLogHeading)
	if i < 0 {
		return s, ""
	}
	rest := s[i+1:]
	if j := strings.IndexByte(rest, '\n'); j >= 0 {
		return s[:i], rest[j+1:]
	}
	return s[:i], ""
}

func nonBlank(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

// factLines returns the non-blank lines of the main memory part, each tagged
// with its section ("[Person] - fact"). Heading lines are dropped: they come and
// go with the sections and are not facts.
func factLines(s string) []string {
	var out []string
	section := ""
	for _, l := range nonBlank(s) {
		if strings.HasPrefix(l, "### ") {
			section = strings.TrimSpace(strings.TrimPrefix(l, "### "))
			// Locations already belong to the fixed prefix, not to every fact.
			// Repeating a long root here can make a valid 4 KiB memory receipt
			// exceed the existing tool-result limit. Keep only its logical scope.
			section, _, _ = strings.Cut(section, " — ")
			continue
		}
		if strings.HasPrefix(l, "#") {
			continue
		}
		if section != "" {
			l = "[" + section + "] " + l
		}
		out = append(out, l)
	}
	return out
}

func removeOnce(lines []string, x string) []string {
	for i, l := range lines {
		if l == x {
			return append(lines[:i:i], lines[i+1:]...)
		}
	}
	return lines
}

// multisetDiff returns the lines only in b (added) and only in a (removed),
// counting repeats, each in its own order.
func multisetDiff(a, b []string) (added, removed []string) {
	count := map[string]int{}
	for _, l := range a {
		count[l]++
	}
	for _, l := range b {
		if count[l] > 0 {
			count[l]--
		} else {
			added = append(added, l)
		}
	}
	for _, l := range a {
		if count[l] > 0 {
			count[l]--
			removed = append(removed, l)
		}
	}
	return added, removed
}

// taskEnvironment reads status from its existing owner, the executor. Terminal
// changes are kept for one comparison; the full report remains in the dialogue.
func (k *Kernel) taskEnvironment(prev []bot.TaskEnv, ev link.Event) ([]bot.TaskEnv, error) {
	var out []bot.TaskEnv
	seen := map[string]bool{}
	if e := k.execSnapshot(); e != nil {
		tasks, err := e.Tasks()
		if err != nil {
			return nil, err
		}
		for _, task := range tasks {
			status := task.Status
			if task.Blocked {
				status = exec.EvBlocked
			}
			out = append(out, bot.TaskEnv{ID: task.ID, Task: strings.TrimSpace(task.Task), Status: status, Position: task.Position})
			seen[task.ID] = true
		}
	}
	for _, l := range k.snapshotLinks() {
		if coding, ok := l.(*code.Link); ok {
			tasks, err := coding.Record().List()
			if err != nil {
				return nil, err
			}
			for _, task := range tasks {
				if task.Status == code.StatusRunning || task.Status == code.StatusQueued {
					out = append(out, bot.TaskEnv{ID: task.ID, Task: strings.TrimSpace(task.Goal), Status: task.Status})
					seen[task.ID] = true
				}
			}
		}
	}
	for _, bg := range k.Backgrounds() {
		if !seen[bg.ID] {
			out = append(out, bot.TaskEnv{ID: bg.ID, Task: bg.Task, Status: "running"})
			seen[bg.ID] = true
		}
	}
	for _, old := range prev {
		if seen[old.ID] || (old.Status != "running" && old.Status != "queued" && old.Status != exec.EvBlocked) {
			continue
		}
		if strings.HasPrefix(old.ID, "ca_") {
			for _, l := range k.snapshotLinks() {
				if coding, ok := l.(*code.Link); ok {
					task, err := coding.Record().Get(old.ID)
					if err != nil {
						return nil, err
					}
					old.Status, old.Position = task.Status, 0
					out = append(out, old)
					seen[old.ID] = true
				}
			}
			continue
		}
		meta, err := exec.ReadMeta(k.Bot.AgentDir(old.ID))
		if err != nil || ((meta.Status == "running" || meta.Status == "queued") && !tools.SameProcess(meta.PID, meta.ProcStart)) {
			// Reuse recovery's process proof even if writing its terminal meta
			// failed; a readable but stale "running" record is not live work.
			meta.Status = bot.ForkInterrupted
			meta.Blocked = false
		}
		old.Status = meta.Status
		if meta.Blocked {
			old.Status = exec.EvBlocked
		}
		if meta.Status != "queued" {
			old.Position = 0
		}
		out = append(out, old)
		seen[old.ID] = true
	}
	if ev.Source == link.SourceSubagent && !seen[ev.Sender] && (ev.Type == exec.EvDone || ev.Type == exec.EvFailed || ev.Type == exec.EvBlocked || ev.Type == "fork_end") {
		out = append(out, bot.TaskEnv{ID: ev.Sender, Status: ev.Type})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
