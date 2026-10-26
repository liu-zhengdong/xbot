// Package manual is the versioned mechanism reference shared by bots and owners.
package manual

import (
	"embed"
	"fmt"
	"strings"
)

//go:embed sections/*.md
var contents embed.FS

// Section is a public, stable section identifier and its display title.
type Section struct{ ID, Title string }

var sections = []Section{
	{"memory", "记忆"},
	{"context", "上下文与压缩"},
	{"routines", "例程与唤醒"},
	{"subagents", "子代理"},
	{"interjections", "插话与排队"},
	{"connectors", "连接器"},
	{"workspace", "文件与工作区"},
}

// Sections returns a copy so callers cannot change the shared directory.
func Sections() []Section { return append([]Section(nil), sections...) }

// Index lists only the directory, never section bodies.
func Index() string {
	var out strings.Builder
	out.WriteString("XBot 机制手册（随二进制更新）\n")
	for _, s := range sections {
		fmt.Fprintf(&out, "- %s: %s\n", s.ID, s.Title)
	}
	out.WriteString("读取：kernel_manual(section)；主人在终端运行 xbot manual <section>。\n")
	return out.String()
}

// Prompt is deliberately just a reading instruction and the small directory.
func Prompt() string {
	return "When asked how you work or where to inspect it, first read the relevant kernel_manual section. The manual describes mechanisms, not live state; check current capabilities and paths.\n" + strings.TrimSpace(Index())
}

// Read accepts only known identifiers, not caller-supplied filesystem paths.
func Read(section string) (string, error) {
	if section == "index" {
		return Index(), nil
	}
	for _, s := range sections {
		if section == s.ID {
			b, err := contents.ReadFile("sections/" + s.ID + ".md")
			return string(b), err
		}
	}
	return "", fmt.Errorf("manual: unknown section %q; use xbot manual to list sections", section)
}
