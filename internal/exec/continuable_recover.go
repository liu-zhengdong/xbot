package exec

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"xbot/internal/bot"
	"xbot/internal/tools"
)

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
