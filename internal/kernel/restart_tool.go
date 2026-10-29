package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"xbot/internal/tools"
)

type restartTool struct{ k *Kernel }

func (restartTool) Name() string { return "restart_xbot" }
func (restartTool) Description() string {
	return "Gracefully restart XBot after saving a short hint for all Bots currently working, so they can continue after startup. Use when the user wants a restart; this action always asks the user to confirm."
}
func (restartTool) Parameters() any {
	return schema(map[string]any{"hint": map[string]any{"type": "string", "description": "Optional continuation hint for all working Bots"}}, nil)
}
func (restartTool) Confirm(args json.RawMessage) (bool, string, error) {
	var in struct {
		Hint string `json:"hint"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return false, "", err
	}
	return false, "restart XBot", nil
}
func (t restartTool) Run(_ context.Context, args json.RawMessage) (string, error) {
	if t.k == nil || t.k.Restart == nil {
		return "", errors.New("restart is unavailable in this session")
	}
	var in struct {
		Hint string `json:"hint"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	hint := strings.TrimSpace(in.Hint)
	if len(hint) > 4000 {
		return "", errors.New("restart hint exceeds 4000 bytes")
	}
	if err := t.k.Restart(hint); err != nil {
		return "", err
	}
	return "已保存续跑提示，正在优雅重启 XBot。", nil
}

var _ tools.Tool = restartTool{}
