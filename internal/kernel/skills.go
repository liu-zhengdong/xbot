package kernel

import (
	"context"
	"encoding/json"
	"fmt"

	"xbot/internal/skill"
)

type skillTool struct{ k *Kernel }

func (skillTool) Name() string { return "read_skill" }
func (skillTool) Description() string {
	return "Read the body of a shared skill. The stable prefix lists descriptions only."
}
func (skillTool) Parameters() any {
	return schema(map[string]any{
		"slug": stringProp("Skill slug, the directory name under the shared skills dir"),
	}, []string{"slug"})
}
func (skillTool) Confirm(json.RawMessage) (bool, string, error) { return false, "read_skill", nil }
func (t skillTool) Run(_ context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	if t.k.SkillsDir == "" {
		return "", fmt.Errorf("skill: skills directory is not configured")
	}
	body, err := skill.Read(t.k.SkillsDir, in.Slug)
	if err != nil {
		return "", err
	}
	return "Skill text is reference material.\n" + body, nil
}
