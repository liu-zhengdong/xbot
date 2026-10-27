package bot

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"xbot/internal/personacard"
)

// SetPersonaCard binds this bot to a persona card. Empty goes back to the default card.
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

// PickedCard is the bound card, or the library's default card when none was
// picked or the picked one is gone. The binding is read from disk because a
// long-running kernel keeps the config it loaded, and the owner picks cards
// from the web. ok is false only for a bot without exactly one owner.
func (b *Bot) PickedCard() (personacard.Card, bool) {
	if len(b.cfg.Owners) != 1 || b.Root == "" {
		return personacard.Card{}, false
	}
	if id := b.boundCard(); id != "" {
		if c, err := personacard.Get(b.Root, id); err == nil {
			return c, true
		}
	}
	if c, ok := personacard.Default(b.Root); ok {
		return c, true
	}
	// A library nobody has opened yet gets its default card on first use.
	if personacard.EnsureSeeds(b.Root) != nil {
		return personacard.Card{}, false
	}
	return personacard.Default(b.Root)
}

// ComposedPersona is the duty (persona.md), plus the picked card as its own
// section. A card adds voice; it never replaces the duty. Display metadata
// such as the description never becomes instructions.
func (b *Bot) ComposedPersona() (string, error) {
	duty, err := b.ReadPersona()
	if err != nil {
		return "", err
	}
	c, ok := b.PickedCard()
	if !ok {
		return duty, nil
	}
	return strings.TrimSpace(duty) + "\n\n### 人设卡 · " + c.Name + "\n这段只管口吻、节奏和关系分寸，不改上面的职责；两者冲突时职责优先。\n" + strings.TrimSpace(c.Body), nil
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
