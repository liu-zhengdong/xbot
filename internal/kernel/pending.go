package kernel

import (
	"time"

	"xbot/internal/bot"
)

// openApprovals lists the confirmation cards still waiting for the owner. Cards
// live in the transcript, so the newest card for an id decides: a later card
// with the same id carries the answer. The list is oldest first.
func (k *Kernel) openApprovals() []PendingApproval {
	if k == nil || k.Bot == nil {
		return nil
	}
	entries, err := k.Bot.Entries()
	if err != nil {
		return nil
	}
	type open struct {
		card bot.ApprovalCard
		at   time.Time
	}
	seen := map[string]int{}
	var list []open
	for _, e := range entries {
		card, ok := bot.ParseApprovalCard(e.Message.Content)
		if !ok {
			continue
		}
		if i, dup := seen[card.ID]; dup {
			list[i] = open{card: card, at: e.Time}
			continue
		}
		seen[card.ID] = len(list)
		list = append(list, open{card: card, at: e.Time})
	}
	now := k.now()
	var out []PendingApproval
	for _, o := range list {
		if o.card.State != bot.ApprovalPending {
			continue
		}
		age := time.Duration(0)
		if !o.at.IsZero() {
			age = now.Sub(o.at)
		}
		out = append(out, PendingApproval{Tool: o.card.Tool, Summary: o.card.Title + " " + o.card.Summary, Age: age})
	}
	return out
}
