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
	"xbot/internal/vault"
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
		j.Forms = k.shellForms
		j.Now = k.now
		j.OnDone = k.onShellDone
		if err := j.Load(); err == nil {
			k.shellMu.Lock()
			k.shell = j
			k.shellMu.Unlock()
		}
	})
	k.shellMu.Lock()
	defer k.shellMu.Unlock()
	return k.shell
}

// attachShellJobs points every shell tool on a mounted link at this bot's jobs.
// The pointer is written once per shell, so a running Shell.Run does not race
// with a later modelTools refresh.
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
				sh.BindJobs(jobs)
			}
		}
	}
}

// attachShell points every shell at this bot's job table and persistent cwd.
func (k *Kernel) attachShell() {
	k.attachShellJobs()
	if k == nil {
		return
	}
	for _, l := range k.snapshotLinks() {
		if l == nil {
			continue
		}
		for _, t := range l.Tools() {
			sh, ok := t.(*tools.Shell)
			if ok {
				sh.BindSession(k.shellStartDir, k.shellAfterDir)
				sh.SetMarkerDir(k.shellMarkerDir())
			}
			k.bindFileCwd(t)
		}
	}
}

// shellBackgroundOn reports that a real shell on these links has a job table.
// An eval jail is also named "tools" but its shell is not *tools.Shell and has
// no jobs, so it must not gain shell_output, shell_status, or shell_cancel.
func shellBackgroundOn(links []link.Link) bool {
	for _, l := range links {
		if l == nil {
			continue
		}
		for _, t := range l.Tools() {
			sh, ok := t.(*tools.Shell)
			if ok && sh.Jobs != nil {
				return true
			}
		}
	}
	return false
}

