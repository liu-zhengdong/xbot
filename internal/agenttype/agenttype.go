// Package agenttype defines the kinds of subagent the foreground can dispatch
// (ADR 0016). A type is a tool allowlist, a prompt fragment, a wall-clock
// budget, and the sections the result must have. Three are built in; a bot can
// override one or add its own with a file under agents/types/.
package agenttype

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Tool names a type can allow. They are the subagent tool set's names, plus
// ShellReadOnly: the shell tool limited to plain read commands.
const (
	ShellReadOnly = "shell:readonly"
	// All allows every tool in the subagent set. Dispatching is never in it.
	All = "*"
)

// Type is one kind of subagent.
type Type struct {
	Name string
	// When is one line for the foreground: what this type is for.
	When string
	// Tools is the allowlist. Empty is an error.
	Tools []string
	// Prompt is added after the subagent's base instructions.
	Prompt string
	// Budget is how long one task may run.
	Budget time.Duration
	// Report lists the sections the final reply must have.
	Report []string
	// Builtin is true for a type that ships with XBot and no file overrides.
	Builtin bool
}

// Allows reports whether the type may use tool.
func (t Type) Allows(tool string) bool {
	for _, n := range t.Tools {
		if n == All || n == tool {
			return true
		}
	}
	return false
}

// Builtin returns the shipped types: research, dev, and general.
func Builtin() []Type {
	return []Type{
		{
			Name:    "research",
			When:    "Find out something: search the web, read pages and files, compare sources. It cannot change files.",
			Tools:   []string{"read_file", ShellReadOnly, "web_fetch", "web_search", "view_image"},
			Prompt:  "You are a researcher. Check more than one source when you can. Say where each fact comes from. Keep facts and your own inference apart. Do not change any file.",
			Budget:  20 * time.Minute,
			Report:  []string{"Findings", "Sources", "Not found"},
			Builtin: true,
		},
		{
			Name:    "dev",
			When:    "Change code or files: read, edit, run commands and tests, use git.",
			Tools:   []string{"read_file", "write_file", "edit_file", "shell", "web_fetch", "web_search", "view_image", "read_skill", "write_skill"},
			Prompt:  "You are a developer. Read before you edit. Run the tests after a change. Stay inside the task: do not refactor or fix things you were not asked to.",
			Budget:  2 * time.Hour,
			Report:  []string{"Changed", "Tests", "Not done"},
			Builtin: true,
		},
		{
			Name:    "general",
			When:    "Anything else that takes more than a few steps.",
			Tools:   []string{All},
			Budget:  45 * time.Minute,
			Report:  []string{"Done", "Result", "Not done"},
			Builtin: true,
		},
	}
}

// Load returns the built-in types overlaid by the files in dir: dir/<name>.md
// replaces the built-in type of that name or adds a new one. A missing dir is
// not an error. A file that does not parse is reported and the rest load.
func Load(dir string) ([]Type, []error) {
	byName := map[string]Type{}
	for _, t := range Builtin() {
		byName[t.Name] = t
	}
	var errs []error
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		errs = append(errs, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".md")
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		t, err := Parse(name, data)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		byName[name] = t
	}
	out := make([]Type, 0, len(byName))
	for _, t := range byName {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, errs
}

// Find returns the type named name.
func Find(types []Type, name string) (Type, bool) {
	for _, t := range types {
		if t.Name == name {
			return t, true
		}
	}
	return Type{}, false
}

// Parse reads one type file:
//
//	---
//	when: one line for the foreground
//	tools: read_file, shell:readonly, web_search
//	budget: 20m
//	report: Findings, Sources, Not found
//	---
//	The prompt fragment.
func Parse(name string, data []byte) (Type, error) {
	if !validName(name) {
		return Type{}, fmt.Errorf("agent type %q: name must be lowercase letters, digits, - or _", name)
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return Type{}, fmt.Errorf("agent type %s: missing --- header", name)
	}
	head, body, ok := strings.Cut(text[4:], "\n---")
	if !ok {
		return Type{}, fmt.Errorf("agent type %s: header is not closed with ---", name)
	}
	t := Type{Name: name, Prompt: strings.TrimSpace(strings.TrimPrefix(body, "\n"))}
	for _, line := range strings.Split(head, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			return Type{}, fmt.Errorf("agent type %s: bad header line %q", name, line)
		}
		key, val = strings.TrimSpace(key), strings.TrimSpace(val)
		switch key {
		case "when":
			t.When = val
		case "tools":
			t.Tools = splitList(val)
		case "budget":
			d, err := time.ParseDuration(val)
			if err != nil || d <= 0 {
				return Type{}, fmt.Errorf("agent type %s: bad budget %q", name, val)
			}
			t.Budget = d
		case "report":
			t.Report = splitList(val)
		default:
			return Type{}, fmt.Errorf("agent type %s: unknown key %q", name, key)
		}
	}
	if strings.TrimSpace(t.When) == "" {
		return Type{}, fmt.Errorf("agent type %s: when is empty", name)
	}
	if len(t.Tools) == 0 {
		return Type{}, fmt.Errorf("agent type %s: tools is empty", name)
	}
	if t.Budget == 0 {
		t.Budget = 45 * time.Minute
	}
	return t, nil
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func validName(s string) bool {
	if s == "" || len(s) > 32 {
		return false
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}

// Instructions is the system text for a subagent of type t: the base rules,
// the type's prompt, and the sections its final reply must have.
func (t Type) Instructions(base string) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(base))
	if p := strings.TrimSpace(t.Prompt); p != "" {
		b.WriteString("\n\n")
		b.WriteString(p)
	}
	if len(t.Report) > 0 {
		b.WriteString("\n\nWhen the task is finished, the final reply has these sections, each a short heading followed by its content: ")
		b.WriteString(strings.Join(t.Report, ", "))
		b.WriteString(". Write \"none\" under a section that has nothing.")
	}
	return b.String()
}
