package kernel

import (
	"sort"
	"sync"
	"time"

	"xbot/internal/bot"
)

type pendingCard struct {
	card bot.ApprovalCard
	at   time.Time
	seq  int
}

// approvals caches the live card projection, not the environment's published
// baseline. It is rebuilt from the append-only transcript after a restart.
// Incremental reads avoid rescanning all history before every model step.
type approvalState struct {
	mu      sync.Mutex
	through int
	open    map[string]pendingCard
}

func (k *Kernel) currentApprovals() ([]PendingApproval, error) {
	if k == nil || k.Bot == nil {
		return nil, nil
	}
	a := &k.approvals
	a.mu.Lock()
	defer a.mu.Unlock()
	n, err := k.Bot.Count()
	if err != nil {
		return nil, err
	}
	if a.open == nil || n < a.through {
		a.open = map[string]pendingCard{}
		a.through = 0
	}
	for a.through < n {
		end := min(n, a.through+64)
		entries, err := k.Bot.Slice(a.through+1, end)
		if err != nil {
			return nil, err
		}
		for i, e := range entries {
			card, ok := bot.ParseApprovalCard(e.Message.Text())
			if !ok {
				continue
			}
			if card.State != bot.ApprovalPending {
				delete(a.open, card.ID)
				continue
			}
			seq := a.through + 1 + i
			if old, ok := a.open[card.ID]; ok {
				seq = old.seq
			}
			a.open[card.ID] = pendingCard{card: card, at: e.Time, seq: seq}
		}
		a.through = end
	}
	list := make([]pendingCard, 0, len(a.open))
	for _, c := range a.open {
		list = append(list, c)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].seq < list[j].seq })
	var out []PendingApproval
	for _, c := range list {
		age := time.Duration(0)
		if !c.at.IsZero() {
			age = k.now().Sub(c.at)
		}
		out = append(out, PendingApproval{Tool: c.card.Tool, Summary: c.card.Title + " " + c.card.Summary, Age: age})
	}
	return out, nil
}

// openApprovals lists waiting cards for the existing reminder. The environment
// caller uses currentApprovals directly so an I/O failure cannot publish a clear.
func (k *Kernel) openApprovals() []PendingApproval {
	list, _ := k.currentApprovals()
	return list
}
