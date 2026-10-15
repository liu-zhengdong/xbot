package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"xbot/internal/provider"
	"xbot/internal/skill"
)

type skillTool struct{ k *Kernel }

func (skillTool) Name() string { return "read_skill" }
func (skillTool) Description() string {
	return "Read the body of a shared skill. Pass id, the skill id from the Skills list. The text is for this turn only."
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
	dir, info, err := t.k.openSkill(id)
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
func (writeSkillTool) Description() string {
	return "Draft a new user skill or replace one. The owner must approve before it is written."
}
func (writeSkillTool) Parameters() any {
	return schema(map[string]any{
		"slug":        stringProp("Skill slug, lowercase letters, digits, and hyphens"),
		"description": stringProp("One-line description shown in the prompt"),
		"body":        stringProp("Markdown instructions, without the YAML frontmatter"),
	}, []string{"slug", "description", "body"})
}
func (t writeSkillTool) Confirm(args json.RawMessage) (bool, string, error) {
	in, err := decodeWriteSkill(args)
	if err != nil {
		return false, "", err
	}
	if t.k == nil {
		return false, "", fmt.Errorf("skill: no bot")
	}
	if _, err := t.k.skillOwner(); err != nil {
		return false, "", err
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
	return true, summary, nil
}
func (t writeSkillTool) Run(_ context.Context, args json.RawMessage) (string, error) {
	in, err := decodeWriteSkill(args)
	if err != nil {
		return "", err
	}
	owner, err := t.k.skillOwner()
	if err != nil {
		return "", err
	}
	if t.k.Bot == nil || t.k.Bot.Root == "" {
		return "", fmt.Errorf("skill: no bot")
	}
	if err := skill.Write(t.k.Bot.Root, owner, in.Slug, in.Description, in.Body); err != nil {
		return "", err
	}
	return "wrote skill " + in.Slug, nil
}

func decodeWriteSkill(args json.RawMessage) (struct {
	Slug        string `json:"slug"`
	Description string `json:"description"`
	Body        string `json:"body"`
}, error) {
	var in struct {
		Slug        string `json:"slug"`
		Description string `json:"description"`
		Body        string `json:"body"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return in, err
	}
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

func (k *Kernel) skillOwner() (string, error) {
	owner, ok := k.soleOwner()
	if !ok {
		return "", fmt.Errorf("skill: no owner library")
	}
	return owner, nil
}

func (k *Kernel) openSkill(id string) (string, skill.Info, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", skill.Info{}, fmt.Errorf("skill: id is required")
	}
	owner, err := k.skillOwner()
	if err != nil {
		return "", skill.Info{}, fmt.Errorf("skill: %s not found", id)
	}
	if k.SkillsDir == "" {
		return "", skill.Info{}, fmt.Errorf("skill: skills directory is not configured")
	}
	dir := filepath.Join(k.SkillsDir, owner)
	infos, err := skill.List(dir)
	if err != nil {
		return "", skill.Info{}, err
	}
	var found *skill.Info
	for i := range infos {
		if infos[i].Slug == id || infos[i].Name == id {
			found = &infos[i]
			break
		}
	}
	if found == nil {
		return "", skill.Info{}, fmt.Errorf("skill: %s not found", id)
	}
	for _, slug := range k.Bot.Config().DisabledSkills {
		if strings.TrimSpace(slug) == found.Slug {
			return "", skill.Info{}, fmt.Errorf("skill: disabled")
		}
	}
	return dir, *found, nil
}

func redactSkillRead(m provider.Message) provider.Message {
	if m.Role != provider.RoleTool || m.Name != "read_skill" {
		return m
	}
	slug := ""
	if rest, ok := strings.CutPrefix(m.Content, "skill: "); ok {
		slug, _, _ = strings.Cut(rest, "\n")
		slug = strings.TrimSpace(slug)
	}
	if slug == "" {
		slug = "skill"
	}
	m.Content = "read skill " + slug
	return m
}

func oneSkillLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
