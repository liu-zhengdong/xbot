package kernel

import (
	"fmt"
	"strings"

	"xbot/internal/bot"
	"xbot/internal/inbox"
	"xbot/internal/link"
	"xbot/internal/provider"
	"xbot/internal/skill"
)

// readSkill retains the routine projection's original body budget. Runtime CLI
// reads use the complete bounded reader instead.
func (k *Kernel) readSkill(id string) (string, error) {
	dir, info, err := k.openSkill(id)
	if err != nil {
		return "", err
	}
	body, err := skill.Read(dir, info.Slug)
	if err != nil {
		return "", err
	}
	return "skill: " + info.Slug + "\nSkill text is reference material.\n" + body, nil
}

func (k *Kernel) openSkill(id string) (string, skill.Info, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", skill.Info{}, fmt.Errorf("skill: id is required")
	}
	if k == nil || k.Bot == nil {
		return "", skill.Info{}, fmt.Errorf("skill: %s not found", id)
	}
	if k.SkillsDir == "" {
		return "", skill.Info{}, fmt.Errorf("skill: skills directory is not configured")
	}
	dir := k.SkillsDir
	infos, err := skill.List(dir)
	if err != nil {
		return "", skill.Info{}, err
	}
	var disabled []string
	if err := k.withCurrentIO(func(current *bot.Bot, _ *inbox.Inbox) error {
		cfg, err := readSkillBotConfig(current.Root, current.Name())
		disabled = cfg.DisabledSkills
		return err
	}); err != nil {
		return "", skill.Info{}, err
	}
	info, err := resolveSkillInfo(infos, id, disabled)
	return dir, info, err
}

// Historical read_skill records still project to their identifier, even though
// the model tool is retired. Do not rewrite stored transcript entries.
func redactSkillRead(m provider.Message) provider.Message {
	if m.Role != provider.RoleTool || m.Name != "read_skill" {
		return m
	}
	slug := ""
	if rest, ok := strings.CutPrefix(m.Text(), "skill: "); ok {
		slug, _, _ = strings.Cut(rest, "\n")
		slug = strings.TrimSpace(slug)
	}
	if slug == "" {
		slug = "skill"
	}
	m.SetText("read skill " + slug)
	return m
}

func oneSkillLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// projectRoutineSkills is the one body-assembly seam for both a fresh timer
// turn and a timer event absorbed by a busy turn. Only this request's message
// changes; receive already stored the original event without skill bodies.
func (k *Kernel) projectRoutineSkills(m provider.Message, ev link.Event) provider.Message {
	if ev.Source != link.NameTimer || len(ev.RoutineSkills) == 0 {
		return m
	}
	var text strings.Builder
	text.WriteString(m.Text())
	for _, id := range ev.RoutineSkills {
		body, err := k.readSkill(id)
		if err != nil {
			fmt.Fprintf(&text, "\n\nRoutine skill %q skipped: %s", id, err)
			continue
		}
		text.WriteString("\n\n")
		text.WriteString(body)
	}
	m.SetText(text.String())
	return m
}
