package web

import (
	"context"
	"strings"
	"time"

	"xbot/internal/bot"
	"xbot/internal/fsutil"
	"xbot/internal/inbox"
	"xbot/internal/kernel"
	"xbot/internal/proc"
	"xbot/internal/provider"
)

// defaultHoldPoll is how often a held handle lock checks whether its
// backgrounds ended. A Server may override it through holdPoll.
const defaultHoldPoll = 200 * time.Millisecond

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
		// Release under s.mu so a new request cannot fall between the map and the flock.
		delete(s.holds, b.Name())
		_ = h.lock.Release()
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	go s.releaseWhenIdle(b.Name(), h, k)
}

// holdPoll is the interval this server polls at. It is a method rather than a
// package variable so a test's interval cannot race a leftover background from
// another test.
func (s *Server) holdPoll() time.Duration {
	if s != nil && s.PollInterval > 0 {
		return s.PollInterval
	}
	return defaultHoldPoll
}

func (s *Server) releaseWhenIdle(name string, h *handleHold, k *kernel.Kernel) {
	for {
		time.Sleep(s.holdPoll())
		s.mu.Lock()
		if h.users > 0 {
			// A new request took the claim; it releases when it is done.
			s.mu.Unlock()
			return
		}
		if s.holds[name] != h {
			// forgetKernel already dropped this hold and its lock.
			s.mu.Unlock()
			return
		}
		if len(k.Backgrounds()) == 0 {
			delete(s.holds, name)
			_ = h.lock.Release()
			s.mu.Unlock()
			return
		}
		// Backgrounds are still running, so the serve loop cannot take the lock: handle
		// timers, messages from other channels, and finished backgrounds here.
		h.users++
		gate := s.gate[name]
		s.mu.Unlock()
		if gate != nil && k.Inbox != nil {
			gate.Lock()
			_ = proc.Drain(context.Background(), k)
			gate.Unlock()
		}
		s.mu.Lock()
		h.users--
		s.mu.Unlock()
	}
}

// absorbedReply reports whether a running turn already took event id from the inbox.
// If so, it finds the row that turn recorded for text: a row on a background branch
// returns that branch; a main-line row returns the main-line answer written after it,
// or failed when none was written.
func (s *Server) absorbedReply(b *bot.Bot, box *inbox.Inbox, id, text string) (absorbed bool, reply, background string, failed bool) {
	if err := box.Reload(); err != nil {
		return false, "", "", false
	}
	for _, ev := range box.Pending() {
		if ev.ID == id {
			return false, "", "", false
		}
	}
	n, err := b.Count()
	if err != nil || n == 0 {
		return true, "", "", true
	}
	entries, err := b.Slice(1, n)
	if err != nil {
		return true, "", "", true
	}
	seqs := make([]bot.SeqEntry, len(entries))
	for i, e := range entries {
		seqs[i] = bot.SeqEntry{Seq: i + 1, Entry: e}
	}
	rows, _ := bot.Replay(seqs)
	at := -1
	for i := len(rows) - 1; i >= 0; i-- {
		m := rows[i].Entry.Message
		if m.Role == provider.RoleUser && !rows[i].Entry.IsForkMark() && strings.Contains(m.Text(), text) {
			at = i
			break
		}
	}
	if at < 0 {
		return true, "", "", true
	}
	if br := rows[at].Branch; br != "" {
		return true, "", br, false
	}
	for _, r := range rows[at+1:] {
		m := r.Entry.Message
		if r.Branch == "" && !r.Entry.IsForkMark() && m.Role == provider.RoleAssistant && len(m.Calls()) == 0 && m.Text() != "" {
			if bot.SilentText(m.Text()) {
				return true, "", "", false
			}
			return true, m.Text(), "", false
		}
	}
	return true, "", "", true
}
