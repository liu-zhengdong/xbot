// Package skill loads shared skills from ~/.xbot/skills/<slug>/SKILL.md.
// The stable prefix lists descriptions only. The body is read on demand.
package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"xbot/internal/clip"
)

const maxBody = 16 << 10

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// Info is the description shown in the stable prefix.
type Info struct {
	Slug        string
	Description string
}

// Dir is the shared skills directory.
func Dir(root string) string {
	return filepath.Join(root, "skills")
}

// List returns descriptions sorted by slug. dir is the skills directory.
// A missing directory is empty.
func List(dir string) ([]Info, error) {
	entries, err := os.ReadDir(dir)
	if dir == "" || os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Info
	for _, ent := range entries {
		if !ent.IsDir() || !slugRe.MatchString(ent.Name()) {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, ent.Name(), "SKILL.md"))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		desc, _ := parse(string(raw))
		if desc == "" {
			desc = ent.Name()
		}
		out = append(out, Info{Slug: ent.Name(), Description: desc})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out, nil
}

// Lines is the stable-prefix form, "slug: description", sorted.
// dir is the skills directory.
func Lines(dir string) ([]string, error) {
	infos, err := List(dir)
	if err != nil || len(infos) == 0 {
		return nil, err
	}
	out := make([]string, len(infos))
	for i, info := range infos {
		out[i] = info.Slug + ": " + info.Description
	}
	return out, nil
}

// Read returns the skill body. dir is the skills directory.
// The description line is not repeated when it was split out; the markdown below it is the body.
func Read(dir, slug string) (string, error) {
	if !slugRe.MatchString(slug) {
		return "", fmt.Errorf("skill: bad slug %q", slug)
	}
	raw, err := os.ReadFile(filepath.Join(dir, slug, "SKILL.md"))
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("skill: %s not found", slug)
		}
		return "", err
	}
	_, body := parse(string(raw))
	if body == "" {
		body = strings.TrimSpace(string(raw))
	}
	body, _ = clip.Bytes(body, maxBody)
	return body, nil
}

func parse(raw string) (desc, body string) {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	lines := strings.Split(raw, "\n")
	if len(lines) == 0 {
		return "", ""
	}
	if strings.TrimSpace(lines[0]) == "---" {
		for i := 1; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) != "---" {
				continue
			}
			for _, ln := range lines[1:i] {
				if d, ok := descLine(ln); ok {
					desc = d
				}
			}
			return clipDesc(desc), strings.TrimSpace(strings.Join(lines[i+1:], "\n"))
		}
	}
	if d, ok := descLine(lines[0]); ok {
		return clipDesc(d), strings.TrimSpace(strings.Join(lines[1:], "\n"))
	}
	for _, ln := range lines {
		if strings.TrimSpace(ln) != "" {
			return clipDesc(strings.TrimSpace(ln)), strings.TrimSpace(raw)
		}
	}
	return "", strings.TrimSpace(raw)
}

func descLine(ln string) (string, bool) {
	trim := strings.TrimSpace(ln)
	low := strings.ToLower(trim)
	if !strings.HasPrefix(low, "description:") {
		return "", false
	}
	return strings.TrimSpace(trim[len("description:"):]), true
}

func clipDesc(s string) string {
	s, _ = clip.Bytes(strings.TrimSpace(s), 200)
	return s
}
