package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"xbot/internal/fsutil"
	"xbot/internal/memory"
)

// UnmarshalJSON accepts just the language and three discarded legacy fields.
// This also lets strict template decoding ignore those fields without accepting
// misspelled or otherwise unknown fields.
func (s *Style) UnmarshalJSON(raw []byte) error {
	var v struct {
		Language string `json:"language"`
		Tone     string `json:"tone"`
		Length   string `json:"length"`
		Emoji    string `json:"emoji"`
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&v); err != nil {
		return err
	}
	s.Language = v.Language
	return nil
}

func hasLegacyStyle(raw []byte) bool {
	var cfg struct {
		Style map[string]json.RawMessage `json:"style"`
	}
	if json.Unmarshal(raw, &cfg) != nil {
		return false
	}
	for _, k := range []string{"tone", "length", "emoji"} {
		if _, ok := cfg.Style[k]; ok {
			return true
		}
	}
	return false
}

// migrateStyle commits memory first, then drops the old config fields. The
// config lock serializes concurrent loads; exact-text deduplication also makes
// retry after a failed config write harmless. No migration marker is needed.
func (b *Bot) migrateStyle(raw []byte) error {
	if !hasLegacyStyle(raw) {
		return nil
	}
	path := filepath.Join(b.dir(), "config.json")
	return fsutil.WithLock(path+".lock", func() error {
		name := b.Name()
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var cfg Config
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return err
		}
		b.cfg = cfg
		if b.cfg.Name == "" {
			b.cfg.Name = name
		}
		if !hasLegacyStyle(raw) {
			return nil
		}
		var old struct {
			Style struct{ Tone, Length, Emoji string } `json:"style"`
		}
		if err := json.Unmarshal(raw, &old); err != nil {
			return err
		}
		var parts []string
		for _, v := range []struct{ name, value string }{{"语气", old.Style.Tone}, {"篇幅", old.Style.Length}, {"emoji", old.Style.Emoji}} {
			if value := strings.Join(strings.Fields(v.value), " "); value != "" {
				parts = append(parts, v.name+" "+value)
			}
		}
		if len(parts) > 0 {
			text := "主人希望我说话：" + strings.Join(parts, "，") + "。"
			store := memory.Open(b.Root)
			facts, err := store.Recall(context.Background(), memory.Query{Scope: memory.ScopeBot, Tier: memory.TierProfile, BotName: b.Name()})
			if err != nil {
				return err
			}
			exists := false
			for _, f := range facts {
				if f.Text == text {
					exists = true
					break
				}
			}
			if !exists {
				if err := store.Remember(context.Background(), memory.Fact{Key: "speaking-preference", Text: text, Scope: memory.ScopeBot, Tier: memory.TierProfile, BotName: b.Name(), WrittenBy: b.Name(), Time: time.Now().UTC()}); err != nil {
					return fmt.Errorf("bot: migrate speaking preference: %w", err)
				}
			}
		}
		if b.cfg.Style != nil && *b.cfg.Style == (Style{}) {
			b.cfg.Style = nil
		}
		return b.saveConfig()
	})
}
