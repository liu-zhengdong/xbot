package web

import (
	"strings"

	"xbot/internal/bot"
	"xbot/internal/peer"
	"xbot/internal/provider"
)

type completionCount struct{ cursor, through, count int }

// Count unread delivered results, using the existing owner's transcript cursor.
// An unchanged poll reads no transcript rows; an append only scans new rows.
func (s *Server) unreadCompletions(b *bot.Bot, cursor int) int {
	total, err := b.Count()
	if err != nil || cursor >= total {
		return 0
	}
	s.mu.Lock()
	cached := s.completed[b.Name()]
	s.mu.Unlock()
	if cached.cursor != cursor || cached.through > total {
		cached = completionCount{cursor: cursor, through: cursor}
	}
	if cached.through < cursor {
		cached.through = cursor
	}
	if cached.through == total {
		return cached.count
	}
	err = b.Walk(cached.through+1, total, func(_ int, e bot.Entry) error {
		// Delivered events are visible owner-origin rows; only model-only rows
		// are excluded here. EventSource below identifies subagent reports.
		if e.Message.Role != provider.RoleUser || e.Origin == bot.OriginInternal {
			return nil
		}
		source, _, ok := peer.EventSource(e.Message.Text())
		if !ok || source != "subagent" {
			return nil
		}
		header, _, _ := strings.Cut(e.Message.Text(), ">")
		if strings.Contains(header, `type="done"`) || strings.Contains(header, `type="failed"`) || strings.Contains(header, `type="fork_end"`) {
			cached.count++
		}
		return nil
	})
	if err != nil {
		return 0
	}
	cached.through = total
	s.mu.Lock()
	if s.completed == nil {
		s.completed = map[string]completionCount{}
	}
	s.completed[b.Name()] = cached
	s.mu.Unlock()
	return cached.count
}
