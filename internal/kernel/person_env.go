package kernel

import (
	"context"
	"path/filepath"

	"xbot/internal/inbox"
	"xbot/internal/memory"
)

// personEvent reuses the authenticated Event identity; a timer carrying an
// owner's id is still a timer, not that person's new words.
func personEvent(ev inbox.Event) bool {
	return memory.ValidID(ev.Sender) && ev.SenderKind != inbox.KindBot && ev.SenderKind != inbox.KindGroup && (ev.SenderKind == inbox.KindPerson || ev.Source == inbox.NameTerminal || ev.Source == inbox.NameWeb || ev.Source == inbox.NameApproval || ev.Source == inbox.NameRemoteOwner)
}

func (k *Kernel) personText(ctx context.Context, ev inbox.Event) (string, error) {
	if k.Memory == nil || !personEvent(ev) {
		return "", nil
	}
	facts, err := k.recallMemory(ctx, memory.Query{Scope: memory.ScopePerson, PersonID: ev.Sender, Tier: memory.TierProfile})
	if err != nil {
		return "", err
	}
	paths := map[memory.Scope]string{memory.ScopePerson: filepath.Join(k.Bot.Root, "people", ev.Sender, "memory", "memory.md")}
	var personFacts []memory.Fact
	for _, fact := range facts {
		if fact.Scope == memory.ScopePerson && fact.PersonID == ev.Sender {
			personFacts = append(personFacts, fact)
		}
	}
	return clipMemory(renderMemoryPaths(memory.Resolve(memory.Visible(personFacts, k.currentName(), ev.Sender)), nil, paths)), nil
}
