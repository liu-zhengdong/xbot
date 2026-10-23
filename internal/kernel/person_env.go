package kernel

import (
	"context"
	"path/filepath"

	"xbot/internal/link"
	"xbot/internal/memory"
)

// personEvent reuses the authenticated Event identity; a timer carrying an
// owner's id is still a timer, not that person's new words.
func personEvent(ev link.Event) bool {
	return memory.ValidID(ev.Sender) && ev.SenderKind != link.KindBot && ev.SenderKind != link.KindGroup && (ev.SenderKind == link.KindPerson || ev.Source == link.NameTerminal || ev.Source == link.NameWeb || ev.Source == link.NameApproval || ev.Source == link.NameRemoteOwner)
}

func (k *Kernel) personText(ctx context.Context, ev link.Event) (string, error) {
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
	return clipMemory(renderMemoryPaths(memory.Resolve(memory.Visible(personFacts, k.Bot.Name(), ev.Sender)), nil, paths)), nil
}
