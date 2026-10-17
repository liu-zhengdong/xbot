package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"xbot/internal/tools"
)

// fileAccess is shared by execution and grading. /sandbox always means work;
// memory paths remain absolute, outside that alias but inside these two folders.
type fileAccess struct {
	work   string
	memory bool
}

func (a fileAccess) logical(path string) string {
	path = tools.AliasCase(a.work, path)
	if !filepath.IsAbs(path) {
		path = filepath.Join(a.work, path)
	}
	return filepath.Clean(path)
}

func (a fileAccess) resolve(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("file path is empty")
	}
	resolved, err := tools.ResolveRoot(a.work, a.logical(path))
	if err != nil {
		return "", err
	}
	roots := []string{a.work}
	if a.memory {
		root := filepath.Dir(a.work)
		roots = append(roots, filepath.Join(root, "bots", "ada", "memory"), filepath.Join(root, "people", "owner", "memory"))
	}
	for _, root := range roots {
		canonical, err := tools.ResolveRoot("", root)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(canonical, resolved)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return resolved, nil
		}
	}
	return "", fmt.Errorf("path is outside the workspace and memory folders")
}

// fileTool retains the kernel's path interfaces so freshness and memory guards
// surround the real file tools. The logical path retains symlinks for the guard.
type fileTool struct {
	tools.Tool
	access fileAccess
}

func (t fileTool) checkedArgs(raw json.RawMessage) (json.RawMessage, error) {
	var in map[string]json.RawMessage
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	var path string
	if err := json.Unmarshal(in["path"], &path); err != nil {
		return nil, err
	}
	if _, err := t.access.resolve(path); err != nil {
		return nil, err
	}
	in["path"], _ = json.Marshal(t.access.logical(path))
	return json.Marshal(in)
}

func (t fileTool) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	checked, err := t.checkedArgs(raw)
	if err != nil {
		return "", err
	}
	out, err := t.Tool.Run(ctx, checked)
	return tools.RewriteHost(t.access.work, "", out), err
}

func (t fileTool) Confirm(raw json.RawMessage) (bool, string, error) {
	checked, err := t.checkedArgs(raw)
	if err != nil {
		return false, "", err
	}
	need, summary, err := t.Tool.Confirm(checked)
	return need, tools.RewriteHost(t.access.work, "", summary), err
}

func (t fileTool) ResolvedPath(raw json.RawMessage) string {
	checked, err := t.checkedArgs(raw)
	if err != nil {
		return ""
	}
	return t.Tool.(tools.PathTool).ResolvedPath(checked)
}

func (t fileTool) LogicalPathContext(_ context.Context, raw json.RawMessage) string {
	checked, err := t.checkedArgs(raw)
	if err != nil {
		return ""
	}
	return jsonString(string(checked), "path")
}

func (t fileTool) ShownPath(path string) string { return tools.DisplayPath(t.access.work, path) }

func memoryCaseTools(work string, network bool) []tools.Tool {
	access := fileAccess{work: work, memory: true}
	return []tools.Tool{
		newEvalShell(work, network),
		fileTool{Tool: &tools.ReadFile{Root: work}, access: access},
		fileTool{Tool: &tools.WriteFile{Root: work}, access: access},
		fileTool{Tool: &tools.EditFile{Root: work}, access: access},
	}
}
