package bot

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"xbot/internal/fsutil"
	"xbot/internal/inbox"
	"xbot/internal/link"
)

const (
	// SeedName is the bot created when a root has none yet.
	// The case is part of the name: it is stored and shown exactly as written.
	SeedName = "XBot"
)

var (
	// ErrNoPrimary means the root has no primary bot recorded.
	ErrNoPrimary = errors.New("bot: no primary")
	// ErrPrimary means delete was asked to remove the primary bot.
	ErrPrimary = errors.New("bot: the primary cannot be deleted; transfer it first")
	// ErrLast means delete would leave the root with no bots.
	ErrLast = errors.New("bot: the last bot cannot be deleted")
)

// Seed is how the first bot is created when a command finds an empty root.
type Seed struct {
	Persona string
	Links   []string
	Owner   string
}

type primaryFile struct {
	Name string `json:"name"`
}

func primaryPath(root string) string { return filepath.Join(root, "primary.json") }

func primaryLock(root string) string { return filepath.Join(root, "primary.lock") }

// Primary is the bot that receives input which did not name one.
func Primary(root string) (string, error) {
	raw, err := os.ReadFile(primaryPath(root))
	if errors.Is(err, os.ErrNotExist) {
		return "", ErrNoPrimary
	}
	if err != nil {
		return "", err
	}
	var f primaryFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return "", err
	}
	if !ValidName(f.Name) {
		return "", ErrNoPrimary
	}
	if _, err := Load(root, f.Name); err != nil {
		return "", err
	}
	return f.Name, nil
}

// EnsurePrimary returns the primary bot, creating the seed when the root is empty.
// created is true only when the seed directory was just made.
func EnsurePrimary(root string, seed Seed) (*Bot, bool, error) {
	var b *Bot
	var created bool
	err := fsutil.WithLock(primaryLock(root), func() error {
		if name, err := Primary(root); err == nil {
			loaded, err := Load(root, name)
			if err != nil {
				return err
			}
			b = loaded
			return nil
		} else if !errors.Is(err, ErrNoPrimary) && !errors.Is(err, ErrNotFound) {
			return err
		}
		bots, err := List(root)
		if err != nil {
			return err
		}
		if len(bots) == 0 {
			persona := seed.Persona
			if persona == "" {
				persona = DefaultPersona
			}
			made, err := Create(root, SeedName, persona, seed.Links)
			if err != nil {
				return err
			}
			if seed.Owner != "" {
				if err := made.SetOwners([]string{seed.Owner}); err != nil {
					return err
				}
			}
			if err := setPrimaryLocked(root, made.Name()); err != nil {
				return err
			}
			loaded, err := Load(root, made.Name())
			if err != nil {
				return err
			}
			b = loaded
			created = true
			return nil
		}
		chosen := bots[0]
		for _, cfg := range bots[1:] {
			if cfg.Created.Before(chosen.Created) || (cfg.Created.Equal(chosen.Created) && cfg.Name < chosen.Name) {
				chosen = cfg
			}
		}
		if err := setPrimaryLocked(root, chosen.Name); err != nil {
			return err
		}
		loaded, err := Load(root, chosen.Name)
		if err != nil {
			return err
		}
		b = loaded
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return b, created, nil
}

// SetPrimary transfers the role. The new primary is pinned.
func SetPrimary(root, name string) error {
	return fsutil.WithLock(primaryLock(root), func() error {
		return setPrimaryLocked(root, name)
	})
}

func setPrimaryLocked(root, name string) error {
	if !ValidName(name) {
		return fmt.Errorf("bot: invalid name %q", name)
	}
	b, err := Load(root, name)
	if err != nil {
		return err
	}
	raw, err := fsutil.Marshal(primaryFile{Name: name})
	if err != nil {
		return err
	}
	if err := fsutil.WriteAtomic(primaryPath(root), raw, 0600); err != nil {
		return err
	}
	if b.Config().Pinned {
		return nil
	}
	return b.SetPinned(true)
}

// Delete removes a bot directory. The primary and the last bot stay.
func Delete(root, name string) error {
	if !ValidName(name) {
		return fmt.Errorf("bot: invalid name %q", name)
	}
	return fsutil.WithLock(primaryLock(root), func() error {
		if _, err := Load(root, name); err != nil {
			return err
		}
		all, err := List(root)
		if err != nil {
			return err
		}
		if len(all) <= 1 {
			return ErrLast
		}
		primary, err := Primary(root)
		if err != nil && !errors.Is(err, ErrNoPrimary) && !errors.Is(err, ErrNotFound) {
			return err
		}
		if name == primary {
			return ErrPrimary
		}
		if err := os.RemoveAll(Dir(root, name)); err != nil {
			return err
		}
		return removeVaultKey(root, name)
	})
}

// PostUnaddressed delivers an event that named no bot into the primary inbox.
func PostUnaddressed(root string, ev link.Event) error {
	name, err := Primary(root)
	if err != nil {
		return err
	}
	b, err := Load(root, name)
	if err != nil {
		return err
	}
	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		return err
	}
	return box.Post(ev)
}

// Order puts pinned bots first, and sorts by name inside each group.
func Order(cfgs []Config) []Config {
	out := append([]Config(nil), cfgs...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Pinned != out[j].Pinned {
			return out[i].Pinned
		}
		return out[i].Name < out[j].Name
	})
	return out
}
