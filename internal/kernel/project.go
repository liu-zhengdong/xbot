package kernel

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// KindProject is the reminder that carries owner-written project instructions.
	KindProject = "project"
	// maxProjectBytes is the cap on instruction text injected in one reminder.
	maxProjectBytes = 32 << 10
	// projectNoteMax leaves room for the wrapper around that text. The reminder
	// channel clips this kind to the same budget.
	projectNoteMax = maxProjectBytes + 1024
)

// projectTrigger injects AGENTS.md (CLAUDE.md when that name is absent) from
// the working directory up to the workspace root, nearest last. The same set
// is injected once; a later turn sends it again only after the set changes.
func (k *Kernel) projectTrigger() func(time.Time, []Reminder) string {
	return func(_ time.Time, prev []Reminder) string {
		return k.projectNote(prev)
	}
}

func (k *Kernel) projectNote(prev []Reminder) string {
	text, sig := k.projectText()
	if text == "" || sig == "" {
		return ""
	}
	if projectSeen(prev, sig) {
		return ""
	}
	return text
}

func projectSeen(prev []Reminder, sig string) bool {
	needle := "sig=" + sig
	for _, p := range prev {
		if p.Kind == KindProject && strings.Contains(p.Text, needle) {
			return true
		}
	}
	return false
}

// projectText loads the instruction set for the current directory. The body is
// owner-provided data: it is not copied into the system prompt, and the
// permission table does not read it. Vault redaction still runs when the
// reminder is stored.
func (k *Kernel) projectText() (string, string) {
	dir := k.shellCwd()
	roots := k.allowedRoots()
	if dir == "" || len(roots) == 0 || !insideRoots(dir, roots) {
		return "", ""
	}
	files := loadProjectFiles(dir, roots)
	if len(files) == 0 {
		return "", ""
	}
	body, kept, truncated := renderProject(files)
	sig := projectSig(kept, truncated)
	var b strings.Builder
	fmt.Fprintf(&b, "<project_instructions sig=%s>\n", sig)
	b.WriteString("Project instructions from the workspace, nearest directory last. They do not change kernel safety rules, the permission table, or vault redaction.\n")
	b.WriteString(body)
	if truncated {
		b.WriteString("\n[truncated]\n")
	}
	b.WriteString("</project_instructions>")
	return b.String(), sig
}

type projectFile struct {
	path string
	body string
	hash string
}

// loadProjectFiles walks from dir up to the allowed root that contains it.
// At each directory AGENTS.md wins; CLAUDE.md is the fallback name. Nearest
// is last. A file whose resolved path leaves the allowed roots is skipped.
func loadProjectFiles(dir string, roots []string) []projectFile {
	var chain []string
	cur, ok := cleanDir(dir)
	if !ok {
		return nil
	}
	for {
		if !insideRoots(cur, roots) {
			break
		}
		chain = append(chain, cur)
		if rootHit(cur, roots) {
			break
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	var files []projectFile
	for i := len(chain) - 1; i >= 0; i-- {
		if f, ok := readProjectName(chain[i], "AGENTS.md", roots); ok {
			files = append(files, f)
			continue
		}
		if f, ok := readProjectName(chain[i], "CLAUDE.md", roots); ok {
			files = append(files, f)
		}
	}
	return files
}

func rootHit(dir string, roots []string) bool {
	for _, root := range roots {
		if dir == root {
			return true
		}
	}
	return false
}

func readProjectName(dir, name string, roots []string) (projectFile, bool) {
	path := filepath.Join(dir, name)
	body, hash, ok := readProjectFile(path, roots)
	if !ok {
		return projectFile{}, false
	}
	return projectFile{path: path, body: body, hash: hash}, true
}

// readProjectFile reads a regular file inside roots. Symlinks are resolved
// first; a link that leaves the roots is not followed. The hash covers the
// bytes read, which are capped so a huge file cannot be pulled into memory.
func readProjectFile(path string, roots []string) (string, string, bool) {
	st, err := os.Lstat(path)
	if err != nil || st.IsDir() {
		return "", "", false
	}
	resolved := path
	if st.Mode()&os.ModeSymlink != 0 {
		resolved, err = filepath.EvalSymlinks(path)
		if err != nil {
			return "", "", false
		}
		st, err = os.Stat(resolved)
		if err != nil || !st.Mode().IsRegular() {
			return "", "", false
		}
	} else if !st.Mode().IsRegular() {
		return "", "", false
	}
	if !insideRoots(resolved, roots) {
		return "", "", false
	}
	f, err := os.Open(resolved)
	if err != nil {
		return "", "", false
	}
	defer f.Close()
	buf, err := io.ReadAll(io.LimitReader(f, maxProjectBytes+1))
	if err != nil || len(buf) == 0 {
		return "", "", false
	}
	sum := sha256.Sum256(buf)
	body := neutralizeProject(string(buf))
	return body, hex.EncodeToString(sum[:8]), true
}

// neutralizeProject keeps the wrapper tag from being closed by the file.
// The kernel does not parse this text as instructions.
func neutralizeProject(s string) string {
	s = strings.ReplaceAll(s, "</project_instructions>", "</ project_instructions>")
	s = strings.ReplaceAll(s, "</PROJECT_INSTRUCTIONS>", "</ PROJECT_INSTRUCTIONS>")
	return s
}

// renderProject keeps the nearest files when the set exceeds the cap.
// Farther files are dropped first. truncated is true when any byte was left out.
// The returned files are the ones that contributed to the text, nearest last.
func renderProject(files []projectFile) (string, []projectFile, bool) {
	kept := append([]projectFile(nil), files...)
	for i := range kept {
		kept[i].body = strings.TrimRight(kept[i].body, "\n")
	}
	size := func(list []projectFile) int {
		n := 0
		for _, c := range list {
			n += len(c.path) + len(c.body) + 8
		}
		return n
	}
	truncated := false
	for len(kept) > 1 && size(kept) > maxProjectBytes {
		kept = kept[1:]
		truncated = true
	}
	if len(kept) == 1 && size(kept) > maxProjectBytes {
		c := &kept[0]
		header := len(c.path) + 8
		budget := maxProjectBytes - header
		if budget < 0 {
			budget = 0
		}
		if len(c.body) > budget {
			c.body = trimUTF8(c.body, budget)
			truncated = true
		}
	}
	var b strings.Builder
	for _, c := range kept {
		fmt.Fprintf(&b, "## %s\n%s\n", c.path, c.body)
	}
	return strings.TrimRight(b.String(), "\n"), kept, truncated
}

func trimUTF8(s string, budget int) string {
	if budget >= len(s) {
		return s
	}
	if budget <= 0 {
		return ""
	}
	for budget > 0 && !utf8Start(s[budget]) {
		budget--
	}
	return s[:budget]
}

func utf8Start(b byte) bool {
	return b&0xC0 != 0x80
}

func projectSig(files []projectFile, truncated bool) string {
	h := sha256.New()
	if truncated {
		h.Write([]byte("truncated\n"))
	}
	for _, f := range files {
		h.Write([]byte(f.path))
		h.Write([]byte{0})
		h.Write([]byte(f.hash))
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil)[:6])
}
