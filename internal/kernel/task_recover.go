package kernel

import (
	"errors"
	"os"
	"strings"

	"xbot/internal/bot"
	"xbot/internal/code"
	"xbot/internal/exec"
	"xbot/internal/link"
)

// RecoverTasks uses the existing owners' records. No task is rerun from an
// incomplete meta file, and repeated startup uses the same ordinary event id.
func (k *Kernel) RecoverTasks() error {
	k.recoverOnce.Do(func() { k.RecoverInterrupted(); k.recoverErr = k.recoverTasks() })
	return k.recoverErr
}

func (k *Kernel) recoverTasks() error {
	dirs, err := os.ReadDir(k.Bot.AgentDir(""))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var ids []string
	for _, d := range dirs {
		if d.IsDir() && strings.HasPrefix(d.Name(), "sa_") {
			ids = append(ids, d.Name())
		}
	}
	if len(ids) > 0 {
		executor := k.execSnapshot()
		if remote, ok := k.execSnapshot().(*exec.Remote); ok {
			for _, id := range ids {
				// Unreadable metadata is handled by Interrupt below, not by the transport.
				if _, err := exec.ReadMeta(k.Bot.AgentDir(id)); err != nil {
					continue
				}
				if err := remote.Restore(id, k.Bot.AgentDir(id)); err != nil {
					return err
				}
			}
		}
		var tasks []exec.Task
		if executor != nil {
			tasks, err = executor.Tasks()
			if err != nil {
				return err
			}
		}
		live := map[string]bool{}
		for _, task := range tasks {
			live[task.ID] = true
		}
		for _, id := range ids {
			if live[id] {
				continue
			}
			m, err := exec.Interrupt(k.Bot.AgentDir(id))
			if err != nil {
				if m.Status != bot.ForkInterrupted {
					return err
				}
				// If the old directory is not writable, the original inbox still records
				// the interruption once via its stable EventID; do not block other work.
				m.Summary += "\n无法保存中断状态：" + err.Error()
			}
			if m.Status != bot.ForkInterrupted || k.Inbox == nil {
				continue
			}
			if err := k.Inbox.Post(link.Event{ID: "ev_" + id + "_interrupted", Source: link.SourceSubagent, Type: exec.EvFailed, Sender: id, Content: m.Summary, Time: m.FinishedAt, Untrusted: true}); err != nil {
				return err
			}
		}
	}
	for _, l := range k.snapshotLinks() {
		if coding, ok := l.(*code.Link); ok {
			if err := coding.Recover(); err != nil {
				return err
			}
		}
	}
	// Load and report lost shell jobs through their existing wake path.
	k.attachShellJobs()
	k.flushShellWake()
	return nil
}
