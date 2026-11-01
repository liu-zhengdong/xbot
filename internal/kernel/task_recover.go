package kernel

import (
	"errors"
	"os"
	"strings"

	"xbot/internal/bot"
	"xbot/internal/code"
	"xbot/internal/exec"
	"xbot/internal/inbox"
	"xbot/internal/link"
)

// RecoverTasks uses the existing owners' records. No task is rerun from an
// incomplete meta file, and repeated startup uses the same ordinary event id.
func (k *Kernel) RecoverTasks() error {
	k.recoverOnce.Do(func() { k.RecoverInterrupted(); k.recoverErr = k.recoverTasks() })
	return k.recoverErr
}

func (k *Kernel) recoverTasks() error {
	var dirs []os.DirEntry
	err := k.withCurrentIO(func(current *bot.Bot, _ *inbox.Inbox) error {
		var err error
		dirs, err = os.ReadDir(current.AgentsDir())
		return err
	})
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
				var dir string
				if err := k.withCurrentIO(func(current *bot.Bot, _ *inbox.Inbox) error {
					dir = current.AgentDir(id)
					_, err := exec.ReadMeta(dir)
					return err
				}); err != nil {
					continue
				}
				// Transport may wait on the runner; never hold route over it.
				if err := remote.Restore(id, dir); err != nil {
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
			// InterruptCurrent owns the owner/address policy before entering the
			// final short route callback. Wrapping Interrupt in route would invert
			// route -> owners, while a detached path could revive the old directory.
			m, err := exec.InterruptCurrent(id, func(fn func(string, string, string) error) error {
				return k.withCurrentIO(func(current *bot.Bot, _ *inbox.Inbox) error {
					return fn(current.Name(), current.AgentsDir(), k.currentWorkspaceFor(current))
				})
			})
			if err != nil {
				if m.Status != bot.ForkInterrupted {
					return err
				}
				// If the old directory is not writable, the original inbox still records
				// the interruption once via its stable EventID; do not block other work.
				m.Summary += "\n无法保存中断状态：" + err.Error()
			}
			if m.Status != bot.ForkInterrupted {
				continue
			}
			eventID := "ev_" + id + "_interrupted"
			if m.TurnSeq > 0 {
				eventID = exec.TerminalEventID(id, m.TurnSeq)
			}
			if err := k.withCurrentIO(func(_ *bot.Bot, box *inbox.Inbox) error {
				if box == nil {
					return nil
				}
				return box.Post(link.Event{ID: eventID, Source: link.SourceSubagent, Type: exec.EvFailed, Sender: id, Content: m.Summary, Time: m.FinishedAt, Untrusted: true})
			}); err != nil {
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
	k.flushToolJobs()
	return nil
}
