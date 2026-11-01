package bot

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"xbot/internal/fsutil"
	"xbot/internal/work"
)

// ProfileChange is the runtime profile family. Role and execution budgets keep
// their separate settings paths. ValidateProfileChange copies supplied values.
type ProfileChange struct {
	Name, Description, Timezone, AvatarSeed *string
}

type ProfileChangeResult struct {
	Name          string   `json:"name"`
	PreviousName  string   `json:"previous_name"`
	AppliedFields []string `json:"applied_fields"`
	Moved         bool     `json:"moved"`
}

func ValidateProfileChange(change ProfileChange) (ProfileChange, error) {
	var normalized ProfileChange
	if change.Name != nil {
		name := flat(*change.Name)
		if !ValidName(name) || !fsutil.PortableName(name) {
			return normalized, fmt.Errorf("bot: invalid name %q", name)
		}
		normalized.Name = &name
	}
	if change.Description != nil {
		description, err := limitText(*change.Description, 160)
		if err != nil {
			return normalized, fmt.Errorf("bot: description: %w", err)
		}
		normalized.Description = &description
	}
	if change.Timezone != nil {
		zone := flat(*change.Timezone)
		if zone != "" {
			if _, err := time.LoadLocation(zone); err != nil {
				return normalized, fmt.Errorf("bot: timezone: %w", err)
			}
		}
		normalized.Timezone = &zone
	}
	if change.AvatarSeed != nil {
		seed := strings.TrimSpace(*change.AvatarSeed)
		if !ValidAvatarSeed(seed) {
			return normalized, fmt.Errorf("bot: avatar seed must be 1–%d characters without <>\"'&` or control characters", maxSeed)
		}
		normalized.AvatarSeed = &seed
	}
	return normalized, nil
}

