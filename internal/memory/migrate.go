package memory

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"xbot/internal/fsutil"
)

const migrationNoteKey = "migration-profile-cap"

// Migrate folds legacy bots/<name>/people/<id>/memory into bots/<name>/memory.
// It runs on startup and on the first store operation. A second call is a no-op:
// the old directory is renamed to memory.bak (or memory.bak.N) and is not read again.
// Facts keep their timestamps and provenance. A merged profile that would pass
// ProfileBytes puts the overflow in the log tier and records why. Nothing is deleted.
func Migrate(root string) error {
	if root == "" {
		return nil
	}
	bots := filepath.Join(root, "bots")
	entries, err := os.ReadDir(bots)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, ent := range entries {
		if !ent.IsDir() || !ValidID(ent.Name()) {
			continue
		}
		if err := migrateBot(root, ent.Name()); err != nil {
			return err
		}
	}
	return nil
}

func migrateBot(root, botName string) error {
	people := filepath.Join(root, "bots", botName, "people")
	entries, err := os.ReadDir(people)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var sources []string
	for _, ent := range entries {
		if !ent.IsDir() || !ValidID(ent.Name()) {
			continue
		}
		src := filepath.Join(people, ent.Name(), "memory")
		st, err := os.Stat(src)
		if errors.Is(err, os.ErrNotExist) || (err == nil && !st.IsDir()) {
			continue
		}
		if err != nil {
			return err
		}
		sources = append(sources, src)
	}
	if len(sources) == 0 {
		return nil
	}
	dest := filepath.Join(root, "bots", botName, "memory")
	return fsutil.WithLock(filepath.Join(dest, ".lock"), func() error {
		profile, err := loadTier(dest, TierProfile, ScopeBot, botName, "")
		if err != nil {
			return err
		}
		logFacts, err := loadTier(dest, TierLog, ScopeBot, botName, "")
		if err != nil {
			return err
		}
		var incomingProfile, incomingLog []Fact
		for _, src := range sources {
			p, l, err := loadLegacy(src, botName)
			if err != nil {
				return err
			}
			incomingProfile = append(incomingProfile, p...)
			incomingLog = append(incomingLog, l...)
		}
		profile, logFacts, profileChanged, logChanged := mergeLegacy(botName, profile, logFacts, incomingProfile, incomingLog)
		if profileChanged {
			if err := saveTier(dest, TierProfile, profile); err != nil {
				return err
			}
		}
		if logChanged {
			if err := writeTier(dest, TierLog, logFacts, false); err != nil {
				return err
			}
		}
		for _, src := range sources {
			if err := backupDir(src); err != nil {
				return err
			}
		}
		return nil
	})
}

func loadLegacy(dir, botName string) (profile, logFacts []Fact, err error) {
	err = fsutil.WithLock(filepath.Join(dir, ".lock"), func() error {
		var err error
		profile, err = loadTier(dir, TierProfile, ScopeBot, botName, "")
		if err != nil {
			return err
		}
		logFacts, err = loadTier(dir, TierLog, ScopeBot, botName, "")
		if err != nil {
			return err
		}
		extra, err := readLooseIndex(filepath.Join(dir, ".index.json"))
		if err != nil {
			return err
		}
		for _, f := range extra {
			f = asBotFact(f, botName)
			if f.Tier == TierProfile {
				profile = append(profile, f)
				continue
			}
			f.Tier = TierLog
			logFacts = append(logFacts, f)
		}
		return nil
	})
	return profile, logFacts, err
}

func readLooseIndex(path string) ([]Fact, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, nil
	}
	var facts []Fact
	if err := json.Unmarshal(raw, &facts); err != nil {
		return nil, fmt.Errorf("memory: %s: %w", path, err)
	}
	return facts, nil
}

func asBotFact(f Fact, botName string) Fact {
	f.Scope = ScopeBot
	f.BotName = botName
	f.PersonID = ""
	f.Text = oneLine(f.Text)
	f.Key = oneLine(f.Key)
	if f.Key == "" {
		f.Key = clipKey(f.Text)
	} else {
		f.Key = clipKey(f.Key)
	}
	if f.Time.IsZero() {
		f.Time = time.Unix(0, 0).UTC()
	}
	f.Time = f.Time.UTC()
	if f.Tier != TierProfile {
		f.Tier = TierLog
	}
	return f
}

func mergeLegacy(botName string, profile, logFacts, incomingProfile, incomingLog []Fact) ([]Fact, []Fact, bool, bool) {
	seen := map[string]bool{}
	for i := range profile {
		profile[i] = asBotFact(profile[i], botName)
		profile[i].Tier = TierProfile
		seen[factID(profile[i])] = true
	}
	for i := range logFacts {
		logFacts[i] = asBotFact(logFacts[i], botName)
		logFacts[i].Tier = TierLog
		seen[factID(logFacts[i])] = true
	}
	profileChanged := false
	logChanged := false
	profileOver := len(renderMD(profile)) > ProfileBytes
	var overflow []Fact
	for _, f := range incomingProfile {
		f = asBotFact(f, botName)
		f.Tier = TierProfile
		if f.Text == "" || seen[factID(f)] {
			continue
		}
		if profileOver {
			f.Tier = TierLog
			overflow = append(overflow, f)
			continue
		}
		trial := append(append([]Fact{}, profile...), f)
		if len(renderMD(trial)) > ProfileBytes {
			f.Tier = TierLog
			overflow = append(overflow, f)
			continue
		}
		profile = trial
		seen[factID(f)] = true
		profileChanged = true
	}
	for _, f := range incomingLog {
		f = asBotFact(f, botName)
		f.Tier = TierLog
		if f.Text == "" || seen[factID(f)] {
			continue
		}
		logFacts = append(logFacts, f)
		seen[factID(f)] = true
		logChanged = true
	}
	for _, f := range overflow {
		if f.Text == "" || seen[factID(f)] {
			continue
		}
		logFacts = append(logFacts, f)
		seen[factID(f)] = true
		logChanged = true
	}
	if len(overflow) > 0 && !hasKey(logFacts, migrationNoteKey) && !hasKey(profile, migrationNoteKey) {
		note := Fact{
			Key:       migrationNoteKey,
			Kind:      KindWork,
			Scope:     ScopeBot,
			Tier:      TierLog,
			BotName:   botName,
			WrittenBy: botName,
			Text:      fmt.Sprintf("migration: %d profile fact(s) from the removed bot-person scope moved to the log tier because the %d-byte profile cap would have been exceeded", len(overflow), ProfileBytes),
			Time:      time.Now().UTC(),
		}
		logFacts = append(logFacts, note)
		logChanged = true
	}
	return profile, logFacts, profileChanged, logChanged
}

func factID(f Fact) string {
	return f.Key + "\x00" + f.Text + "\x00" + f.Time.UTC().Format(time.RFC3339Nano) + "\x00" + f.WrittenBy + "\x00" + string(f.Kind) + "\x00" + f.Device + "\x00" + boolMark(f.Untrusted) + "\x00" + boolMark(f.OwnerRecorded)
}

func boolMark(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

func hasKey(facts []Fact, key string) bool {
	for _, f := range facts {
		if f.Key == key {
			return true
		}
	}
	return false
}

func backupDir(src string) error {
	base := src + ".bak"
	dest := base
	for i := 1; i < 1000; i++ {
		if _, err := os.Stat(dest); errors.Is(err, os.ErrNotExist) {
			return os.Rename(src, dest)
		}
		dest = fmt.Sprintf("%s.%d", base, i)
	}
	return fmt.Errorf("memory: too many backups for %s", src)
}
