package memory

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"xbot/internal/fsutil"
)

var datedFactRE = regexp.MustCompile(`^- \((\d{4}-\d{2}-\d{2})\) (.+)$`)
var viaRE = regexp.MustCompile(` via ([A-Za-z0-9_-]{1,64})$`)

// ValidMemoryLine reports a memory.md entry the store keeps: - (YYYY-MM-DD) text.
// Other lines outside the index section are dropped when the store rewrites
// the file.
func ValidMemoryLine(line string) bool {
	m := datedFactRE.FindStringSubmatch(strings.TrimSuffix(line, "\r"))
	if m == nil {
		return false
	}
	_, err := time.Parse("2006-01-02", m[1])
	return err == nil
}

func memoryIndex(body string) string {
	var lines []string
	in := false
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.HasPrefix(line, "## ") {
			if in {
				break
			}
			in = line == "## 索引"
			continue
		}
		if in {
			lines = append(lines, line)
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func renderMemoryMD(facts []Fact, index string) string {
	sort.SliceStable(facts, func(i, j int) bool {
		if facts[i].Time.Equal(facts[j].Time) {
			return facts[i].Key < facts[j].Key
		}
		return facts[i].Time.Before(facts[j].Time)
	})
	if index == "" {
		index = "（暂无）"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# memory\n\n## 索引\n%s\n\n## 条目\n", index)
	for _, f := range facts {
		fmt.Fprintf(&b, "- (%s) %s", f.Time.In(time.Local).Format("2006-01-02"), oneLine(f.Text))
		if f.Scope == ScopePerson && f.WrittenBy != "" {
			fmt.Fprintf(&b, " via %s", oneLine(f.WrittenBy))
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func parseMemoryMD(body string, scope Scope) []Fact {
	var facts []Fact
	inIndex := false
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.HasPrefix(line, "## ") {
			inIndex = line == "## 索引"
		}
		if inIndex {
			continue
		}
		m := datedFactRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		date, err := time.ParseInLocation("2006-01-02", m[1], time.Local)
		if err != nil {
			continue
		}
		f := Fact{Text: m[2], Time: date.UTC(), Kind: KindAttribute}
		if scope == ScopeBot {
			f.Kind = KindWork
		}
		f.Untrusted = strings.Contains(f.Text, untrustedMarker)
		f.OwnerRecorded = strings.Contains(f.Text, ownerMarker)
		if via := viaRE.FindStringSubmatchIndex(f.Text); scope == ScopePerson && via != nil {
			f.WrittenBy = f.Text[via[2]:via[3]]
			f.Text = f.Text[:via[0]]
		}
		f.Key = clipKey(f.Text)
		facts = append(facts, f)
	}
	return facts
}

// The older bot-person merge can encounter either generation of profile.
func legacyProfileBase(dir string) string {
	if _, err := os.Stat(filepath.Join(dir, "memory.md")); err == nil {
		return "memory"
	}
	return "profile"
}

func migrateLayout(root string) {
	for _, group := range []string{"people", "bots"} {
		entries, err := os.ReadDir(filepath.Join(root, group))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			log.Printf("memory: layout %s: %v", group, err)
			continue
		}
		for _, ent := range entries {
			if !ent.IsDir() || !ValidID(ent.Name()) {
				continue
			}
			dir := filepath.Join(root, group, ent.Name(), "memory")
			if !dirExists(dir) {
				continue
			}
			err := fsutil.WithLock(filepath.Join(dir, ".lock"), func() error {
				scope, bot, person := ScopeBot, ent.Name(), ""
				if group == "people" {
					scope, bot, person = ScopePerson, "", ent.Name()
				}
				if err := migrateLogs(dir, scope, bot, person); err != nil {
					return err
				}

				if _, err := os.Stat(filepath.Join(dir, "profile.md")); errors.Is(err, os.ErrNotExist) {
					// A previous crash may have left only the index behind.
					if _, err := os.Stat(filepath.Join(dir, "memory.md")); err != nil {
						return nil
					}
				} else if err != nil {
					return err
				} else {
					scope, bot, person := ScopeBot, ent.Name(), ""
					if group == "people" {
						scope, bot, person = ScopePerson, "", ent.Name()
					}
					legacy, err := loadNamed(dir, "profile", scope, TierProfile, bot, person)
					if err != nil {
						return err
					}
					var facts []Fact
					if _, err := os.Stat(filepath.Join(dir, "memory.md")); err == nil {
						facts, err = loadNamed(dir, "memory", scope, TierProfile, bot, person)
						if err != nil {
							return err
						}
					} else if !errors.Is(err, os.ErrNotExist) {
						return err
					}
					seen := map[string]bool{}
					for _, f := range facts {
						seen[factID(f)] = true
					}
					for _, f := range legacy {
						if !seen[factID(f)] {
							facts = append(facts, f)
							seen[factID(f)] = true
						}
					}
					if err := writeTier(dir, TierProfile, facts, false); err != nil {
						return err
					}
				}
				for _, name := range []string{"profile.md", "profile.index.json"} {
					if err := backupLayoutFile(filepath.Join(dir, name)); err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				log.Printf("memory: layout %s: %v", dir, err)
			}
		}
	}
}

func backupLayoutFile(src string) error {
	if _, err := os.Stat(src); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	for i := 0; ; i++ {
		dest := src + ".bak"
		if i > 0 {
			dest = fmt.Sprintf("%s.%d", dest, i)
		}
		if _, err := os.Lstat(dest); errors.Is(err, os.ErrNotExist) {
			return os.Rename(src, dest)
		} else if err != nil {
			return err
		}
	}
}
