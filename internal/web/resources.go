package web

import (
	"context"
	"errors"
	"sync"
	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/inbox"
	"xbot/internal/kernel"
	"xbot/internal/peer"
)

type kernelEntry struct {
	Resources *kernel.BotResources
	Kernel    *kernel.Kernel
	Gate      *sync.Mutex
}

// entryLocked is the existing cache, promoted to retain resources across kernels.
func (s *Server) entryLocked(b *bot.Bot) (*kernelEntry, error) {
	if s.kernels == nil {
		s.kernels = map[string]*kernelEntry{}
		if !s.ForkTurns {
			kernel.SetWaker(s.Root, s.wakeBot)
		}
	}
	entry := s.kernels[b.Name()]
	if entry == nil {
		fresh, err := bot.Load(b.Root, b.Name())
		if err != nil {
			return nil, err
		}
		entry = &kernelEntry{Gate: &sync.Mutex{}}
		if s.NewResources != nil {
			entry.Resources, err = s.NewResources(fresh)
			if err != nil {
				return nil, err
			}
		}
		s.kernels[b.Name()] = entry
	}
	// Refuse a new Kernel before its recovery/assembly can write any files.
	if entry.Resources != nil && !entry.Resources.AdmissionOpen() {
		return nil, errors.New("bot resources paused or closed")
	}
	if entry.Gate == nil {
		entry.Gate = &sync.Mutex{}
	}
	return entry, nil
}

func (s *Server) ResourcesFor(b *bot.Bot) (*kernel.BotResources, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, err := s.entryLocked(b)
	if err != nil {
		return nil, err
	}
	return entry.Resources, nil
}

func (s *Server) cachedKernelLocked(name string) *kernel.Kernel {
	if entry := s.kernels[name]; entry != nil {
		return entry.Kernel
	}
	return nil
}

func (s *Server) refreshMCP() {
	if s.ReconcileMCP != nil {
		s.ReconcileMCP()
	}
}

// CloseResources snapshots the host index, then closes and joins without its
// lock. Resource callbacks remain usable until each resource's final fence.
func (s *Server) CloseResources() {
	s.mu.Lock()
	list := make([]*kernel.BotResources, 0, len(s.kernels))
	for _, entry := range s.kernels {
		if entry.Resources != nil {
			list = append(list, entry.Resources)
		}
	}
	s.mu.Unlock()
	for _, resources := range list {
		resources.Close()
	}
}

// RenameResources is injected into each resource. Source operations is held;
// the host name index encloses the complete mover and live-turn rekey tail.
func (s *Server) RenameResources(source *kernel.Kernel, change bot.ProfileChange) (bot.ProfileChangeResult, error) {
	s.mu.Lock()
	name := source.Bot.Name()
	if source.Resources != nil {
		name = source.Resources.Name()
	}
	entry := s.kernels[name]
	if entry == nil || entry.Resources != source.Resources {
		s.mu.Unlock()
		return bot.ProfileChangeResult{}, errors.New("bot rename: target changed")
	}
	result, retire, err := s.renameResourcesLocked(name, change, entry, source)
	s.mu.Unlock()
	if retire != nil {
		retire.RetireOperations()
	}
	return result, err
}

func (s *Server) RenameRuntimeBot(name, next string) error {
	change, err := bot.ValidateProfileChange(bot.ProfileChange{Name: &next})
	if err != nil {
		return err
	}
	_, err = s.applyProfileRuntime(name, change)
	return err
}

func (s *Server) renameResourcesLocked(name string, change bot.ProfileChange, entry *kernelEntry, source *kernel.Kernel) (result bot.ProfileChangeResult, retire *kernel.Kernel, err error) {
	next := name
	if change.Name != nil {
		next = *change.Name
	}
	if other := s.kernels[next]; next != name && other != nil && other != entry {
		return result, nil, bot.ErrExists
	}
	if entry != nil && entry.Resources != nil {
		if entry.Resources.Name() != name || !entry.Resources.AdmissionOpen() {
			return result, nil, errors.New("bot rename: target changed or paused")
		}
	}
	switch {
	case source != nil:
		result, err = source.MoveResources(change)
	case entry != nil && entry.Resources != nil:
		result, err = entry.Resources.ApplyProfileChange(change)
	default:
		var b *bot.Bot
		b, err = bot.Load(s.Root, name)
		if err == nil {
			_, result, err = b.ApplyProfileChange(change)
			if result.Moved {
				err = errors.Join(err, peer.RenameMember(s.Root, name, result.Name))
			}
		}
	}
	if !result.Moved {
		return result, nil, err
	}
	if entry != nil {
		delete(s.kernels, name)
		s.kernels[result.Name] = entry
		if entry.Kernel != nil && entry.Kernel != source {
			retire = entry.Kernel
			entry.Kernel = nil
		}
	}
	s.releaseNameHoldLocked(name)
	return result, retire, err
}

