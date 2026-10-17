package link

import "xbot/internal/tools"

// ToolsLink is the built-in outbound link: shell and files.
type ToolsLink struct {
	list []tools.Tool
}

// NewToolsLink returns the default local tools, relative to the process working directory.
func NewToolsLink() *ToolsLink {
	return NewToolsLinkAt("", "")
}

// NewToolsLinkAt aims shell and relative file paths at dir.
// shared is exported to the shell as XBOT_SHARED when set.
func NewToolsLinkAt(dir, shared string) *ToolsLink {
	sh := tools.NewShell()
	sh.Dir = dir
	sh.Shared = shared
	return &ToolsLink{list: []tools.Tool{
		sh,
		&tools.ReadFile{Root: dir},
		&tools.WriteFile{Root: dir},
		&tools.EditFile{Root: dir},
		&tools.WebFetch{},
		&tools.WebSearch{},
		&tools.ViewImage{Root: dir},
	}}
}

func (t *ToolsLink) Name() string { return NameTools }

func (t *ToolsLink) Tools() []tools.Tool { return t.list }
