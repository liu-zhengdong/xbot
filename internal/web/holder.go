package web

import (
	"time"

	"xbot/internal/bot"
	"xbot/internal/fsutil"
	"xbot/internal/inbox"
	"xbot/internal/kernel"
	"xbot/internal/provider"
)

// holdPoll is how often a held handle lock checks whether its backgrounds ended.
var holdPoll = 200 * time.Millisecond

// handleHold is this process's claim on one bot's handle lock. With fork turns on,
// the lock stays held while any request is running or any background turn is alive,
// and requests in this process share it instead of queueing behind it.
type handleHold struct {
	lock  *fsutil.FileLock
	users int
}

// acquireHandle claims the bot's handle lock for one request. shared is true when
// this process already held it, so the caller is a foreground beside a running turn.
// ok is false when another process holds the lock.
func (s *Server) acquireHandle(b *bot.Bot) (shared, ok bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.holds == nil {
		s.holds = map[string]*handleHold{}
	}
	if h := s.holds[b.Name()]; h != nil {
		h.users++
		return true, true, nil
	}
	lock, got, err := b.TryLockHandle()
	if err != nil || !got {
		return false, false, err
	}
	s.holds[b.Name()] = &handleHold{lock: lock, users: 1}
	return false, true, nil
}

// releaseHandle drops one request's claim. The lock is released once no request
// is left and the bot's kernel has no live background turn.
func (s *Server) releaseHandle(b *bot.Bot, k *kernel.Kernel) {
	s.mu.Lock()
	h := s.holds[b.Name()]
	if h == nil {
		s.mu.Unlock()
		return
	}
	h.users--
	if h.users > 0 {
		s.mu.Unlock()
		return
	}
	if k == nil || len(k.Backgrounds()) == 0 {
		delete(s.holds, b.Name())
		s.mu.Unlock()
		h.lock.Release()
		return
	}
	s.mu.Unlock()
	go s.releaseWhenIdle(b.Name(), h, k)
}

func (s *Server) releaseWhenIdle(name string, h *handleHold, k *kernel.Kernel) {
	for {
		time.Sleep(holdPoll)
		s.mu.Lock()
		if h.users > 0 {
			// A new request took the claim; it releases when it is done.
			s.mu.Unlock()
			return
		}
		if len(k.Backgrounds()) == 0 {
			if s.holds[name] == h {
				delete(s.holds, name)
			}
			s.mu.Unlock()
			h.lock.Release()
			return
		}
		s.mu.Unlock()
	}
}

// absorbedReply reports whether the running turn already took event id from the
// inbox, and if so the latest main-line reply, which answered it.
func (s *Server) absorbedReply(b *bot.Bot, box *inbox.Inbox, id string) (bool, string) {
	if err := box.Reload(); err != nil {
		return false, ""
	}
	for _, ev := range box.Pending() {
		if ev.ID == id {
			return false, ""
		}
	}
	entries, err := b.Entries()
	if err != nil {
		return true, ""
	}
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e.Branch == "" && !e.IsForkMark() && e.Message.Role == provider.RoleAssistant && len(e.Message.ToolCalls) == 0 && e.Message.Content != "" {
			if bot.SilentText(e.Message.Content) {
				return true, ""
			}
			return true, e.Message.Content
		}
	}
	return true, ""
}
