package code

import (
	"context"
	"encoding/json"

	"xbot/internal/tools"
)

// These three tools perform local file I/O. Resolve Root and finish the call
// within the same short Task route; shell execution has its own Start fence.
type currentFileTool struct {
	tools.Tool
	current   func(func(Task) error) error
	recordDir string
}

func (t currentFileTool) Run(ctx context.Context, args json.RawMessage) (text string, err error) {
	err = t.current(func(task Task) error {
		var tool tools.Tool
		switch t.Tool.(type) {
		case *tools.ReadFile:
			tool = &tools.ReadFile{Root: task.WorkDir}
		case *tools.WriteFile:
			tool = &tools.WriteFile{Root: task.WorkDir}
		case *tools.EditFile:
			tool = &tools.EditFile{Root: task.WorkDir}
		}
		var in map[string]json.RawMessage
		if err := json.Unmarshal(args, &in); err != nil {
			return err
		}
		var path string
		if raw, ok := in["path"]; ok {
			if err := json.Unmarshal(raw, &path); err != nil {
				return err
			}
		}
		if t.recordDir != "" && task.recordDir != "" {
			if next := rebaseCodePath(path, t.recordDir, task.recordDir); next != path {
				in["path"], _ = json.Marshal(next)
				args, _ = json.Marshal(in)
			}
		}
		var e error
		text, e = tool.Run(ctx, args)
		return e
	})
	return
}
