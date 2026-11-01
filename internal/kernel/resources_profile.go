package kernel

import (
	"errors"

	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/inbox"
)

// WithCurrentIO protects the complete short filesystem operation, not just its
// path lookup. Callers must not take executor/Jobs/host locks or publish here.
func (r *BotResources) WithCurrentIO(fn func(*bot.Bot, *inbox.Inbox) error) error {
	r.route.RLock()
	defer r.route.RUnlock()
	if r.closed || r.ctx.Err() != nil {
		return errResourcesUnavailable
	}
	return fn(r.current, r.box)
}

func (k *Kernel) withCurrentIO(fn func(*bot.Bot, *inbox.Inbox) error) error {
	if k.Resources != nil {
		return k.Resources.WithCurrentIO(fn)
	}
	return fn(k.Bot, k.Inbox)
}

// ProfileSnapshot reports the actual route after a commit, including partial
// commits. It reads only mutable ordinary profile metadata.
func (r *BotResources) ProfileSnapshot(result bot.ProfileChangeResult) (value cliops.Profile, err error) {
	err = r.WithCurrentIO(func(b *bot.Bot, _ *inbox.Inbox) error {
		if err := b.RefreshProfile(); err != nil {
			return err
		}
		value = profileSnapshot(b, result, r.Redact)
		return nil
	})
	return value, err
}

func profileSnapshot(b *bot.Bot, result bot.ProfileChangeResult, redact func(string) string) cliops.Profile {
	description, timezone := b.ProfileValues()
	avatar, seed := b.AvatarState()
	if redact != nil {
		description, seed = redact(description), redact(seed)
	}
	return cliops.Profile{Name: b.Name(), Description: description, Timezone: timezone, Avatar: avatar, AvatarSeed: seed, Scope: "runtime", Applied: result.Moved || len(result.AppliedFields) != 0, PreviousName: result.PreviousName, AppliedFields: result.AppliedFields, Moved: result.Moved}
}

// ApplyProfileChange receives the normalized boundary value. Publish is always
// outside the source operations mutex and all host/mover/path locks.
func (k *Kernel) ApplyProfileChange(change bot.ProfileChange) (cliops.Profile, error) {
	k.operations.mu.Lock()
	result, err := k.changeProfileLocked(change)
	value, snapshotErr := k.profileResult(result)
	k.operations.mu.Unlock()
	err = errors.Join(err, snapshotErr)
	if err != nil {
		value.Error = k.redactor()(err.Error())
		err = errors.New(value.Error)
	}
	if result.Moved || len(result.AppliedFields) != 0 {
		k.publishProfileNotice(ProfileNotice{Name: value.Name, PreviousName: result.PreviousName})
	}
	return value, err
}

func (k *Kernel) changeProfileLocked(change bot.ProfileChange) (result bot.ProfileChangeResult, err error) {
	if r := k.Resources; r != nil {
		name := r.Name()
		if change.Name != nil && *change.Name != name {
			if r.Rename != nil {
				return r.Rename(k, change)
			}
			return k.MoveResources(change)
		}
		return r.ApplyProfileChange(change)
	}
	var current *bot.Bot
	current, result, err = k.Bot.ApplyProfileChange(change)
	if current != nil {
		k.Bot = current
	}
	return result, err
}

func (k *Kernel) profileResult(result bot.ProfileChangeResult) (value cliops.Profile, err error) {
	if k.Resources != nil {
		return k.Resources.ProfileSnapshot(result)
	}
	err = k.Bot.RefreshProfile()
	value = profileSnapshot(k.Bot, result, k.redactor())
	return value, err
}

func (k *Kernel) profileAvatarOperation(data []byte, clear bool) (cliops.Profile, error) {
	k.operations.mu.Lock()
	var result bot.ProfileChangeResult
	err := k.withCurrentIO(func(b *bot.Bot, _ *inbox.Inbox) error {
		var err error
		result, err = b.ApplyAvatarChange(data, clear)
		return err
	})
	value, snapshotErr := k.profileResult(result)
	k.operations.mu.Unlock()
	err = errors.Join(err, snapshotErr)
	if err != nil {
		value.Error = k.redactor()(err.Error())
		err = errors.New(value.Error)
	}
	if len(result.AppliedFields) != 0 {
		k.publishProfileNotice(ProfileNotice{Name: value.Name})
	}
	return value, err
}

// ApplyAvatarChange is the owner Web entry into the source's independent avatar
// action. Upload/clear never become a combined name/profile request.
func (k *Kernel) ApplyAvatarChange(data []byte, clear bool) (cliops.Profile, error) {
	return k.profileAvatarOperation(data, clear)
}

// PublishProfile reports cold-resource changes through the existing live turn.
func (r *BotResources) PublishProfile(result bot.ProfileChangeResult) {
	if result.Moved || len(result.AppliedFields) != 0 {
		publishResourceProfile(r, ProfileNotice{Name: r.Name(), PreviousName: result.PreviousName})
	}
}
