package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/tools"
)

// shellJobs is the process-lifetime job set for this bot. The first call loads
// records left by a previous serve and marks the ones still running as lost.
func (k *Kernel) shellJobs() *tools.Jobs {
	if k == nil || k.Bot == nil || k.Bot.Root == "" || k.Bot.Name() == "" {
		return nil
	}
	k.shellOnce.Do(func() {
		dir := filepath.Join(bot.Dir(k.Bot.Root, k.Bot.Name()), "shell-jobs")
		j := tools.NewJobs(dir)
		j.Redact = func(s string) string {
			return scrubSecrets(k.redactor()(s), k.secretValues())
		}
		j.Now = k.now
		j.OnDone = k.onShellDone
		if err := j.Load(); err == nil {
			k.shell = j
		}
	})
	return k.shell
}

// attachShellJobs points every shell tool on a mounted link at this bot's jobs.
func (k *Kernel) attachShellJobs() {
	jobs := k.shellJobs()
	if jobs == nil {
		return
	}
	for _, l := range k.snapshotLinks() {
		if l == nil {
			continue
		}
		for _, t := range l.Tools() {
			sh, ok := t.(*tools.Shell)
			if ok {
				sh.Jobs = jobs
			}
		}
	}
}

func (k *Kernel) onShellDone(job tools.Job) {
	if job.Status == "" || job.Status == "running" {
		return
	}
	// A live turn hears about the job at its next boundary. Waking now would
	// be absorbed between steps only after the current tool returns, and the
	// reminder channel is the path that boundary already uses.
	if k.turnBusy() {
		return
	}
	k.postShellWake(job)
}

func (k *Kernel) postShellWake(job tools.Job) {
	if k == nil || k.Inbox == nil || job.ID == "" {
		return
	}
	_ = k.Inbox.Post(link.Event{
		ID:        "shelljob_" + job.ID,
		Source:    "shell",
		Type:      "shell_job",
		Content:   "后台命令 " + job.ID + " 已结束。看提醒里的输出路径，用 shell_output 读，不要紧循环轮询。",
		Sender:    "shell",
		Time:      k.now(),
		Untrusted: true,
	})
}

// flushShellWake posts an inbox event for a finished job the turn did not
// already put on the reminder channel. It does not start a background segment.
func (k *Kernel) flushShellWake() {
	if k == nil || k.turnBusy() || k.Inbox == nil {
		return
	}
	jobs := k.shell
	if jobs == nil {
		return
	}
	k.shellMu.Lock()
	claimed := k.shellClaimed
	k.shellMu.Unlock()
	for _, job := range jobs.List() {
		if job.Status == "" || job.Status == "running" {
			continue
		}
		if claimed[job.ID] {
			continue
		}
		k.postShellWake(job)
	}
}

func (k *Kernel) turnBusy() bool {
	if k == nil {
		return false
	}
	k.turnMu.Lock()
	t := k.turn
	k.turnMu.Unlock()
	if turnRunning(t) {
		return true
	}
	k.bgMu.Lock()
	defer k.bgMu.Unlock()
	for _, bg := range k.bgs {
		if turnRunning(bg) {
			return true
		}
	}
	return false
}

func turnRunning(t *turn) bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return !t.finished && !t.started.IsZero()
}

func (k *Kernel) shellTrigger() func(time.Time, []Reminder) string {
	return func(_ time.Time, prev []Reminder) string {
		return k.shellNotice(prev)
	}
}

func (k *Kernel) shellPrev() []Reminder {
	all, err := k.reminders().log.all()
	if err != nil {
		return nil
	}
	var prev []Reminder
	for _, n := range all {
		if n.Kind == KindShellJob {
			prev = append(prev, n)
		}
	}
	return prev
}

// shellNotice is the completion reminder. It claims each job it includes so a
// second boundary in the same turn does not send it again. Jobs already named
// in prev were sent by an earlier turn.
func (k *Kernel) shellNotice(prev []Reminder) string {
	jobs := k.shellJobs()
	if jobs == nil {
		return ""
	}
	var seen strings.Builder
	for _, p := range prev {
		seen.WriteString(p.Text)
		seen.WriteByte('\n')
	}
	blob := seen.String()
	k.shellMu.Lock()
	defer k.shellMu.Unlock()
	if k.shellClaimed == nil {
		k.shellClaimed = map[string]bool{}
	}
	var lines []string
	for _, job := range jobs.List() {
		if job.ID == "" || job.Status == "" || job.Status == "running" {
			continue
		}
		if k.shellClaimed[job.ID] || strings.Contains(blob, job.ID) {
			continue
		}
		if len(lines) >= 8 {
			break
		}
		k.shellClaimed[job.ID] = true
		lines = append(lines, shellJobLine(job))
	}
	if len(lines) == 0 {
		return ""
	}
	return "<shell_job>\n后台命令已结束。用 shell_output 看输出，不要紧循环轮询。\n" + strings.Join(lines, "\n") + "\n</shell_job>"
}

