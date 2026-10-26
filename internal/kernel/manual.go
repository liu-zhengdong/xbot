package kernel

import (
	"context"
	"encoding/json"

	"xbot/internal/manual"
)

type kernelManualTool struct{}

func (kernelManualTool) Name() string { return "kernel_manual" }
func (kernelManualTool) Description() string {
	return "Read a versioned XBot mechanism manual section before explaining how you work or where the owner can inspect it. Read-only; does not inspect live state."
}
func (kernelManualTool) Parameters() any {
	ids := []string{"index"}
	for _, s := range manual.Sections() {
		ids = append(ids, s.ID)
	}
	return schema(map[string]any{"section": map[string]any{"type": "string", "enum": ids, "description": "manual section identifier; index lists the directory"}}, []string{"section"})
}
func (kernelManualTool) Confirm(json.RawMessage) (bool, string, error) { return false, "", nil }
func (kernelManualTool) Run(_ context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Section string `json:"section"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	return manual.Read(in.Section)
}
