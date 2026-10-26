package bot

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"xbot/internal/personacard"
)

// SetPersonaCard binds this bot to one of its owner's persona cards. Empty unbinds it.
// The card is read on every turn, so later edits to it reach this bot too.
func (b *Bot) SetPersonaCard(id string) error {
	id = strings.TrimSpace(id)
	if id != "" && !personacard.ValidID(id) {
		return errors.New("bot: bad persona card")
	}
	b.cfg.PersonaCard = id
	b.cardSet = true
	return b.SaveConfig()
}

// PersonaText is the picked card's body, or persona.md when no card is picked
// or the card is gone. The binding is read from disk because a long-running
// kernel keeps the config it loaded, and the owner picks cards from the web.
func (b *Bot) PersonaText() (string, error) {
	id := b.boundCard()
	if owners := b.cfg.Owners; id != "" && len(owners) == 1 && b.Root != "" {
		if c, err := personacard.Get(b.Root, owners[0], id); err == nil {
			return c.Body, nil
		}
	}
	return b.ReadPersona()
}

func (b *Bot) boundCard() string {
	raw, err := os.ReadFile(filepath.Join(b.dir(), "config.json"))
	if err != nil {
		return b.cfg.PersonaCard
	}
	var cfg struct {
		PersonaCard string `json:"persona_card"`
	}
	if json.Unmarshal(raw, &cfg) != nil {
		return b.cfg.PersonaCard
	}
	return cfg.PersonaCard
}
