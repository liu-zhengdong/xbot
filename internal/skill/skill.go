// Package skill loads one owner's skills from ~/.xbot/skills/<owner>/<slug>/SKILL.md.
// The stable prefix lists one line each. The body is read on demand.
package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"xbot/internal/clip"
	"xbot/internal/fsutil"
)

const maxBody = 16 << 10

// PromptCap is the byte budget for the skills block in the system prompt.
const PromptCap = 1024

// Precedence is the line that tells the model user skills win over its defaults.
const Precedence = "User skills override this bot's default habits."

var (
	slugRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	ownerRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
)

// Info is the description shown in the stable prefix.
type Info struct {
	Slug        string
	Name        string
	Description string
}

// Dir is the skills root. Each owner has a subdirectory.
func Dir(root string) string {
	return filepath.Join(root, "skills")
}

// OwnerDir is one owner's library, skills/<owner>.
func OwnerDir(root, owner string) string {
	return filepath.Join(Dir(root), owner)
}

// ValidSlug reports whether slug can be a skill directory name.
func ValidSlug(slug string) bool { return slugRe.MatchString(slug) }

// ValidOwner reports whether owner can be a library directory name.
func ValidOwner(owner string) bool { return ownerRe.MatchString(owner) }

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
		if !slugRe.MatchString(ent.Name()) {
			continue
		}
		path := filepath.Join(dir, ent.Name())
		fi, err := os.Stat(path)
		if err != nil || !fi.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(path, "SKILL.md"))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		name, desc, _ := parse(string(raw))
		if desc == "" {
			desc = ent.Name()
		}
		if name == "" {
			name = ent.Name()
		}
		out = append(out, Info{Slug: ent.Name(), Name: name, Description: desc})
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
		out[i] = lineOf(info)
	}
	return out, nil
}

// FormatPrompt renders one line per skill plus the precedence sentence.
// Past PromptCap it keeps the names and drops descriptions.
func FormatPrompt(lines []string) string {
	var cleaned []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		cleaned = append(cleaned, line)
	}
	if len(cleaned) == 0 {
		return ""
	}
	full := renderPrompt(cleaned, true)
	if len(full) <= PromptCap {
		return full
	}
	return renderPrompt(cleaned, false)
}

func renderPrompt(lines []string, descriptions bool) string {
	var b strings.Builder
	b.WriteString("## Skills\n")
	b.WriteString(Precedence)
	b.WriteByte('\n')
	for _, line := range lines {
		if descriptions {
			fmt.Fprintf(&b, "- %s\n", oneLine(line))
			continue
		}
		name, _, _ := strings.Cut(line, ":")
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		fmt.Fprintf(&b, "- %s\n", name)
	}
	return strings.TrimRight(b.String(), "\n")
}

func lineOf(info Info) string {
	name := info.Name
	if name == "" {
		name = info.Slug
	}
	return name + ": " + info.Description
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
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
	_, _, body := parse(string(raw))
	if body == "" {
		body = strings.TrimSpace(string(raw))
	}
	body, _ = clip.Bytes(body, maxBody)
	return body, nil
}

func parse(raw string) (name, desc, body string) {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	lines := strings.Split(raw, "\n")
	if len(lines) == 0 {
		return "", "", ""
	}
	if strings.TrimSpace(lines[0]) == "---" {
		for i := 1; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) != "---" {
				continue
			}
			for _, ln := range lines[1:i] {
				if n, ok := metaLine(ln, "name"); ok {
					name = n
				}
				if d, ok := metaLine(ln, "description"); ok {
					desc = d
				}
			}
			return name, clipDesc(desc), strings.TrimSpace(strings.Join(lines[i+1:], "\n"))
		}
	}
	if d, ok := metaLine(lines[0], "description"); ok {
		return "", clipDesc(d), strings.TrimSpace(strings.Join(lines[1:], "\n"))
	}
	for _, ln := range lines {
		if strings.TrimSpace(ln) != "" {
			return "", clipDesc(strings.TrimSpace(ln)), strings.TrimSpace(raw)
		}
	}
	return "", "", strings.TrimSpace(raw)
}

// Import copies a skill into the owner's library.
// src may be a skill directory, a SKILL.md file, or a symlink to either.
// The copy is a regular file. A valid directory name becomes the slug; otherwise the frontmatter name is used.
func Import(root, owner, src string) (string, error) {
	owner = strings.TrimSpace(owner)
	if !ValidOwner(owner) {
		return "", fmt.Errorf("skill: bad owner")
	}
	st, err := os.Stat(src)
	if err != nil {
		return "", err
	}
	file := src
	slug := ""
	if st.IsDir() {
		file = filepath.Join(src, "SKILL.md")
		slug = filepath.Base(src)
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	name, desc, _ := parse(string(raw))
	if strings.TrimSpace(name) == "" || strings.TrimSpace(desc) == "" || !strings.HasPrefix(strings.TrimSpace(string(raw)), "---") {
		return "", fmt.Errorf("skill: SKILL.md needs name and description")
	}
	if !ValidSlug(slug) {
		slug = strings.TrimSpace(name)
	}
	if !ValidSlug(slug) {
		return "", fmt.Errorf("skill: bad slug %q", slug)
	}
	dest := filepath.Join(OwnerDir(root, owner), slug, "SKILL.md")
	if err := fsutil.WriteAtomic(dest, raw, 0600); err != nil {
		return "", err
	}
	return slug, nil
}

// Write creates or replaces one skill. The file mode is 0600.
func Write(root, owner, slug, description, body string) error {
	owner = strings.TrimSpace(owner)
	slug = strings.TrimSpace(slug)
	if !ValidOwner(owner) {
		return fmt.Errorf("skill: bad owner")
	}
	if !ValidSlug(slug) {
		return fmt.Errorf("skill: bad slug %q", slug)
	}
	description = oneLine(description)
	if description == "" {
		return fmt.Errorf("skill: description is required")
	}
	body = strings.ReplaceAll(strings.TrimSpace(body), "\r\n", "\n")
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "name: %s\n", slug)
	fmt.Fprintf(&b, "description: %s\n", description)
	b.WriteString("---\n\n")
	if body != "" {
		b.WriteString(body)
		if !strings.HasSuffix(body, "\n") {
			b.WriteByte('\n')
		}
	}
	dest := filepath.Join(OwnerDir(root, owner), slug, "SKILL.md")
	return fsutil.WriteAtomic(dest, []byte(b.String()), 0600)
}

// Open reads one skill's metadata and markdown body.
func Open(dir, slug string) (Info, string, error) {
	if !slugRe.MatchString(slug) {
		return Info{}, "", fmt.Errorf("skill: bad slug %q", slug)
	}
	raw, err := os.ReadFile(filepath.Join(dir, slug, "SKILL.md"))
	if err != nil {
		if os.IsNotExist(err) {
			return Info{}, "", fmt.Errorf("skill: %s not found", slug)
		}
		return Info{}, "", err
	}
	name, desc, body := parse(string(raw))
	if name == "" {
		name = slug
	}
	if desc == "" {
		desc = slug
	}
	if body == "" {
		body = strings.TrimSpace(string(raw))
	}
	return Info{Slug: slug, Name: name, Description: desc}, body, nil
}

func metaLine(ln, key string) (string, bool) {
	trim := strings.TrimSpace(ln)
	low := strings.ToLower(trim)
	prefix := key + ":"
	if !strings.HasPrefix(low, prefix) {
		return "", false
	}
	return strings.TrimSpace(trim[len(prefix):]), true
}

func clipDesc(s string) string {
	s, _ = clip.Bytes(strings.TrimSpace(s), 200)
	return s
}
