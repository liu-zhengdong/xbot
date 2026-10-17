package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"xbot/internal/bm25"
	"xbot/internal/memory"
)

type recallTool struct{ k *Kernel }

func (recallTool) Name() string { return "recall" }
func (recallTool) Description() string {
	return "Search visible memory folders; returns up to 20 ranked path:line hits."
}
func (recallTool) Parameters() any {
	return schema(map[string]any{"query": stringProp("keywords"), "scope": stringProp("optional bot or person"), "since": stringProp("optional YYYY-MM-DD")}, []string{"query"})
}
func (recallTool) Confirm(json.RawMessage) (bool, string, error) { return false, "", nil }
func (t recallTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct{ Query, Scope, Since string }
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	if in.Scope != "" && in.Scope != "bot" && in.Scope != "person" {
		return "", errors.New("memory: scope must be bot or person")
	}
	var since time.Time
	if in.Since != "" {
		var err error
		since, err = time.ParseInLocation("2006-01-02", in.Since, time.Local)
		if err != nil {
			return "", err
		}
	}
	var docs, locations []string
	person := t.k.memoryPersonOf(t.k.turnFor(ctx))
	for _, scope := range []string{"bot", "person"} {
		if in.Scope != "" && in.Scope != scope {
			continue
		}
		group, id := "bots", t.k.Bot.Name()
		if scope == "person" {
			group, id = "people", person
		}
		if !memory.ValidID(id) {
			continue
		}
		dir := filepath.Join(t.k.Bot.Root, group, id, "memory")
		err := filepath.WalkDir(dir, func(path string, ent fs.DirEntry, err error) error {
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return nil
				}
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if ent.Type()&os.ModeSymlink != 0 {
				return nil
			}
			if strings.Contains(ent.Name(), ".bak") {
				if ent.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if ent.IsDir() {
				return nil
			}
			ext := filepath.Ext(path)
			if ext != ".md" && ext != ".txt" {
				return nil
			}
			st, err := ent.Info()
			if err != nil {
				return err
			}
			if !st.Mode().IsRegular() {
				return nil
			}
			rel, _ := filepath.Rel(dir, path)
			rel = filepath.ToSlash(rel)
			dated := rel == "memory.md" || strings.HasPrefix(rel, "log/") && ext == ".md"
			if !since.IsZero() && !dated && st.ModTime().Before(since) {
				return nil
			}
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			raw, err := io.ReadAll(io.LimitReader(f, 256<<10))
			f.Close()
			if err != nil {
				return err
			}
			for i, line := range strings.Split(string(raw), "\n") {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "#") || rel == "memory.md" && line == "（暂无）" {
					continue
				}
				if !since.IsZero() && dated {
					date := ""
					if rel == "memory.md" && strings.HasPrefix(line, "- (") && len(line) >= 14 && line[13] == ')' {
						date = line[3:13]
					}
					if rel != "memory.md" && strings.HasPrefix(line, "- ") && len(line) >= 12 {
						date = line[2:12]
					}
					d, err := time.ParseInLocation("2006-01-02", date, time.Local)
					if err != nil || d.Before(since) {
						continue
					}
				}
				docs = append(docs, line)
				locations = append(locations, fmt.Sprintf("%s:%s:%d", scope, rel, i+1))
			}
			return nil
		})
		if err != nil {
			return "", err
		}
	}
	hits := bm25.Top(in.Query, docs, -1)
	if len(hits) == 0 {
		return "no matches", nil
	}
	var out strings.Builder
	for i, hit := range hits {
		if i == 20 {
			break
		}
		text := []rune(docs[hit.Index])
		if len(text) > 200 {
			text = append(text[:200], '…')
		}
		fmt.Fprintf(&out, "- %s: %s\n", locations[hit.Index], string(text))
	}
	if len(hits) > 20 {
		fmt.Fprintf(&out, "(%d more; narrow the query or use since=)\n", len(hits)-20)
	}
	return strings.TrimSuffix(out.String(), "\n"), nil
}
