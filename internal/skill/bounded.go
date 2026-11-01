package skill

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ListBounded follows List's directory and metadata rules, limiting every
// candidate's raw SKILL.md before parsing it. A missing library is empty.
func ListBounded(dir string, maxRaw int64) ([]Info, error) {
	entries, err := os.ReadDir(dir)
	if dir == "" || os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var infos []Info
	// ReadDir supplies the same slug order as List.
	for _, entry := range entries {
		if !ValidSlug(entry.Name()) {
			continue
		}
		st, err := os.Stat(filepath.Join(dir, entry.Name()))
		if err != nil || !st.IsDir() {
			continue
		}
		info, _, err := OpenBounded(dir, entry.Name(), maxRaw)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		infos = append(infos, info)
	}
	return infos, nil
}

// OpenBounded returns the full parsed body, including an explicitly empty body.
// The raw file is read through maxRaw+1, so growing files cannot bypass the cap.
func OpenBounded(dir, slug string, maxRaw int64) (Info, string, error) {
	if !ValidSlug(slug) {
		return Info{}, "", fmt.Errorf("skill: bad slug %q", slug)
	}
	f, err := os.Open(filepath.Join(dir, slug, "SKILL.md"))
	if err != nil {
		return Info{}, "", err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxRaw+1))
	if err != nil {
		return Info{}, "", err
	}
	if int64(len(raw)) > maxRaw {
		return Info{}, "", fmt.Errorf("skill: %s raw SKILL.md exceeds %d bytes", slug, maxRaw)
	}
	name, title, desc, body := parse(string(raw))
	if name == "" {
		name = slug
	}
	return Info{Slug: slug, Name: name, Title: title, Description: desc}, body, nil
}