// bindFileCwd points file tools at the shell's current directory. A sandbox
// stays rooted where it was built. Relative paths resolve at call time;
// absolute paths stay absolute, and freshness records use that absolute path.
func (k *Kernel) bindFileCwd(t tools.Tool) {
	if k == nil || t == nil {
		return
	}
	switch f := t.(type) {
	case *tools.ReadFile:
		if f.Sandbox == "" {
			f.BindRoot(k.cwdFor)
		}
	case *tools.WriteFile:
		if f.Sandbox == "" {
			f.BindRoot(k.cwdFor)
		}
	case *tools.EditFile:
		if f.Sandbox == "" {
			f.BindRoot(k.cwdFor)
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
	if !k.claimShell(job.ID) {
		return
	}
	if err := k.postShellWake(job); err != nil {
		k.unclaimShell(job.ID)
		return
	}
	if jobs := k.liveShell(); jobs != nil {
		jobs.MarkNotified(job.ID)
	}
}

// shellForms lists every string the job redactor replaces: vault literals and
// their JSON, URL, and base64 forms, plus staged secret literals. The writer
// holds a suffix of pending output only when it is a prefix of one of these.
// Vault forms are cached until the vault generation changes.
func (k *Kernel) shellForms() []string {
	if k == nil {
		return nil
	}
	staged := k.stagedSecretValues()
	vGen := vaultFormGen(k)
	rGen := vault.RegisteredFormGen()
	var base []string
	k.shellMu.Lock()
	if k.formsOK && k.formsVGen == vGen && k.formsRGen == rGen {
		base = append([]string(nil), k.forms...)
		k.shellMu.Unlock()
	} else {
		k.shellMu.Unlock()
		base = k.collectVaultForms()
		k.shellMu.Lock()
		if k.formsVGen == vGen && k.formsRGen == rGen && k.formsOK {
			base = append([]string(nil), k.forms...)
		} else {
			k.formsOK = true
			k.forms = append([]string(nil), base...)
			k.formsVGen = vGen
			k.formsRGen = rGen
		}
		k.shellMu.Unlock()
	}
	if len(staged) == 0 {
		return base
	}
	seen := make(map[string]struct{}, len(base)+len(staged))
	for _, f := range base {
		seen[f] = struct{}{}
	}
	out := append([]string(nil), base...)
	for _, v := range staged {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func (k *Kernel) collectVaultForms() []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(list []string) {
		for _, f := range list {
			if f == "" {
				continue
			}
			if _, ok := seen[f]; ok {
				continue
			}
			seen[f] = struct{}{}
			out = append(out, f)
		}
	}
	if k != nil && k.Vault != nil {
		if src, ok := k.Vault.(interface{ Forms() []string }); ok && src != nil {
			add(src.Forms())
		}
	}
	add(vault.RegisteredForms())
	return out
}

func (k *Kernel) stagedSecretValues() []string {
	if k == nil {
		return nil
	}
	k.secretMu.Lock()
	defer k.secretMu.Unlock()
	var out []string
	seen := map[string]struct{}{}
	for _, m := range k.secrets {
		for _, value := range m {
			if value == "" {
				continue
			}
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			out = append(out, value)
		}
	}
	return out
}

func vaultFormGen(k *Kernel) uint64 {
	if k == nil || k.Vault == nil {
		return 0
	}
	type gen interface{ FormGen() uint64 }
	if g, ok := k.Vault.(gen); ok && g != nil {
		return g.FormGen()
	}
	return 0
}

// claimShell marks id as delivered for this process. The caller must not also
// put the same id on the reminder channel.
func (k *Kernel) claimShell(id string) bool {
	if id == "" {
		return false
	}
	k.shellMu.Lock()
	defer k.shellMu.Unlock()
	if k.shellClaimed == nil {
		k.shellClaimed = map[string]bool{}
	}
	if k.shellClaimed[id] {
		return false
	}
	k.shellClaimed[id] = true
	return true
}

func (k *Kernel) liveShell() *tools.Jobs {
	if k == nil {
		return nil
	}
	k.shellMu.Lock()
	defer k.shellMu.Unlock()
	return k.shell
}

func (k *Kernel) unclaimShell(id string) {
	if k == nil || id == "" {
		return
	}
	k.shellMu.Lock()
	defer k.shellMu.Unlock()
	delete(k.shellClaimed, id)
}

func (k *Kernel) postShellWake(job tools.Job) error {
	if k == nil || k.Inbox == nil || job.ID == "" {
		return nil
	}
	return k.Inbox.Post(link.Event{
		ID:        "shelljob_" + job.ID,
		Source:    "shell",
		Type:      "shell_job",
		Content:   "后台命令 " + job.ID + " 已结束（" + job.Status + "）。输出 " + job.Output + "。用 shell_output 读，不要紧循环轮询。",
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
	k.shellMu.Lock()
	jobs := k.shell
	k.shellMu.Unlock()
	if jobs == nil {
		return
	}
	// Only jobs this process finished are woken. Loaded history, including jobs
	// a previous serve already reported, is not a wake. The claim happens
	// before the post, under the same lock the reminder path uses, so the two
	// cannot both fire.
	var wake []tools.Job
	k.shellMu.Lock()
	if k.shellClaimed == nil {
		k.shellClaimed = map[string]bool{}
	}
	for _, job := range jobs.List() {
		if !job.Local || job.Notified || job.Status == "" || job.Status == "running" {
			continue
		}
		if k.shellClaimed[job.ID] {
			continue
		}
		k.shellClaimed[job.ID] = true
		wake = append(wake, job)
	}
	k.shellMu.Unlock()
	for _, job := range wake {
		if err := k.postShellWake(job); err != nil {
			k.unclaimShell(job.ID)
			continue
		}
		jobs.MarkNotified(job.ID)
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
	if k.shellClaimed == nil {
		k.shellClaimed = map[string]bool{}
	}
	var lines []string
	var mark []string
	for _, job := range jobs.List() {
		if job.ID == "" || job.Status == "" || job.Status == "running" {
			continue
		}
		if job.Notified || k.shellClaimed[job.ID] || strings.Contains(blob, job.ID) {
			if strings.Contains(blob, job.ID) && !job.Notified {
				k.shellClaimed[job.ID] = true
				mark = append(mark, job.ID)
			}
			continue
		}
		if len(lines) >= 8 {
			break
		}
		k.shellClaimed[job.ID] = true
		mark = append(mark, job.ID)
		lines = append(lines, shellJobLine(job))
	}
	k.shellMu.Unlock()
	for _, id := range mark {
		jobs.MarkNotified(id)
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
	return "Read a background shell job's output. offset is a byte position in the whole stream, not the retained file; the result names next_offset and says when earlier bytes were dropped. When bytes are still buffered because they might begin a secret, the result includes held_bytes. Pass tail to take the last N lines. Do not poll in a tight loop; wait for the completion reminder, then read."
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
	return "Report one background shell job, or list them when id is omitted. Status is running, done, cancelled, or lost. lost means this serve process restarted and is not tracking the process anymore. When bytes are still buffered because they might begin a secret, the result includes held_bytes."
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
			fmt.Fprintf(&b, "%s %s %s", job.ID, job.Status, clipRunes(job.Command, 80))
			if job.Cwd != "" {
				fmt.Fprintf(&b, " cwd %s", job.Cwd)
			}
			if job.Held > 0 {
				fmt.Fprintf(&b, " held_bytes: %d", job.Held)
			}
			b.WriteByte('\n')
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
	if job.Cwd != "" {
		line += "\ncwd: " + job.Cwd
	}
	if job.Held > 0 {
		line += fmt.Sprintf("\nheld_bytes: %d", job.Held)
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
