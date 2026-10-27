package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"xbot/internal/bot"
	"xbot/internal/fsutil"
)

const (
	jobRunning = "running"
	jobReady   = "ready"
)

// compactJob is the crash-safe record of one background compaction.
// Status running has no summary yet. Status ready has a finished summary. The
// summary block is added only when a turn boundary applies the job. Compaction
// never writes memory.
type compactJob struct {
	From    int    `json:"from"`
	To      int    `json:"to"`
	Focus   string `json:"focus"`
	Status  string `json:"status"`
	Summary string `json:"summary,omitempty"`
}

func (k *Kernel) jobPath() string {
	return filepath.Join(bot.Dir(k.Bot.Root, k.Bot.Name()), "compact-job.json")
}

func (k *Kernel) jobLockPath() string {
	return filepath.Join(bot.Dir(k.Bot.Root, k.Bot.Name()), "compact-job.lock")
}

func (k *Kernel) runLockPath() string {
	return filepath.Join(bot.Dir(k.Bot.Root, k.Bot.Name()), "compact-run.lock")
}

// maybeStartBackground opens one job when the context crosses the soft target
// and the hard ceiling has not been hit (past the ceiling the turn compacts
// inline instead). It returns without waiting for the summary.
func (k *Kernel) maybeStartBackground(st bot.State, protectSeq int, mem string) {
	if k == nil || k.Bot == nil || k.Compactor == nil {
		return
	}
	k.startBackground(st, protectSeq, mem, k.target(), false)
}

func (k *Kernel) startBackground(st bot.State, protectSeq int, mem string, minimum int, idle bool) {
	if k == nil || k.Bot == nil || k.Compactor == nil {
		return
	}
	_, usage, err := k.assemble(st, mem)
	if err != nil {
		return
	}
	size := k.scaled(usage.Total())
	if !idle && size > k.ceiling() {
		return
	}
	unlock, err := k.lockJob(false)
	if err != nil {
		return
	}
	job, err := k.readJob()
	if err != nil {
		unlock()
		return
	}
	if job.Status == jobRunning || job.Status == jobReady {
		unlock()
		if job.Status == jobRunning {
			k.kickJob()
		}
		return
	}
	if size < minimum {
		unlock()
		return
	}
	budget := k.tailBudget()
	if idle {
		budget = min(budget, minimum/tailShare)
	}
	from, to, _, ok, err := k.compactPlanBudget(st, protectSeq, budget)
	if err != nil || !ok {
		unlock()
		return
	}
	job = compactJob{From: from, To: to, Focus: defaultFocus, Status: jobRunning}
	if err := k.writeJob(job); err != nil {
		unlock()
		return
	}
	unlock()
	k.kickJob()
}

// kickJob starts the worker unless this kernel already has one. A second
// process loses the run lock and exits, so a bot still has one worker.
func (k *Kernel) kickJob() {
	k.compactMu.Lock()
	if k.compactBusy {
		k.compactMu.Unlock()
		return
	}
	k.compactBusy = true
	k.compactMu.Unlock()
	go k.finishJob()
}

func (k *Kernel) finishJob() {
	defer func() {
		k.compactMu.Lock()
		k.compactBusy = false
		k.compactMu.Unlock()
	}()
	unlockRun, err := k.lockRun(true)
	if err != nil {
		return
	}
	defer unlockRun()

	unlock, err := k.lockJob(false)
	if err != nil {
		return
	}
	job, err := k.readJob()
	unlock()
	if err != nil || job.Status != jobRunning || job.To < job.From || job.From < 1 {
		return
	}
	span, err := k.Bot.Slice(job.From, job.To)
	if err != nil {
		return
	}
	st, err := k.Bot.LoadState()
	if err != nil {
		return
	}
	st = migrate(st)
	// The saved range is the snapshot. Messages appended after it stay outside.
	text, err := k.summarize(context.Background(), span, st.Blocks, job.From, job.To, job.Focus)
	if err != nil {
		return
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	unlock, err = k.lockJob(false)
	if err != nil {
		return
	}
	defer unlock()
	cur, err := k.readJob()
	if err != nil || cur.Status != jobRunning || cur.From != job.From || cur.To != job.To {
		return
	}
	cur.Summary = text
	cur.Status = jobReady
	_ = k.writeJob(cur)
}

// applyReadyJob inserts a finished summary block if one is waiting.
// It is safe to call more than once: a block for the same range is not added again.
func (k *Kernel) applyReadyJob(st bot.State) (bot.State, bool, error) {
	if k == nil || k.Bot == nil {
		return st, false, nil
	}
	unlock, err := k.lockJob(false)
	if err != nil {
		return st, false, err
	}
	defer unlock()
	job, err := k.readJob()
	if err != nil || job.Status != jobReady || strings.TrimSpace(job.Summary) == "" {
		return st, false, err
	}
	fresh, err := k.Bot.LoadState()
	if err != nil {
		return st, false, err
	}
	fresh = migrate(fresh)
	st.Usage = fresh.Usage
	st.Blocks = fresh.Blocks
	changed := false
	if !hasSameRange(st.Blocks, job.From, job.To) {
		if coveredBy(st.Blocks, job.From, job.To) {
			_ = os.Remove(k.jobPath())
			return st, false, nil
		}
		n, err := k.Bot.Count()
		if err != nil {
			return st, false, err
		}
		span, err := k.Bot.Slice(job.From, job.To)
		if err != nil {
			return st, false, err
		}
		if err := validateRange(span, job.From, job.From, job.To, 0, n, st.Blocks); err != nil {
			_ = os.Remove(k.jobPath())
			return st, false, nil
		}
		st.Blocks = addBlock(st.Blocks, bot.SummaryBlock{
			From: job.From, To: job.To, Text: job.Summary, Focus: "background", At: k.now(),
		})
		if err := k.refreshMemSnap(context.Background(), &st); err != nil {
			return st, false, err
		}
		if err := k.Bot.SaveState(st); err != nil {
			return st, false, err
		}
		changed = true
		if err := k.ensureSnapshot(st); err != nil {
			return st, changed, err
		}
	}
	_ = os.Remove(k.jobPath())
	return st, changed, nil
}

func hasSameRange(blocks []bot.SummaryBlock, from, to int) bool {
	for _, b := range blocks {
		if b.From == from && b.To == to && strings.TrimSpace(b.Text) != "" {
			return true
		}
	}
	return false
}

func (k *Kernel) readJob() (compactJob, error) {
	raw, err := os.ReadFile(k.jobPath())
	if errors.Is(err, os.ErrNotExist) {
		return compactJob{}, nil
	}
	if err != nil {
		return compactJob{}, err
	}
	var job compactJob
	if err := json.Unmarshal(raw, &job); err != nil {
		return compactJob{}, err
	}
	return job, nil
}

func (k *Kernel) writeJob(job compactJob) error {
	raw, err := fsutil.Marshal(job)
	if err != nil {
		return err
	}
	return fsutil.WriteAtomic(k.jobPath(), raw, 0600)
}

func (k *Kernel) lockJob(nonblock bool) (func(), error) {
	return lockPath(k.jobLockPath(), nonblock)
}

func (k *Kernel) lockRun(nonblock bool) (func(), error) {
	return lockPath(k.runLockPath(), nonblock)
}

func lockPath(path string, nonblock bool) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := fsutil.LockFile(f, true, !nonblock); err != nil {
		_ = f.Close()
		return nil, err
	}
	return func() {
		_ = fsutil.UnlockFile(f)
		_ = f.Close()
	}, nil
}
