package kernel

import (
	"xbot/internal/bot"
	"xbot/internal/inbox"
)

// ProfileNotice keeps the original response stream attached while its Bot name
// changes. It contains no tool result or avatar data.
type ProfileNotice struct {
	Name         string `json:"name"`
	PreviousName string `json:"previous_name,omitempty"`
}

// Called only after code/exec/Jobs have returned and released route/storage/
// owners. Interject can hold inter.mu while doing current-route IO.
func rekeyLiveTurn(root, previous, next string, r *BotResources) {
	oldKey, newKey := bot.Dir(root, previous), bot.Dir(root, next)
	if oldKey == newKey {
		return
	}
	entry, ok := liveTurns.Load(oldKey)
	if !ok {
		return
	}
	live := entry.(*liveTurn)
	if live.k.Resources != r {
		return
	}
	state := &live.t.inter
	state.mu.Lock()
	defer state.mu.Unlock()
	if !state.open || state.registration != live || state.registrationKey != oldKey {
		return
	}
	if !liveTurns.CompareAndDelete(oldKey, live) {
		return
	}
	state.registrationKey = newKey
	liveTurns.Store(newKey, live)
}

func (k *Kernel) publishProfileNotice(notice ProfileNotice) {
	if k.Resources != nil {
		publishResourceProfile(k.Resources, notice)
		return
	}
	if entry, ok := liveTurns.Load(bot.Dir(k.Bot.Root, notice.Name)); ok {
		live := entry.(*liveTurn)
		_ = live.k.Bot.RefreshProfile()
		if sink := live.t.sinkNow(); sink != nil && sink.OnProfile != nil {
			sink.OnProfile(notice)
		}
	}
}

func publishResourceProfile(r *BotResources, notice ProfileNotice) {
	var live *liveTurn
	_ = r.WithCurrentIO(func(current *bot.Bot, _ *inbox.Inbox) error {
		notice.Name = current.Name()
		if entry, ok := liveTurns.Load(bot.Dir(current.Root, current.Name())); ok {
			candidate := entry.(*liveTurn)
			if candidate.k.Resources == r {
				live = candidate
				if err := current.RefreshProfile(); err != nil {
					return err
				}
				live.k.Bot.RefreshProfileFrom(current)
			}
		}
		return nil
	})
	if live != nil {
		if sink := live.t.sinkNow(); sink != nil && sink.OnProfile != nil {
			sink.OnProfile(notice)
		}
	}
}
