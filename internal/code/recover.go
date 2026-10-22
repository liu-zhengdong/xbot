package code

import "xbot/internal/tools"

// Recover only preserves work proved live by this backend. Persisted running or
// queued records are not sufficient proof that starting the work again is safe.
func (l *Link) Recover() error {
	tasks, err := l.rec.List()
	if err != nil {
		return err
	}
	for _, task := range tasks {
		l.mu.Lock()
		alive := l.live[task.ID] != nil
		l.mu.Unlock()
		if alive || tools.SameProcess(task.PID, task.ProcStart) {
			continue
		}
		if task.Status == StatusRunning || task.Status == StatusQueued {
			err = l.rec.lock(func() error {
				cur, err := l.rec.read(task.ID)
				if err != nil {
					return err
				}
				if cur.Status != StatusRunning && cur.Status != StatusQueued {
					task = cur
					return nil
				}
				cur.Status = StatusInterrupted
				cur.Result = "任务因进程重启中断，没有自动重新执行。"
				cur.Updated = l.rec.now()
				task = cur
				return l.rec.write(cur)
			})
			if err != nil {
				return err
			}
		}
		if task.Status == StatusInterrupted {
			l.post(task)
		}
	}
	return nil
}
