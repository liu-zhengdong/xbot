package exec

import (
	"time"

	"xbot/internal/bot"
	"xbot/internal/tools"
)

// Interrupt records a task that this executor can no longer prove is live.
// It never launches a second copy or guesses missing admission parameters.
func Interrupt(dir string) (Meta, error) {
	m, err := ReadMeta(dir)
	if err != nil {
		return m, err
	}
	if (m.Status != statusRunning && m.Status != statusQueued) || tools.SameProcess(m.PID, m.ProcStart) {
		return m, nil
	}
	m.Status = bot.ForkInterrupted
	m.Blocked = false
	m.Summary = "任务因进程重启中断，没有自动重新执行。"
	m.FinishedAt = time.Now().UTC()
	return m, writeMeta(dir, m)
}