// CheckProfileChange reads dependencies without loading/migrating another Bot.
// ApplyProfileChange repeats this check under the primary commit lock for moves.
func CheckProfileChange(root, from string, normalized ProfileChange) error {
	if !ValidName(from) {
		return fmt.Errorf("bot: invalid name %q", from)
	}
	if _, err := readProfileConfig(root, from); err != nil {
		return err
	}
	if normalized.Name == nil || *normalized.Name == from {
		return nil
	}
	to := *normalized.Name
	for _, pair := range [][2]string{
		{Dir(root, from), Dir(root, to)},
		{work.BotDir(root, from), work.BotDir(root, to)},
		{vaultKeyPath(root, from), vaultKeyPath(root, to)},
	} {
		if _, err := os.Stat(pair[1]); err == nil {
			if !samePath(pair[0], pair[1]) {
				return fmt.Errorf("bot: rename destination %q: %w", pair[1], ErrExists)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func readProfileConfig(root, name string) (Config, error) {
	var cfg Config
	raw, err := os.ReadFile(filepath.Join(Dir(root, name), "config.json"))
	if errors.Is(err, os.ErrNotExist) {
		return cfg, fmt.Errorf("bot %q: %w", name, ErrNotFound)
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cfg, err
	}
	if cfg.Name == "" {
		cfg.Name = name
	}
	return cfg, nil
}

// ApplyProfileChange accepts the normalized value produced at the external
// boundary. It returns the actual directory and committed fields on IO failure.
func (b *Bot) ApplyProfileChange(normalized ProfileChange) (current *Bot, result ProfileChangeResult, err error) {
	from, to := b.Name(), b.Name()
	result = ProfileChangeResult{Name: from, PreviousName: from, AppliedFields: []string{}}
	current = b
	if normalized.Name != nil {
		to = *normalized.Name
	}
	if err := CheckProfileChange(b.Root, from, normalized); err != nil {
		return current, result, err
	}
	if from == to && normalized.Description == nil && normalized.Timezone == nil && normalized.AvatarSeed == nil {
		return current, result, nil
	}
	commit := func() error {
		if err := CheckProfileChange(b.Root, from, normalized); err != nil {
			return err
		}
		b.profile.mu.Lock()
		defer b.profile.mu.Unlock()
		// The open lock file moves with its directory. Do not acquire it again
		// via Load/SaveConfig once the physical rename has happened.
		return fsutil.WithLock(filepath.Join(Dir(b.Root, from), "config.json.lock"), func() error {
			fresh, err := readProfileConfig(b.Root, from)
			if err != nil {
				return err
			}
			before := fresh
			if normalized.Description != nil {
				fresh.Description = *normalized.Description
			}
			if normalized.Timezone != nil {
				fresh.Timezone = *normalized.Timezone
			}
			if normalized.AvatarSeed != nil {
				fresh.Avatar, fresh.AvatarSeed = AvatarGenerated, *normalized.AvatarSeed
			}
			if from != to && fresh.AvatarSeed == "" {
				fresh.AvatarSeed = from
			}
			fresh.Name = to
			moved, written := false, false
			writeConfig := func(name string) error {
				raw, err := fsutil.Marshal(fresh)
				if err != nil {
					return err
				}
				return fsutil.WriteAtomic(filepath.Join(Dir(b.Root, name), "config.json"), raw, 0600)
			}
			rollback := func() error {
				if err := os.Rename(Dir(b.Root, to), Dir(b.Root, from)); err != nil {
					return err
				}
				moved = false
				var configErr error
				if written {
					fresh.Name = from
					configErr = writeConfig(from)
				}
				return errors.Join(configErr, moveVaultKey(b.Root, to, from))
			}
			perform := func() error {
				if from != to {
					closeStoreDir(Dir(b.Root, from))
					if err := moveVaultKey(b.Root, from, to); err != nil {
						return err
					}
					if err := os.Rename(Dir(b.Root, from), Dir(b.Root, to)); err != nil {
						return errors.Join(err, moveVaultKey(b.Root, to, from))
					}
					moved = true
				}
				if err := writeConfig(to); err != nil {
					if moved {
						return errors.Join(err, rollback())
					}
					return err
				}
				written = true
				if normalized.Description != nil {
					result.AppliedFields = append(result.AppliedFields, "description")
				}
				if normalized.Timezone != nil {
					result.AppliedFields = append(result.AppliedFields, "timezone")
				}
				if normalized.AvatarSeed != nil {
					result.AppliedFields = append(result.AppliedFields, "avatar_seed")
				}
				if !moved {
					return nil
				}
				if err := rewritePrimaryName(b.Root, from, to); err != nil {
					return errors.Join(err, rollback())
				}
				if err := moveWork(b.Root, from, to); err != nil {
					rollbackErr := rollback()
					var primaryErr error
					if !moved {
						primaryErr = rewritePrimaryName(b.Root, to, from)
					}
					return errors.Join(err, rollbackErr, primaryErr)
				}
				return nil
			}
			operationErr := perform()
			actual := from
			if moved {
				actual = to
				result.AppliedFields = append([]string{"name"}, result.AppliedFields...)
				migrateShellJobs(Dir(b.Root, from), Dir(b.Root, to))
			}
			result.Name, result.Moved = actual, moved
			// Reload without migration or relocking the same config inode. The
			// known physical location remains authoritative if reloading fails.
			var reloadErr error
			if written || moved {
				var loaded Config
				loaded, reloadErr = readProfileConfig(b.Root, actual)
				if reloadErr == nil {
					fresh = loaded
				} else if !written {
					fresh = before
				}
				fresh.Name = actual
				current = &Bot{Root: b.Root, cfg: fresh, profile: newProfileState(fresh)}
				if !moved {
					b.profile.set(fresh)
				}
			}
			if written && normalized.AvatarSeed != nil {
				_ = os.Remove(filepath.Join(Dir(b.Root, actual), "avatar"))
				_ = os.Remove(filepath.Join(Dir(b.Root, actual), "avatar.mime"))
			}
			return errors.Join(operationErr, reloadErr)
		})
	}
	if from != to {
		err = fsutil.WithLock(primaryLock(b.Root), commit)
	} else {
		err = commit()
	}
	return current, result, err
}

// RefreshProfileFrom refreshes display fields without reading a former path or
// changing the loaded Bot's name, provider, role, or execution-budget snapshot.
func (b *Bot) RefreshProfileFrom(current *Bot) {
	cfg := current.Config()
	b.profile.mu.Lock()
	b.profile.description, b.profile.timezone = cfg.Description, cfg.Timezone
	b.profile.avatar, b.profile.avatarSeed = cfg.Avatar, cfg.AvatarSeed
	b.profile.mu.Unlock()
}
