package kernel

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/provider"
)

// forkMetaFile is the on-disk shape Kernel.writeForkMeta writes for a background
// turn. It is read back on recovery and rewritten with status interrupted.
type forkMetaFile struct {
	ID      string    `json:"id"`
	Mode    string    `json:"mode"`
	Status  string    `json:"status"`
	Task    string    `json:"task"`
	From    int       `json:"from"`
	Steps   int       `json:"steps"`
	Started time.Time `json:"started"`
}

// RecoverInterrupted closes background branches the transcript left open when
// the process died: a fork marker with no fork_end. It writes
// fork_end{interrupted}, marks meta.json interrupted, and posts one completion
// event per branch. It is idempotent: a branch that already has a fork_end is
// skipped, and the inbox ignores a repeated event id.
//
// It refuses to run on a kernel that still owns live backgrounds, so a running
// turn is never mistaken for a crash. Call it once per kernel lifetime, before
// repairTail, so a dangling branch tool call is repaired on that branch.
func (k *Kernel) RecoverInterrupted() {
	if k == nil || k.Bot == nil {
		return
	}
	k.bgMu.Lock()
	live := len(k.bgs)
	k.bgMu.Unlock()
	if k.AllowFork && live > 0 {
		return
	}
	n, err := k.Bot.Count()
	if err != nil || n == 0 {
		return
	}
	entries, err := k.Bot.Slice(1, n)
	if err != nil {
		return
	}
	seqs := make([]bot.SeqEntry, len(entries))
	for i := range entries {
		seqs[i] = bot.SeqEntry{Seq: i + 1, Entry: entries[i]}
	}
	rows, branches := bot.Replay(seqs)
	fromSeq := map[string]int{}
	for _, se := range seqs {
		if m := se.Entry.Mark; m != nil && m.Kind == bot.ForkKindStart {
			fromSeq[m.Branch] = m.From
		}
	}
	ids := make([]string, 0, len(branches))
	for id, st := range branches {
		if st.Open {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		k.bgMu.Lock()
		_, running := k.bgs[id]
		k.bgMu.Unlock()
		if running {
			continue
		}
		meta := readForkMeta(k.Bot.AgentDir(id))
		task := branches[id].Task
		if meta.Task != "" {
			task = meta.Task
		}
		// The transcript is the durable step record; meta.json is a snapshot
		// from fork time and only fills in when no branch row survives.
		steps := branchSteps(rows, id)
		if steps == 0 && meta.Steps > 0 {
			steps = meta.Steps
		}
		if _, err := k.Bot.AppendForkEnd(id, bot.ForkInterrupted, steps); err != nil {
			continue
		}
		started := meta.Started
		if started.IsZero() {
			started = k.now()
		}
		k.writeForkMeta(&turn{branch: id, task: task, fromSeq: fromSeq[id], replies: steps, started: started}, bot.ForkInterrupted)
		if k.Inbox == nil {
			continue
		}
		text := fmt.Sprintf("后台任务 %s 因进程重启中断，做到第 %d 步。任务：%s。没有自动续跑，需要的话重新交代。", id, steps, task)
		_ = k.Inbox.Post(link.Event{
			ID:        "ev_" + id + "_end",
			Source:    link.SourceSubagent,
			Type:      "fork_end",
			Content:   text,
			Sender:    id,
			Time:      k.now(),
			Untrusted: true,
		})
	}
}

// branchSteps counts assistant rows Replay assigns to branch: the branch's own
// model steps, including rows written before its fork marker.
func branchSteps(rows []bot.ReplayRow, branch string) int {
	n := 0
	for _, r := range rows {
		if r.Branch == branch && r.Entry.Message.Role == provider.RoleAssistant {
			n++
		}
	}
	return n
}

// readForkMeta returns the recorded metadata, or the zero value when it is
// missing or unreadable.
func readForkMeta(dir string) forkMetaFile {
	raw, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		return forkMetaFile{}
	}
	var m forkMetaFile
	if err := json.Unmarshal(raw, &m); err != nil {
		return forkMetaFile{}
	}
	return m
}