func shellJobLine(job tools.Job) string {
	extra := ""
	if job.Status == "done" {
		extra = fmt.Sprintf("，exit %d", job.ExitCode)
	}
	return "- " + job.ID + "：" + job.Status + extra + "，输出 " + job.Output
}

type shellOutputTool struct{ k *Kernel }

func (shellOutputTool) Name() string { return "shell_output" }
func (shellOutputTool) Description() string {
	return "Read a background shell job's output. Pass offset to continue from a byte position, or tail to take the last N lines. Do not poll in a tight loop; wait for the completion reminder, then read."
}
func (shellOutputTool) Parameters() any {
	return schema(map[string]any{
		"id":     stringProp("Background job id"),
		"offset": intProp("Byte offset to read from. Ignored when tail is set."),
		"tail":   intProp("Return the last N lines instead of a byte range."),
	}, []string{"id"})
}
func (shellOutputTool) Confirm(json.RawMessage) (bool, string, error) { return false, "", nil }
func (t shellOutputTool) Run(_ context.Context, args json.RawMessage) (string, error) {
	var in struct {
		ID     string `json:"id"`
		Offset int    `json:"offset"`
		Tail   int    `json:"tail"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	jobs := t.k.shellJobs()
	if jobs == nil {
		return "", errNoShellJobs
	}
	return jobs.Output(strings.TrimSpace(in.ID), in.Offset, in.Tail)
}

type shellStatusTool struct{ k *Kernel }

func (shellStatusTool) Name() string { return "shell_status" }
func (shellStatusTool) Description() string {
	return "Report one background shell job, or list them when id is omitted. Status is running, done, cancelled, or lost. lost means this serve process restarted and is not tracking the process anymore."
}
func (shellStatusTool) Parameters() any {
	return schema(map[string]any{
		"id": stringProp("Background job id. Omit to list jobs."),
	}, []string{})
}
func (shellStatusTool) Confirm(json.RawMessage) (bool, string, error) { return false, "", nil }
func (t shellStatusTool) Run(_ context.Context, args json.RawMessage) (string, error) {
	var in struct {
		ID string `json:"id"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &in); err != nil {
			return "", err
		}
	}
	jobs := t.k.shellJobs()
	if jobs == nil {
		return "", errNoShellJobs
	}
	id := strings.TrimSpace(in.ID)
	if id == "" {
		list := jobs.List()
		if len(list) == 0 {
			return "no background shell jobs", nil
		}
		var b strings.Builder
		for i, job := range list {
			if i >= 32 {
				b.WriteString("...\n")
				break
			}
			fmt.Fprintf(&b, "%s %s %s\n", job.ID, job.Status, clipRunes(job.Command, 80))
		}
		return strings.TrimRight(b.String(), "\n"), nil
	}
	job, err := jobs.Status(id)
	if err != nil {
		return "", err
	}
	line := fmt.Sprintf("%s %s", job.ID, job.Status)
	if job.Status == "done" {
		line += fmt.Sprintf(" exit %d", job.ExitCode)
	}
	if job.Output != "" {
		line += "\noutput: " + job.Output
	}
	return line, nil
}

type shellCancelTool struct{ k *Kernel }

func (shellCancelTool) Name() string { return "shell_cancel" }
func (shellCancelTool) Description() string {
	return "Stop a background shell job by killing its process group. Use this when the command should not keep running."
}
func (shellCancelTool) Parameters() any {
	return schema(map[string]any{
		"id": stringProp("Background job id"),
	}, []string{"id"})
}
func (shellCancelTool) Confirm(args json.RawMessage) (bool, string, error) {
	var in struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(args, &in)
	if strings.TrimSpace(in.ID) == "" {
		return false, "shell_cancel", nil
	}
	return false, "cancel " + strings.TrimSpace(in.ID), nil
}
func (t shellCancelTool) Run(_ context.Context, args json.RawMessage) (string, error) {
	var in struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	jobs := t.k.shellJobs()
	if jobs == nil {
		return "", errNoShellJobs
	}
	return jobs.Cancel(strings.TrimSpace(in.ID))
}

var errNoShellJobs = errShell("shell: background jobs are not available")

type errShell string

func (e errShell) Error() string { return string(e) }
