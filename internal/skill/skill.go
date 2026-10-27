// Package skill loads local skills from ~/.xbot/skills/<slug>/SKILL.md.
// The stable prefix lists one line each. The body is read on demand.
package skill

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"xbot/internal/clip"
	"xbot/internal/fsutil"
)

const maxBody = 16 << 10

// PromptCap is the byte budget for the skills block in the system prompt.
const PromptCap = 1024

// Precedence is the line that tells the model user skills win over its defaults.
const Precedence = "User skills override this bot's default habits."

// ReadWith tells the model which tool reads a skill and which id to pass.
const ReadWith = "Call read_skill with id set to the skill id on each line."

var (
	slugRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}(?:__from_[A-Za-z0-9][A-Za-z0-9_-]{0,63})*(?:__[0-9]+)?$`)
	ownerRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
)

// Info is the description shown in the stable prefix.
type Info struct {
	Slug        string
	Name        string
	Title       string
	Description string
}

// Dir is the local library shared by bots in this data root.
func Dir(root string) string {
	return filepath.Join(root, "skills")
}

// ValidSlug reports whether slug can be a skill directory name.
func ValidSlug(slug string) bool { return len(slug) <= 240 && slugRe.MatchString(slug) }

// ValidOwner reports whether owner can be a legacy library directory name.
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
		if !ValidSlug(ent.Name()) {
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
		name, title, desc, _ := parse(string(raw))
		if desc == "" {
			desc = ent.Name()
		}
		if name == "" {
			name = ent.Name()
		}
		out = append(out, Info{Slug: ent.Name(), Name: name, Title: title, Description: desc})
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
// dir is the skills directory as given at runtime; empty omits the Directory line.
// Past PromptCap it keeps the names and drops descriptions.
func FormatPrompt(dir string, lines []string) string {
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
	full := renderPrompt(dir, cleaned, true)
	if len(full) <= PromptCap {
		return full
	}
	return renderPrompt(dir, cleaned, false)
}

func renderPrompt(dir string, lines []string, descriptions bool) string {
	var b strings.Builder
	b.WriteString("## Skills\n")
	if dir != "" {
		fmt.Fprintf(&b, "Directory: %s\n", dir)
	}
	b.WriteString(Precedence)
	b.WriteByte('\n')
	b.WriteString(ReadWith)
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

// Line is one prompt row. The token before the colon is the skill id.
func Line(info Info) string { return lineOf(info) }

func lineOf(info Info) string {
	id := info.Slug
	if id == "" {
		id = info.Name
	}
	if info.Name != "" && info.Name != id {
		return id + " (" + info.Name + "): " + info.Description
	}
	return id + ": " + info.Description
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// Read returns the skill body. dir is the skills directory.
// The description line is not repeated when it was split out; the markdown below it is the body.
func Read(dir, slug string) (string, error) {
	if !ValidSlug(slug) {
		return "", fmt.Errorf("skill: bad slug %q", slug)
	}
	raw, err := os.ReadFile(filepath.Join(dir, slug, "SKILL.md"))
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("skill: %s not found", slug)
		}
		return "", err
	}
	_, _, _, body := parse(string(raw))
	if body == "" {
		body = strings.TrimSpace(string(raw))
	}
	body, _ = clip.Bytes(body, maxBody)
	return body, nil
}

func parse(raw string) (name, title, desc, body string) {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	lines := strings.Split(raw, "\n")
	if len(lines) == 0 {
		return "", "", "", ""
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
				if t, ok := metaLine(ln, "title"); ok {
					title = t
				}
				if d, ok := metaLine(ln, "description"); ok {
					desc = d
				}
			}
			return name, title, clipDesc(desc), strings.TrimSpace(strings.Join(lines[i+1:], "\n"))
		}
	}
	if d, ok := metaLine(lines[0], "description"); ok {
		return "", "", clipDesc(d), strings.TrimSpace(strings.Join(lines[1:], "\n"))
	}
	for _, ln := range lines {
		if strings.TrimSpace(ln) != "" {
			return "", "", clipDesc(strings.TrimSpace(ln)), strings.TrimSpace(raw)
		}
	}
	return "", "", "", strings.TrimSpace(raw)
}

// Import copies a skill into the local library.
// src may be a skill directory, a SKILL.md file, or a symlink to either.
// The copy is a regular file. A valid directory name becomes the slug; otherwise the frontmatter name is used.
func Import(root, src string) (string, error) {
	st, err := os.Stat(src)
	if err != nil {
		return "", err
	}
	file := src
	dir := ""
	if st.IsDir() {
		file = filepath.Join(src, "SKILL.md")
		dir = filepath.Base(src)
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	res, err := ImportData(root, dir, raw, true)
	return res.Slug, err
}

// ErrExists means the library already holds a skill with that slug and the import did not ask to overwrite.
var ErrExists = errors.New("skill: already exists")

// ErrNoMeta means SKILL.md has no name or description.
var ErrNoMeta = errors.New("skill: SKILL.md needs name and description")

// ErrBadSlug means neither the folder name nor the frontmatter name can be a skill directory name.
var ErrBadSlug = errors.New("skill: bad slug")

// Imported is what an import produced. Name is the human name, for messages.
type Imported struct {
	Slug string
	Name string
}

// ImportData is Import for a SKILL.md already in memory, with dir the name of the folder it came from.
// Without overwrite an existing skill is left alone and ErrExists comes back with its name filled in.
func ImportData(root, dir string, raw []byte, overwrite bool) (Imported, error) {
	name, _, desc, _ := parse(string(raw))
	if strings.TrimSpace(name) == "" || strings.TrimSpace(desc) == "" || !strings.HasPrefix(strings.TrimSpace(string(raw)), "---") {
		return Imported{}, ErrNoMeta
	}
	slug := dir
	if !ValidSlug(slug) {
		slug = strings.TrimSpace(name)
	}
	if !ValidSlug(slug) {
		return Imported{}, fmt.Errorf("%w %q", ErrBadSlug, slug)
	}
	res := Imported{Slug: slug, Name: strings.TrimSpace(name)}
	dest := filepath.Join(Dir(root), slug, "SKILL.md")
	lock, err := fsutil.Lock(filepath.Join(Dir(root), ".write.lock"))
	if err != nil {
		return Imported{}, err
	}
	defer lock.Release()
	if !overwrite {
		if _, err := os.Stat(dest); err == nil {
			return res, ErrExists
		} else if !os.IsNotExist(err) {
			return Imported{}, err
		}
	}
	if err := fsutil.WriteAtomic(dest, raw, 0600); err != nil {
		return Imported{}, err
	}
	return res, nil
}

// ErrNotFound means the library has no skill with that slug.
var ErrNotFound = errors.New("skill: not found")

// Remove deletes one skill's folder from the library.
// before runs under the library write lock once the skill is known to exist,
// so clearing what refers to it cannot interleave with an import of the same slug.
func Remove(root, slug string, before func() error) error {
	if !ValidSlug(slug) {
		return fmt.Errorf("%w %q", ErrBadSlug, slug)
	}
	lock, err := fsutil.Lock(filepath.Join(Dir(root), ".write.lock"))
	if err != nil {
		return err
	}
	defer lock.Release()
	dir := filepath.Join(Dir(root), slug)
	if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%w: %s", ErrNotFound, slug)
		}
		return err
	}
	if before != nil {
		if err := before(); err != nil {
			return err
		}
	}
	return os.RemoveAll(dir)
}

// Write creates or replaces one skill. The file mode is 0600.
// title optionally supplies a short human-readable label; name stays the slug.
func Write(root, slug, description, body string, title ...string) error {
	slug = strings.TrimSpace(slug)
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
	if len(title) > 0 && oneLine(title[0]) != "" {
		fmt.Fprintf(&b, "title: %s\n", strconv.Quote(oneLine(title[0])))
	}
	fmt.Fprintf(&b, "description: %s\n", description)
	b.WriteString("---\n\n")
	if body != "" {
		b.WriteString(body)
		if !strings.HasSuffix(body, "\n") {
			b.WriteByte('\n')
		}
	}
	dest := filepath.Join(Dir(root), slug, "SKILL.md")
	return fsutil.WriteAtomic(dest, []byte(b.String()), 0600)
}

// Open reads one skill's metadata and markdown body.
func Open(dir, slug string) (Info, string, error) {
	if !ValidSlug(slug) {
		return Info{}, "", fmt.Errorf("skill: bad slug %q", slug)
	}
	raw, err := os.ReadFile(filepath.Join(dir, slug, "SKILL.md"))
	if err != nil {
		if os.IsNotExist(err) {
			return Info{}, "", fmt.Errorf("skill: %s not found", slug)
		}
		return Info{}, "", err
	}
	name, title, desc, body := parse(string(raw))
	if name == "" {
		name = slug
	}
	if desc == "" {
		desc = slug
	}
	if body == "" {
		body = strings.TrimSpace(string(raw))
	}
	return Info{Slug: slug, Name: name, Title: title, Description: desc}, body, nil
}

func metaLine(ln, key string) (string, bool) {
	trim := strings.TrimSpace(ln)
	low := strings.ToLower(trim)
	prefix := key + ":"
	if !strings.HasPrefix(low, prefix) {
		return "", false
	}
	value := strings.TrimSpace(trim[len(prefix):])
	if decoded, err := strconv.Unquote(value); err == nil {
		value = decoded
	} else if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
		value = strings.ReplaceAll(value[1:len(value)-1], "''", "'")
	}
	return value, true
}

func clipDesc(s string) string {
	s, _ = clip.Bytes(strings.TrimSpace(s), 200)
	return s
}
