package web

import (
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
