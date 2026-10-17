package memory

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"xbot/internal/fsutil"
)

var logLineRE = regexp.MustCompile(`^- (\d{4}-\d{2}-\d{2} \d{2}:\d{2}) \[([^\]\r\n]+)\] (.+)$`)
var monthNameRE = regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])\.md$`)

func monthPaths(dir string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(dir, "log"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, e := range entries {
		if monthNameRE.MatchString(e.Name()) {
			paths = append(paths, filepath.Join(dir, "log", e.Name()))
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(paths)))
	return paths, nil
}

func parseLogLine(line string, scope Scope, bot, person string) (Fact, bool) {
	m := logLineRE.FindStringSubmatch(strings.TrimSuffix(line, "\r"))
	if m == nil {
		return Fact{}, false
	}
	t, err := time.ParseInLocation("2006-01-02 15:04", m[1], time.Local)
	if err != nil {
		return Fact{}, false
	}
	f := Fact{Scope: scope, Tier: TierLog, BotName: bot, PersonID: person, Source: m[2], Text: m[3], Time: t, Kind: KindAttribute}
	if scope == ScopeBot {
		f.Kind, f.WrittenBy = KindWork, bot
	} else if f.Source != "对话" && f.Source != "episode" && f.Source != "整理" {
		f.WrittenBy = f.Source
	}
	f.Key = DefaultKey(f.Text)
	f.Untrusted, f.OwnerRecorded = strings.Contains(f.Text, untrustedMarker), strings.Contains(f.Text, ownerMarker)
	return f, true
}

func loadMonths(dir string, scope Scope, bot, person string, limit int, checkLegacy bool) ([]Fact, error) {
	if checkLegacy {
		for _, base := range []string{"log", "log.overflow"} {
			if _, err := os.Stat(filepath.Join(dir, base+".md")); err == nil {
				return nil, fmt.Errorf("memory: legacy log awaits migration: %s", dir)
			} else if !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
		}
	}
	paths, err := monthPaths(dir)
	if err != nil {
		return nil, err
	}
	var facts []Fact
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var month []Fact
		for _, line := range strings.Split(string(raw), "\n") {
			if f, ok := parseLogLine(line, scope, bot, person); ok {
				month = append(month, f)
			}
		}
		sort.SliceStable(month, func(i, j int) bool { return month[i].Time.Before(month[j].Time) })
		facts = append(month, facts...)
		if limit > 0 && len(facts) >= limit {
			return facts[len(facts)-limit:], nil
		}
	}
	return facts, nil
}

func writeMonths(dir string, facts []Fact, enforce bool) error {
	paths, err := monthPaths(dir)
	if err != nil {
		return err
	}
	bodies := map[string]*strings.Builder{}
	for _, path := range paths {
		bodies[path] = &strings.Builder{}
	}
	sort.SliceStable(facts, func(i, j int) bool { return facts[i].Time.Before(facts[j].Time) })
	for _, f := range facts {
		t := f.Time.In(time.Local)
		path := filepath.Join(dir, "log", t.Format("2006-01")+".md")
		if bodies[path] == nil {
			bodies[path] = &strings.Builder{}
		}
		source := oneLine(f.Source)
		if source == "" && f.Scope == ScopePerson {
			source = oneLine(f.WrittenBy)
		}
		if source == "" {
			source = "对话"
		}
		source = strings.ReplaceAll(source, "]", "")
		text := logText(f)
		fmt.Fprintf(bodies[path], "- %s [%s] %s\n", t.Format("2006-01-02 15:04"), source, text)
	}
	current := filepath.Join(dir, "log", time.Now().In(time.Local).Format("2006-01")+".md")
	// Validate every changed file before writing any of them.
	changed := map[string][]byte{}
	for path, body := range bodies {
		raw, err := os.ReadFile(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		next := []byte(body.String())
		if bytes.Equal(raw, next) && err == nil {
			continue
		}
		if enforce && path == current && len(next) > maxLogBytes && len(next) > len(raw) {
			return fmt.Errorf("memory: current month exceeds %d bytes; consolidate this month into a few 整理 lines", maxLogBytes)
		}
		changed[path] = next
	}
	if len(changed) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Join(dir, "log"), 0700); err != nil {
		return err
	}
	for path, body := range changed {
		if err := fsutil.WriteAtomic(path, body, 0600); err != nil {
			return err
		}
	}
	return nil
}

func migrateLogs(dir string, scope Scope, bot, person string) error {
	var legacy []Fact
	found := false
	for _, base := range []string{"log", "log.overflow"} {
		if _, err := os.Stat(filepath.Join(dir, base+".md")); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		found = true
		facts, err := loadNamed(dir, base, scope, TierLog, bot, person)
		if err != nil {
			return err
		}
		legacy = append(legacy, facts...)
	}
	if !found {
		return nil
	}
	facts, err := loadMonths(dir, scope, bot, person, 0, false)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	id := func(f Fact) string { return f.Time.In(time.Local).Format("2006-01-02 15:04") + "\x00" + f.Text }
	for _, f := range facts {
		seen[id(f)] = true
	}
	for _, f := range legacy {
		f.Text = logText(f)
		if !seen[id(f)] {
			facts = append(facts, f)
			seen[id(f)] = true
		}
	}
	if err := writeMonths(dir, facts, false); err != nil {
		return err
	}
	for _, name := range []string{"log.md", "log.index.json", "log.overflow.md", "log.overflow.index.json"} {
		if err := backupLayoutFile(filepath.Join(dir, name)); err != nil {
			return err
		}
	}
	return nil
}

// LogCount counts entry-shaped lines without constructing facts or parsing times.
func (s *FileStore) LogCount(ctxScope Scope, bot, person string) (int, error) {
	dir, err := s.dir(ctxScope, bot, person)
	if err != nil {
		return 0, err
	}
	n := 0
	err = fsutil.WithLock(filepath.Join(dir, ".lock"), func() error {
		paths, err := monthPaths(dir)
		if err != nil {
			return err
		}
		for _, path := range paths {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			scanner := bufio.NewScanner(f)
			scanner.Buffer(make([]byte, 4096), 1<<20)
			for scanner.Scan() {
				if logLineRE.Match(bytes.TrimSuffix(scanner.Bytes(), []byte("\r"))) {
					n++
				}
			}
			err = scanner.Err()
			f.Close()
			if err != nil {
				return err
			}
		}
		return nil
	})
	return n, err
}

// Existing markers carry provenance even though monthly files have no device field.
func logText(f Fact) string {
	text := oneLine(f.Text)
	if f.OwnerRecorded && !strings.Contains(text, ownerMarker) {
		return OwnerProvenance(text, f.Device)
	}
	if f.Untrusted && !strings.Contains(text, untrustedMarker) {
		return Provenance(text, f.WrittenBy, f.Device)
	}
	return text
}
