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

// seededFile marks a library that got the seeds, so deleting them all does not bring them back.
const seededFile = ".seeded"

// EnsureSeeds writes Seeds into a library that has never been written to.
// A library with the marker, any card, or a lock file (it was written before) is left alone
// without a single write. The check is repeated under the library lock, so concurrent
// callers seed once.
func EnsureSeeds(root, owner string) error {
	if !ownerRe.MatchString(owner) {
		return errors.New("personacard: bad owner")
	}
	dir := OwnerDir(root, owner)
	for _, f := range []string{seededFile, ".lock"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			return nil
		}
	}
	if cards, err := list(dir); err != nil || len(cards) > 0 {
		return err
	}
	return locked(root, owner, func(dir string) error {
		if _, err := os.Stat(filepath.Join(dir, seededFile)); err == nil {
			return nil
		}
		if cards, err := list(dir); err != nil || len(cards) > 0 {
			return err
		}
		// The list is newest first; step the times back so the seeds keep their order.
		now := time.Now()
		for i, s := range Seeds {
			name, body, err := clean(s.Name, s.Body)
			if err != nil {
				return err
			}
			c, err := write(dir, newID(), name, body)
			if err != nil {
				return err
			}
			t := now.Add(-time.Duration(i) * time.Second)
			_ = os.Chtimes(filepath.Join(dir, c.ID, "card.md"), t, t)
		}
		return fsutil.WriteAtomic(filepath.Join(dir, seededFile), nil, 0600)
	})
}
