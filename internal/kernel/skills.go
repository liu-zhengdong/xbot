package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/inbox"
	"xbot/internal/link"
	"xbot/internal/provider"
	"xbot/internal/skill"
)

type skillTool struct{ k *Kernel }

// skillsAt names the runtime skills directory for tool descriptions; empty when unset.
func (k *Kernel) skillsAt() string {
	if k == nil || k.SkillsDir == "" {
		return ""
	}
	return " at " + k.SkillsDir
}

func (skillTool) Name() string { return "read_skill" }
func (t skillTool) Description() string {
	return "Read a skill from the local skill library" + t.k.skillsAt() + ". Pass id, the skill id from the Skills list. The text is for this turn only."
}
func (skillTool) Parameters() any {
	return schema(map[string]any{
		"id": stringProp("Skill id from the Skills list"),
	}, []string{"id"})
}
func (skillTool) Confirm(json.RawMessage) (bool, string, error) { return false, "read_skill", nil }
func (t skillTool) Run(_ context.Context, args json.RawMessage) (string, error) {
	var in struct {
		ID   string `json:"id"`
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	id := strings.TrimSpace(in.ID)
	if id == "" {
		id = strings.TrimSpace(in.Slug)
	}
	return t.k.readSkill(id)
}

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

type writeSkillTool struct{ k *Kernel }

func (writeSkillTool) Name() string { return "write_skill" }
func (t writeSkillTool) Description() string {
	at := " as <slug>/SKILL.md"
	if t.k != nil && t.k.SkillsDir != "" {
		at = " at " + filepath.Join(t.k.SkillsDir, "<slug>", "SKILL.md")
	}
	return "Create or replace a skill directly in the local skill library" + at + ". Call only after the owner confirms a proposal or explicitly requests writing a skill. Supply title as a short human-readable label, e.g. 记一下; name remains the slug. When the current request says only for you or a named bot, pass scope=self or that bot name. Omit scope when no restriction was requested; the default is enabled for all local bots."
}
func (writeSkillTool) Parameters() any {
	return schema(map[string]any{
		"slug":        stringProp("Skill slug, lowercase letters, digits, and hyphens"),
		"title":       stringProp("Optional short human-readable title, e.g. 记一下 (not a long description)"),
		"scope":       stringProp("Optional: self for this bot only, or a local bot name (case-insensitive). Omitted/empty means all local bots. Only set from the current request, never guess a restriction."),
		"description": stringProp("One-line description shown in the prompt"),
		"body":        stringProp("Markdown instructions, without the YAML frontmatter"),
	}, []string{"slug", "description", "body"})
}
func (t writeSkillTool) Confirm(args json.RawMessage) (bool, string, error) {
	in, err := decodeWriteSkill(args)
	if err != nil {
		return false, "", err
	}
	if t.k == nil || t.k.Bot == nil {
		return false, "", fmt.Errorf("skill: no bot")
	}
	if t.k.SkillsDir == "" {
		return false, "", fmt.Errorf("skill: skills directory is not configured")
	}
	verb := "write"
	if _, info, err := t.k.openSkill(in.Slug); err == nil && info.Slug != "" {
		verb = "edit"
	}
	summary := verb + " skill " + in.Slug + "\n" + oneSkillLine(in.Description)
	body := strings.TrimSpace(in.Body)
	if body != "" {
		if len(body) > 800 {
			body = body[:800] + "…"
		}
		summary += "\n" + body
	}
	return false, summary, nil
}
func (t writeSkillTool) Run(_ context.Context, args json.RawMessage) (string, error) {
	in, err := decodeWriteSkill(args)
	if err != nil {
		return "", err
	}
	if t.k == nil || t.k.Bot == nil || t.k.Bot.Root == "" {
		return "", fmt.Errorf("skill: no bot")
	}
	err = t.k.withCurrentIO(func(current *bot.Bot, _ *inbox.Inbox) error {
		_, err := writeRuntimeSkill(t.k.Bot.Root, t.k.SkillsDir, current.Name(), cliops.SkillWrite{
			Slug: in.Slug, Title: in.Title, Description: in.Description, Scope: in.Scope, Body: &in.Body,
		})
		return err
	})
	if err != nil {
		return "", err
	}

	return "wrote skill " + in.Slug, nil
}

type writeSkillInput struct {
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Scope       string `json:"scope"`
	Description string `json:"description"`
	Body        string `json:"body"`
}

func decodeWriteSkill(args json.RawMessage) (writeSkillInput, error) {
	var in writeSkillInput
	if err := json.Unmarshal(args, &in); err != nil {
		return in, err
	}
	in.Scope = strings.TrimSpace(in.Scope)
	in.Slug = strings.TrimSpace(in.Slug)
	in.Description = strings.TrimSpace(in.Description)
	if !skill.ValidSlug(in.Slug) {
		return in, fmt.Errorf("skill: bad slug %q", in.Slug)
	}
	if in.Description == "" {
		return in, fmt.Errorf("skill: description is required")
	}
	return in, nil
}

// Resolve only explicit scope against the current data root's local bots.
// Invalid/ambiguous targets fail before writing the skill or any switch.
func (k *Kernel) skillScope(scope string) ([]*bot.Bot, string, error) {
	return k.skillScopeFor(scope, k.currentName())
}

func (k *Kernel) skillScopeFor(scope, currentName string) ([]*bot.Bot, string, error) {
	names, target, err := resolveSkillScope(k.Bot.Root, scope, currentName)
	if err != nil {
		return nil, "", err
	}
	var bots []*bot.Bot
	for _, name := range names {
		b, err := bot.Load(k.Bot.Root, name)
		if err != nil {
			return nil, "", err
		}
		bots = append(bots, b)
	}
	return bots, target, nil
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
	if err := k.withCurrentIO(func(current *bot.Bot, _ *inbox.Inbox) error { disabled = current.DisabledSkills(); return nil }); err != nil {
		return "", skill.Info{}, err
	}
	info, err := resolveSkillInfo(infos, id, disabled)
	return dir, info, err
}

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

func oneSkillLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

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
