package bot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"xbot/internal/fsutil"
	"xbot/internal/memory"
)

// legacyStyle is the speaking style old configs kept. Bots no longer have one:
// how a bot talks is written in its persona card. Loading an old config moves
// whatever was set into the bot's memory once, then drops the field.
type legacyStyle struct {
	Language string `json:"language"`
	Tone     string `json:"tone"`
	Length   string `json:"length"`
	Emoji    string `json:"emoji"`
}

func hasLegacyStyle(raw []byte) bool {
	var cfg struct {
		Style json.RawMessage `json:"style"`
	}
	return json.Unmarshal(raw, &cfg) == nil && len(cfg.Style) > 0 && string(cfg.Style) != "null"
}

// rememberPreference saves the old style as a profile fact. A full profile must
// not keep the bot from loading, so the fact goes to the log tier instead,
// where recall still finds it and the bot can fold it in when it consolidates.
func rememberPreference(botName string, store memory.Store, text string) error {
	ctx := context.Background()
	for _, tier := range []memory.Tier{memory.TierProfile, memory.TierLog} {
		facts, err := store.Recall(ctx, memory.Query{Scope: memory.ScopeBot, Tier: tier, BotName: botName})
		if err != nil {
			return err
		}
		for _, f := range facts {
			if f.Text == text {
				return nil
			}
		}
	}
	fact := memory.Fact{Key: "speaking-preference", Text: text, Scope: memory.ScopeBot, Tier: memory.TierProfile, BotName: botName, WrittenBy: botName, Time: time.Now().UTC()}
	err := store.Remember(ctx, fact)
	if errors.Is(err, memory.ErrProfileFull) {
		fact.Tier = memory.TierLog
		err = store.Remember(ctx, fact)
	}
	return err
}

// migrateStyle commits memory first, then drops the old config field. The
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
			Style legacyStyle `json:"style"`
		}
		if err := json.Unmarshal(raw, &old); err != nil {
			return err
		}
		var parts []string
		for _, v := range []struct{ name, value string }{{"语言", old.Style.Language}, {"语气", old.Style.Tone}, {"篇幅", old.Style.Length}, {"emoji", old.Style.Emoji}} {
			if value := strings.Join(strings.Fields(v.value), " "); value != "" {
				parts = append(parts, v.name+" "+value)
			}
		}
		if len(parts) > 0 {
			text := "用户希望我说话：" + strings.Join(parts, "，") + "。"
			if err := rememberPreference(b.Name(), memory.Open(b.Root), text); err != nil {
				return fmt.Errorf("bot: migrate speaking preference: %w", err)
			}
		}
		return b.saveConfig()
	})
}
