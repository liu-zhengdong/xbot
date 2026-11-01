package bot

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"xbot/internal/fsutil"
)

// DisabledSkills reads the saved switches so a running bot sees web and tool changes.
func (b *Bot) DisabledSkills() []string {
	raw, err := os.ReadFile(filepath.Join(b.dir(), "config.json"))
	var cfg struct {
		DisabledSkills []string `json:"disabled_skills"`
	}
	if err == nil && json.Unmarshal(raw, &cfg) == nil {
		return cfg.DisabledSkills
	}
	return slices.Clone(b.cfg.DisabledSkills)
}

// SetSkillDisabled updates an already resolved local Bot's switch without loading
// or migrating it. The existing writer reads and merges fresh config under its lock.
func SetSkillDisabled(root, name, slug string, disabled bool) error {
	b := &Bot{Root: root, cfg: Config{Name: name}}
	return b.setSkillDisabled(slug, disabled)
}

func (b *Bot) setSkillDisabled(slug string, disabled bool) error {
	slug = strings.TrimSpace(slug)
	if slug == "" || strings.ContainsAny(slug, "/\\") {
		return errors.New("bot: bad skill")
	}
	return fsutil.WithLock(filepath.Join(b.dir(), "config.json.lock"), func() error {
		raw, err := os.ReadFile(filepath.Join(b.dir(), "config.json"))
		if err != nil {
			return err
		}
		fresh := &Bot{Root: b.Root}
		if err := json.Unmarshal(raw, &fresh.cfg); err != nil {
			return err
		}
		if fresh.cfg.Name == "" {
			fresh.cfg.Name = b.Name()
		}
		if disabled {
			fresh.cfg.DisabledSkills = addUnique(fresh.cfg.DisabledSkills, slug)
		} else {
			fresh.cfg.DisabledSkills = slices.DeleteFunc(fresh.cfg.DisabledSkills, func(s string) bool { return s == slug })
		}
		// Change only the switch field. A nonmigrating caller must preserve
		// legacy style until the normal loader can commit its memory migration.
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return err
		}
		if fields == nil {
			fields = make(map[string]json.RawMessage)
		}
		fields["name"], _ = json.Marshal(fresh.cfg.Name)
		if len(fresh.cfg.DisabledSkills) == 0 {
			delete(fields, "disabled_skills")
		} else {
			fields["disabled_skills"], _ = json.Marshal(fresh.cfg.DisabledSkills)
		}
		updated, err := fsutil.Marshal(fields)
		if err != nil {
			return err
		}
		if err := fsutil.WriteAtomic(filepath.Join(b.dir(), "config.json"), updated, 0600); err != nil {
			return err
		}
		b.cfg.DisabledSkills = slices.Clone(fresh.cfg.DisabledSkills)
		return nil
	})
}

// ForgetSkill drops slug from every bot's disable list, for a skill leaving the library.
// Bots that never switched it off are left untouched.
func ForgetSkill(root, slug string) error {
	cfgs, err := List(root)
	if err != nil {
		return err
	}
	for _, cfg := range cfgs {
		if !slices.Contains(cfg.DisabledSkills, slug) {
			continue
		}
		b, err := Load(root, cfg.Name)
		if err != nil {
			return err
		}
		if err := b.EnableSkill(slug); err != nil {
			return err
		}
	}
	return nil
}
