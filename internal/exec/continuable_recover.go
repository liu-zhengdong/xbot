package exec

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"xbot/internal/bot"
	"xbot/internal/tools"
)

// InterruptCurrent resolves an existing task under the same owner/address lock
// as MoveBot. withIO is the final short path fence and must not reenter exec.
// An accepted owner is observed after releasing that fence, never interrupted.
func InterruptCurrent(id string, withIO func(func(botName, agentsDir, workspace string) error) error) (m Meta, err error) {
	if !idRe.MatchString(id) {
		return m, fmt.Errorf("exec: bad id")
	}
	owners.Lock()
	defer owners.Unlock()
	var owner *Local
	err = withIO(func(_ string, agentsDir string, _ string) error {
		dir := filepath.Join(agentsDir, id)
		owner = owners.local[canonicalDir(dir)]
		if owner == nil {
			st, err := os.Lstat(dir)
			if err != nil {
				return err
			}
			if !st.IsDir() {
				return ErrNotFound
			}
		}
		var metaErr error
		m, metaErr = ReadMeta(dir)
		if metaErr != nil {
			m = Meta{ID: id, Mode: ModeBrief}
		}
		if owner != nil {
			return nil
		}
		if _, descriptorErr := ReadDescriptor(dir); descriptorErr == nil {
			m, err = interruptContinuableIO(dir, m, metaErr)
			return err
		}
		// Preserve Interrupt's legacy metadata and live-process behavior. This
		// branch performs local IO only and cannot take the owners lock again.
		if metaErr == nil && ((m.Status != statusRunning && m.Status != statusQueued) || tools.SameProcess(m.PID, m.ProcStart)) {
			return nil
		}
		m.Status, m.Blocked = bot.ForkInterrupted, false
		m.Summary = "任务因进程重启中断，没有自动重新执行。"
		if metaErr != nil {
			m.Summary = "任务记录不可读，无法确认原工作；已按中断处理，没有自动重新执行。"
		}
		m.FinishedAt = time.Now().UTC()
		return writeMeta(dir, m)
	})
	if err != nil || owner == nil {
		return m, err
	}
	// MoveBot/start/finish cannot replace the accepted owner while owners is
	// held. Local/storage/route locks are taken only after the first IO exits.
	owner.mu.Lock()
	defer owner.mu.Unlock()
	j, position := owner.findLocked(id)
	status := statusRunning
	if position > 0 {
		status = statusQueued
	}
	task := j.snapshot(status, position)
	m.ID, m.Task, m.Status = task.ID, task.Task, task.Status
	m.Mode, m.Type = j.mode, j.kind
	m.Continuable, m.TurnSeq, m.ResultPath, m.Blocked = task.Continuable, task.TurnSeq, task.ResultPath, task.Blocked
	return m, nil
}

func interruptContinuable(dir string, m Meta, metaErr error) (Meta, error) {
	if _, ok := ActiveTask(dir); ok {
		return m, nil
	}
	return interruptContinuableIO(dir, m, metaErr)
}

func interruptContinuableIO(dir string, m Meta, metaErr error) (Meta, error) {
	if metaErr == nil && ((m.Status != statusRunning && m.Status != statusQueued && m.Status != "stopping") || tools.SameProcess(m.PID, m.ProcStart)) {
		return m, nil
	}
	entries, err := readRecords(dir)
	if err != nil {
		return m, err
	}
	pending := make(map[int]bool)
	for _, e := range entries {
		if e.Kind == "message_received" {
			pending[e.Seq] = true
		}
		if e.Kind == "message_consumed" || e.Kind == "message_dropped" {
			delete(pending, e.InputSeq)
		}
		if metaErr != nil && e.TurnSeq > 0 {
			m.TurnSeq = e.TurnSeq
		}
	}
	if m.TurnSeq == 0 {
		for _, e := range entries {
			if e.Kind == "message_received" {
				m.TurnSeq = e.Seq
				break
			}
		}
	}
	m.Continuable = true
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e.Kind == "turn_end" && e.TurnSeq == m.TurnSeq {
			m.Status, m.FinishedAt = e.Status, e.Time
			if e.ResultPath != "" {
				m.ResultPath = filepath.Join(dir, "results", fmt.Sprintf("%d.txt", e.TurnSeq))
			}
			return m, writeMeta(dir, m)
		}
	}
	j := &job{id: m.ID, dir: dir, turnSeq: m.TurnSeq, recordSeq: len(entries)}
	if err := j.sealCallsLocked(); err != nil {
		return m, err
	}
	// Walk in original order for deterministic cancellation markers.
	for _, e := range entries {
		if pending[e.Seq] {
			if _, err := j.recordLocked(SeqEntry{Entry: bot.Entry{Origin: bot.OriginInternal}, Kind: "message_dropped", InputSeq: e.Seq, TurnSeq: j.turnSeq}); err != nil {
				return m, err
			}
		}
	}
	m.Status, m.Blocked = bot.ForkInterrupted, false
	m.Summary = "任务因进程重启中断，没有自动重新执行。"
	m.FinishedAt = time.Now().UTC()
	if m.TurnSeq > 0 {
		path, _, err := turnResult(dir, m.TurnSeq, m.Summary, "")
		if os.IsExist(err) {
			// A crash can leave the immutable result before its end marker.
			path = filepath.Join(dir, "results", fmt.Sprintf("%d.txt", m.TurnSeq))
			err = nil
		}
		if err != nil {
			return m, err
		}
		m.ResultPath = path
	}
	if _, err := j.recordLocked(SeqEntry{Entry: bot.Entry{Origin: bot.OriginInternal}, Kind: "turn_end", TurnSeq: m.TurnSeq, Status: m.Status, ResultPath: m.ResultPath}); err != nil {
		return m, err
	}
	return m, writeMeta(dir, m)
}
