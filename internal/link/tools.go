package link

import "xbot/internal/tools"

// ToolsLink is the built-in outbound link: shell and files.
type ToolsLink struct {
	list []tools.Tool
}

// NewToolsLink returns the default local tools.
func NewToolsLink() *ToolsLink {
	return &ToolsLink{list: []tools.Tool{
		tools.NewShell(),
		&tools.ReadFile{},
		&tools.WriteFile{},
		&tools.EditFile{},
	}}
}

func (t *ToolsLink) Name() string { return NameTools }

func (t *ToolsLink) Tools() []tools.Tool { return t.list }
