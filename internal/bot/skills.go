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
		fresh.skillsSet = true
		if err := fresh.saveConfig(); err != nil {
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
