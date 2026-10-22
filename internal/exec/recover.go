package exec

import (
	"path/filepath"
	"time"

	"xbot/internal/bot"
	"xbot/internal/tools"
)

// Interrupt records a task that this executor can no longer prove is live.
// It never launches a second copy or guesses missing admission parameters.
func Interrupt(dir string) (Meta, error) {
	m, err := ReadMeta(dir)
	if err != nil {
		// The directory itself is an accepted task identity. Reuse its normal meta
		// and interrupted event instead of inventing launch parameters or rerunning.
		m = Meta{ID: filepath.Base(dir), Mode: ModeBrief}
	}
	if err == nil && ((m.Status != statusRunning && m.Status != statusQueued) || tools.SameProcess(m.PID, m.ProcStart)) {
		return m, nil
	}
	m.Status = bot.ForkInterrupted
	m.Blocked = false
	m.Summary = "任务因进程重启中断，没有自动重新执行。"
	if err != nil {
		m.Summary = "任务记录不可读，无法确认原工作；已按中断处理，没有自动重新执行。"
	}
	m.FinishedAt = time.Now().UTC()
	return m, writeMeta(dir, m)
}
