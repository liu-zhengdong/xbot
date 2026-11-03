package kernel

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"xbot/internal/bot"
	"xbot/internal/inbox"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

// forkMetaFile retains the metadata written by the retired fork-turn engine.
type forkMetaFile struct {
	PID       int       `json:"pid,omitempty"`
	ProcStart int64     `json:"proc_start,omitempty"`
	ID        string    `json:"id"`
	Mode      string    `json:"mode"`
	Status    string    `json:"status"`
	Task      string    `json:"task"`
	From      int       `json:"from"`
	Steps     int       `json:"steps"`
	Started   time.Time `json:"started"`
}

// RecoverInterrupted repairs legacy transcript branches once per resource lifetime.
// The caller holds the handle lock. This is best effort: it never reruns work.
func (k *Kernel) RecoverInterrupted() {
	if k == nil || k.Bot == nil {
		return
	}
	if k.Resources != nil {
		k.Resources.legacyForkOnce.Do(k.recoverInterrupted)
		return
	}
	k.legacyForkOnce.Do(k.recoverInterrupted)
}

func (k *Kernel) recoverInterrupted() {
	n, err := k.currentCount()
	if err != nil || n == 0 {
		return
	}
	entries, err := k.currentSlice(1, n)
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
		var meta forkMetaFile
		_ = k.withCurrentIO(func(b *bot.Bot, _ *inbox.Inbox) error { meta = readForkMeta(b.AgentDir(id)); return nil })
		if tools.SameProcess(meta.PID, meta.ProcStart) {
			continue
		}
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
		if _, err := k.appendCurrentForkEnd(id, bot.ForkInterrupted, steps); err != nil {
			continue
		}
		started := meta.Started
		if started.IsZero() {
			started = k.now()
		}
		k.writeInterruptedForkMeta(forkMetaFile{ID: id, Mode: "fork-turn", Status: bot.ForkInterrupted, Task: task, From: fromSeq[id], Steps: steps, Started: started})
		if k.Inbox == nil && k.Resources == nil {
			continue
		}
		text := fmt.Sprintf("后台任务 %s 因进程重启中断，做到第 %d 步。任务：%s。没有自动续跑，需要的话重新交代。", id, steps, task)
		_ = k.postCurrentEvent(inbox.Event{
			ID:        "ev_" + id + "_end",
			Source:    inbox.SourceSubagent,
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

// writeInterruptedForkMeta updates only the legacy task snapshot.
func (k *Kernel) writeInterruptedForkMeta(meta forkMetaFile) {
	raw, err := json.Marshal(meta)
	if err != nil {
		return
	}
	_ = k.withCurrentIO(func(b *bot.Bot, _ *inbox.Inbox) error {
		dir := b.AgentDir(meta.ID)
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, "meta.json"), raw, 0600)
	})
}
