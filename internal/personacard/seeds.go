package personacard

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"xbot/internal/fsutil"
)

// Seed is a built-in card a new library starts with.
type Seed struct{ Name, Body string }

// Seeds are the cards a new library starts with, listed in this order.
// They are ordinary cards once written: people edit or delete them like any other.
var Seeds = []Seed{
	{"碎嘴群友", `说话像群里那个爱接梗的：短句、口语，别端着。

能玩梗就玩，但别硬凹；接不住就老实说「这个我接不上」。

正经事也能办，先给结论再补一句人话。

别碰：别替主人做决定；别编没发生过的群聊八卦；别用公文腔。`},
	{"值班运维", `你是值班那班：先报状态，再给下一步。

出事先止血，再说道理。日志和命令用短句甩出来。

别吓主人，也别报喜不报忧。`},
	{"周末搭子", `周末搭子气质：轻松、会起哄，约局和闲聊都行。

别太卷，别一开口就任务清单。能一起摸鱼也能一起办正事。`},
}

// seededFile marks a library that has had cards: a seed run finished, or a card was
// created, imported or updated. Only it and cards decide; .lock is also made by failed
// calls, so it says nothing about history.
const seededFile = ".seeded"

// beforeSeed lets tests pause a seed run before card i.
var beforeSeed func(i int)

func has(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}

// markSeeded records that the library has had cards. Callers hold the library lock.
func markSeeded(dir string) error {
	if has(dir, seededFile) {
		return nil
	}
	return fsutil.WriteAtomic(filepath.Join(dir, seededFile), nil, 0600)
}

// EnsureSeeds writes Seeds into a library that has never had a card, and returns once
// the library is complete, so a list read after it sees all seeds or none of this run.
// A marked library returns without touching the disk. Cards without a lock file were
// never part of a seed run (a run takes the lock before its first card), so they also
// return at once; otherwise the caller waits on the library lock and checks again,
// which also waits out a run in another process. A failed run removes its cards.
func EnsureSeeds(root, owner string) error {
	if !ownerRe.MatchString(owner) {
		return errors.New("personacard: bad owner")
	}
	dir := OwnerDir(root, owner)
	if has(dir, seededFile) {
		return nil
	}
	cards, err := list(dir)
	if err != nil {
		return err
	}
	if len(cards) > 0 && !has(dir, ".lock") {
		return nil
	}
	return locked(root, owner, func(dir string) error {
		if has(dir, seededFile) {
			return nil
		}
		if cards, err := list(dir); err != nil || len(cards) > 0 {
			return err
		}
		var ids []string
		err := func() error {
			// The list is newest first; step the times back so the seeds keep their order.
			now := time.Now()
			for i, s := range Seeds {
				if beforeSeed != nil {
					beforeSeed(i)
				}
				name, body, err := clean(s.Name, s.Body)
				if err != nil {
					return err
				}
				id := newID()
				ids = append(ids, id)
				if _, err := write(dir, id, name, body); err != nil {
					return err
				}
				t := now.Add(-time.Duration(i) * time.Second)
				_ = os.Chtimes(filepath.Join(dir, id, "card.md"), t, t)
			}
			return markSeeded(dir)
		}()
		if err != nil {
			for _, id := range ids {
				_ = os.RemoveAll(filepath.Join(dir, id))
			}
		}
		return err
	})
}
