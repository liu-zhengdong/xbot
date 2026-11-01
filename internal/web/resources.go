package web

import (
	"context"
	"errors"
	"strings"
	"sync"
	"xbot/internal/bot"
	"xbot/internal/kernel"
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

// RenameResources is injected into each resource. Source operations is already
// held; the host lock encloses the complete coding/Jobs/physical move.
func (s *Server) RenameResources(source *kernel.Kernel, next string) error {
	next = strings.TrimSpace(next)
	s.mu.Lock()
	name := source.Bot.Name()
	if source.Resources != nil {
		name = source.Resources.Name()
	}
	entry := s.kernels[name]
	if entry == nil || entry.Resources != source.Resources {
		s.mu.Unlock()
		return errors.New("bot rename: target changed")
	}
	retire, err := s.renameResourcesLocked(name, next, entry, source)
	s.mu.Unlock()
	if retire != nil {
		retire.RetireOperations()
	}
	return err
}

// RenameRuntimeBot chooses the cached source outside host locking, or moves only
// existing resources on a cold path. It never constructs a full Kernel to rename.
func (s *Server) RenameRuntimeBot(name, next string) error {
	next = strings.TrimSpace(next)
	s.mu.Lock()
	entry := s.kernels[name]
	if entry != nil && entry.Kernel != nil && entry.Resources != nil && entry.Resources.Rename != nil {
		source := entry.Kernel
		s.mu.Unlock()
		return source.RenameRuntime(next)
	}
	retire, err := s.renameResourcesLocked(name, next, entry, nil)
	s.mu.Unlock()
	if retire != nil {
		retire.RetireOperations()
	}
	return err
}

func (s *Server) renameResourcesLocked(name, next string, entry *kernelEntry, source *kernel.Kernel) (*kernel.Kernel, error) {
	if name == next {
		return nil, nil
	}
	if other := s.kernels[next]; other != nil && other != entry {
		return nil, bot.ErrExists
	}
	if entry != nil && entry.Resources != nil {
		if entry.Resources.Name() != name || !entry.Resources.AdmissionOpen() {
			return nil, errors.New("bot rename: target changed or paused")
		}
	}
	var err error
	moved := false
	switch {
	case source != nil:
		err = source.MoveResources(next)
		moved = source.Bot.Name() == next
	case entry != nil && entry.Resources != nil:
		err = entry.Resources.RenameTo(next)
		moved = entry.Resources.Name() == next
	default:
		err = kernel.RenameBot(s.Root, name, next)
		moved = err == nil
	}
	if !moved {
		return nil, err
	}
	var retire *kernel.Kernel
	if entry != nil {
		delete(s.kernels, name)
		s.kernels[next] = entry
		if entry.Kernel != nil && entry.Kernel != source {
			retire = entry.Kernel
			entry.Kernel = nil
		}
	}
	s.releaseNameHoldLocked(name)
	return retire, err
}

func (s *Server) releaseNameHoldLocked(name string) {
	if h := s.holds[name]; h != nil {
		delete(s.holds, name)
		_ = h.lock.Release()
	}
}

// DeleteRuntimeBot keeps all global/resource locks free during cancellation and
// the true owned-call join. Original completions and MCP notes can still arrive.
func (s *Server) DeleteRuntimeBot(ctx context.Context, name string) error {
	if err := bot.CheckDelete(s.Root, name); err != nil {
		return err
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
		return err
	}
	resources := entry.Resources
	if err := resources.BeginDelete(name); err != nil {
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()

	resources.Jobs.Shutdown()
	if err := ctx.Err(); err != nil {
		return errors.Join(err, resources.ResumeAdmission())
	}

	s.mu.Lock()
	if s.kernels[name] != entry || resources.Name() != name {
		s.mu.Unlock()
		return errors.Join(errors.New("bot delete: target changed"), resources.ResumeAdmission())
	}
	removed, err := resources.FinishDelete(name)
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
	return err
}
