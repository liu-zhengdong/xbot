package web

import (
	"xbot/internal/bot"
	"xbot/internal/inbox"
	"xbot/internal/kernel"
	"xbot/internal/provider"
)

// withInboxIO borrows the original Resources across a wait or Handle. A saved
// Inbox pointer is only the legacy fallback; its path may predate a rename.
func withInboxIO(resources *kernel.BotResources, b *bot.Bot, box *inbox.Inbox, fn func(*bot.Bot, *inbox.Inbox) error) error {
	if resources != nil {
		return resources.WithCurrentIO(fn)
	}
	if box == nil {
		var err error
		box, err = inbox.Open(b.InboxPath())
		if err != nil {
			return err
		}
	}
	return fn(b, box)
}

// absorbedReply reports whether a running turn already took event id from the inbox.
// If so, it finds the row with that EventID: a row on a background branch
// returns that branch; a main-line row returns the main-line answer written after it,
// or failed when none was written.
func (s *Server) absorbedReply(b *bot.Bot, resources *kernel.BotResources, box *inbox.Inbox, id string) (absorbed bool, reply, background string, failed bool) {
	var entries []bot.Entry
	err := withInboxIO(resources, b, box, func(current *bot.Bot, currentBox *inbox.Inbox) error {
		if err := currentBox.Reload(); err != nil {
			return err
		}
		for _, ev := range currentBox.Pending() {
			if ev.ID == id {
				return nil
			}
		}
		absorbed = true
		n, err := current.Count()
		if err != nil || n == 0 {
			return err
		}
		entries, err = current.Slice(1, n)
		return err
	})
	if !absorbed {
		return false, "", "", false
	}
	if err != nil || len(entries) == 0 {
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
		if m.Role == provider.RoleUser && !rows[i].Entry.IsForkMark() && rows[i].Entry.EventID == id {
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