func (s *Server) applyProfileRuntime(name string, change bot.ProfileChange) (value cliops.Profile, err error) {
	s.mu.Lock()
	entry := s.kernels[name]
	if entry != nil && entry.Kernel != nil && entry.Resources != nil && entry.Resources.Rename != nil {
		source := entry.Kernel
		s.mu.Unlock()
		return source.ApplyProfileChange(change)
	}
	result, retire, err := s.renameResourcesLocked(name, change, entry, nil)
	s.mu.Unlock()
	if retire != nil {
		retire.RetireOperations()
	}
	if entry != nil && entry.Resources != nil {
		value, snapshotErr := entry.Resources.ProfileSnapshot(result)
		err = errors.Join(err, snapshotErr)
		if err != nil {
			value.Error = entry.Resources.Redact(err.Error())
		}
		entry.Resources.PublishProfile(result)
		return value, err
	}
	actual := result.Name
	if actual == "" {
		actual = name
	}
	value = cliops.Profile{Name: actual, Scope: "runtime", PreviousName: result.PreviousName, Moved: result.Moved, AppliedFields: result.AppliedFields, Applied: result.Moved || len(result.AppliedFields) > 0}
	b, snapshotErr := bot.Load(s.Root, actual)
	err = errors.Join(err, snapshotErr)
	if b != nil {
		value = webProfileSnapshot(b, result)
	}
	if err != nil {
		value.Error = bot.RedactText(err.Error())
	}
	return value, err
}

func (s *Server) applyAvatarRuntime(name string, data []byte, clear bool) (value cliops.Profile, err error) {
	s.mu.Lock()
	entry := s.kernels[name]
	s.mu.Unlock()
	if entry != nil && entry.Kernel != nil {
		return entry.Kernel.ApplyAvatarChange(data, clear)
	}
	var result bot.ProfileChangeResult
	if entry != nil && entry.Resources != nil {
		err = entry.Resources.WithCurrentIO(func(b *bot.Bot, _ *inbox.Inbox) error {
			var err error
			result, err = b.ApplyAvatarChange(data, clear)
			return err
		})
		value, snapshotErr := entry.Resources.ProfileSnapshot(result)
		err = errors.Join(err, snapshotErr)
		if err != nil {
			value.Error = entry.Resources.Redact(err.Error())
		}
		entry.Resources.PublishProfile(result)
		return value, err
	}
	b, err := bot.Load(s.Root, name)
	if err == nil {
		result, err = b.ApplyAvatarChange(data, clear)
		value = webProfileSnapshot(b, result)
	}
	if err != nil {
		value.Error = bot.RedactText(err.Error())
	}
	return value, err
}

func webProfileSnapshot(b *bot.Bot, result bot.ProfileChangeResult) cliops.Profile {
	description, timezone := b.ProfileValues()
	avatar, seed := b.AvatarState()
	return cliops.Profile{Name: b.Name(), Description: bot.RedactText(description), Timezone: timezone, Avatar: avatar, AvatarSeed: bot.RedactText(seed), Scope: "runtime", Applied: result.Moved || len(result.AppliedFields) != 0, PreviousName: result.PreviousName, AppliedFields: result.AppliedFields, Moved: result.Moved}
}

func (s *Server) releaseNameHoldLocked(name string) {
	if h := s.holds[name]; h != nil {
		delete(s.holds, name)
		_ = h.lock.Release()
	}
}

// DeleteRuntimeBot keeps all global/resource locks free during cancellation and
// the true owned-call join. Original completions and MCP notes can still arrive.
func (s *Server) DeleteRuntimeBot(ctx context.Context, name string) (removed bool, err error) {
	if err := bot.CheckDelete(s.Root, name); err != nil {
		return false, err
	}
	s.mu.Lock()
	entry := s.kernels[name]
	if entry == nil || entry.Resources == nil {
		removed, err := bot.DeleteWithResult(s.Root, name)
		var cached *kernel.Kernel
		if removed {
			if entry != nil {
				cached = entry.Kernel
			}
			delete(s.kernels, name)
			s.releaseNameHoldLocked(name)
		}
		s.mu.Unlock()
		if cached != nil {
			cached.CloseOperations()
			cached.CancelBackgrounds()
		}
		return removed, err
	}
	resources := entry.Resources
	if err := resources.BeginDelete(name); err != nil {
		s.mu.Unlock()
		return false, err
	}
	s.mu.Unlock()

	resources.Jobs.Shutdown()
	if err := ctx.Err(); err != nil {
		return false, errors.Join(err, resources.ResumeAdmission())
	}

	s.mu.Lock()
	if s.kernels[name] != entry || resources.Name() != name {
		s.mu.Unlock()
		return false, errors.Join(errors.New("bot delete: target changed"), resources.ResumeAdmission())
	}
	removed, err = resources.FinishDelete(name)
	cached := entry.Kernel
	if removed {
		delete(s.kernels, name)
		s.releaseNameHoldLocked(name)
	}
	s.mu.Unlock()
	if removed {
		resources.Close()
		if cached != nil {
			cached.CloseOperations()
			cached.CancelBackgrounds()
		}
	} else {
		err = errors.Join(err, resources.ResumeAdmission())
	}
	return removed, err
}
