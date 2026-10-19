package kernel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"xbot/internal/agent"
	"xbot/internal/bot"
	"xbot/internal/clip"
	"xbot/internal/code"
	"xbot/internal/fsutil"
	"xbot/internal/link"
	"xbot/internal/provider"
)

// Reminder kinds wired now. File changes fire from the freshness scan.
// Todo fires from todoNote when the active list is stale. Timer wakes skip it.
const (
	KindPendingApproval = "pending_approval"
	KindLoop            = "loop"
	KindBackgroundDone  = "background_done"
	KindTodo            = "todo"
	KindFileChange      = "file_change"
	KindShellJob        = "shell_job"

	pendingCooldown = 10 * time.Minute
	loopCooldown    = time.Minute
	// reminderMax clips an ordinary reminder. Project instructions carry their
	// own larger budget; reminderHardMax is the absolute cap prepare enforces.
	reminderMax     = 2048
	reminderHardMax = projectNoteMax
)

// Kind is one reminder. Cooldown is the minimum gap between firings.
// Trigger returns the note, or "" when the condition is not met. prev is this
// kind's earlier firings, oldest first, so a trigger can suppress repeats
// without rewriting them. MaxBytes overrides reminderMax for one kind.
type Kind struct {
	Name     string
	Cooldown time.Duration
	MaxBytes int
	Trigger  func(now time.Time, prev []Reminder) string
}

// Reminder is one firing. Seq is the transcript line it was attached to.
// The transcript body is not modified; replay appends Text onto that line
// in the request only.
type Reminder struct {
	Seq  int       `json:"seq"`
	Kind string    `json:"kind"`
	Text string    `json:"text"`
	At   time.Time `json:"at"`
}

// Channel decides which reminders are due and remembers what it already sent.
type Channel struct {
	log   *reminderLog
	kinds []Kind
}

func newChannel(path string) *Channel {
	return &Channel{log: &reminderLog{path: path}}
}

// Register adds or replaces a kind. File-change uses the same path; the
// channel does not special-case names.
func (c *Channel) Register(k Kind) {
	if c == nil || k.Name == "" {
		return
	}
	for i, old := range c.kinds {
		if old.Name == k.Name {
			c.kinds[i] = k
			return
		}
	}
	c.kinds = append(c.kinds, k)
}

// Due returns reminders whose trigger fired and whose cooldown has elapsed.
func (c *Channel) Due(now time.Time) ([]Reminder, error) {
	if c == nil {
		return nil, nil
	}
	all, err := c.log.all()
	if err != nil {
		return nil, err
	}
	var out []Reminder
	for _, k := range c.kinds {
		if k.Trigger == nil {
			continue
		}
		var prev []Reminder
		var last time.Time
		for _, n := range all {
			if n.Kind != k.Name {
				continue
			}
			prev = append(prev, n)
			if n.At.After(last) {
				last = n.At
			}
		}
		if k.Cooldown > 0 && !last.IsZero() && now.Sub(last) < k.Cooldown {
			continue
		}
		limit := k.MaxBytes
		if limit <= 0 {
			limit = reminderMax
		}
		if limit > reminderHardMax {
			limit = reminderHardMax
		}
		text := clipLimit(k.Trigger(now, prev), limit)
		if text == "" {
			continue
		}
		out = append(out, Reminder{Kind: k.Name, Text: text, At: now})
	}
	return out, nil
}

// Bind appends due reminders to the newest message and records them against seq
// so the next request can replay the same bytes. Earlier messages are not touched.
// A repo-provided project note is its own trailing user message, so it is not
// concatenated into the owner's trusted line.
func (c *Channel) Bind(msgs []provider.Message, seq int, now time.Time) ([]provider.Message, error) {
	if c == nil {
		return msgs, nil
	}
	due, err := c.Due(now)
	if err != nil || len(due) == 0 {
		return msgs, err
	}
	var trusted, apart []string
	for i, d := range due {
		due[i].Text = c.log.prepare(d.Text)
		if due[i].Text == "" {
			continue
		}
		if projectApart(due[i].Text) {
			apart = append(apart, due[i].Text)
			continue
		}
		trusted = append(trusted, due[i].Text)
	}
	if len(trusted) == 0 && len(apart) == 0 {
		return msgs, nil
	}
	if len(trusted) > 0 {
		msgs = agent.AttachReminder(msgs, strings.Join(trusted, "\n\n"))
	}
	for _, text := range apart {
		msgs = append(msgs, provider.Message{Role: provider.RoleUser, Content: text})
	}
	if seq <= 0 {
		return msgs, nil
	}
	for _, d := range due {
		if d.Text == "" {
			continue
		}
		if err := c.log.Append(seq, d.Kind, d.Text, now); err != nil {
			return msgs, err
		}
	}
	return msgs, nil
}

// Record stores a reminder the breaker emitted directly. Cooldown is not
// consulted: the caller already decided this firing should happen.
func (c *Channel) Record(seq int, kind, text string, now time.Time) error {
	if c == nil {
		return nil
	}
	return c.log.Append(seq, kind, text, now)
}

// Apply joins stored reminders onto msgs[i] when seqs[i] is the line they were
// bound to. It does not move a reminder onto a later message.
func (c *Channel) Apply(msgs []provider.Message, seqs []int) error {
	if c == nil || len(msgs) == 0 || len(seqs) == 0 {
		return nil
	}
	all, err := c.log.all()
	if err != nil || len(all) == 0 {
		return err
	}
	by := map[int][]string{}
	for _, n := range all {
		if n.Seq > 0 && n.Text != "" {
			by[n.Seq] = append(by[n.Seq], n.Text)
		}
	}
	for i := range msgs {
		if i >= len(seqs) {
			break
		}
		texts := by[seqs[i]]
		if len(texts) == 0 {
			continue
		}
		msgs[i].Content = joinReminder(msgs[i].Content, strings.Join(texts, "\n\n"))
	}
	return nil
}

// Replay appends stored reminders onto the in-memory copies of the lines they
// belong to. It does not write the transcript. seqs[i] is the sequence of entries[i].
// A repo-provided project note bound to a user line becomes the next message
// instead of joining that line, so a later wake does not put it back inside
// the owner's trusted text. The returned slice may be longer than entries, so
// the returned sequence numbers let a caller keep the lines in transcript order:
// a split-out note carries the sequence of the line it was bound to.
func (c *Channel) Replay(entries []bot.Entry, seqs []int) ([]bot.Entry, []int, error) {
	if c == nil || len(entries) == 0 {
		return entries, seqs, nil
	}
	all, err := c.log.all()
	if err != nil || len(all) == 0 {
		return entries, seqs, err
	}
	by := map[int][]string{}
	for _, n := range all {
		if n.Seq > 0 && n.Text != "" {
			by[n.Seq] = append(by[n.Seq], n.Text)
		}
	}
	if len(by) == 0 {
		return entries, seqs, nil
	}
	out := make([]bot.Entry, 0, len(entries))
	outSeqs := make([]int, 0, len(seqs))
	for i := range entries {
		e := entries[i]
		if i < len(seqs) {
			var join, apart []string
			for _, text := range by[seqs[i]] {
				if projectApart(text) && e.Message.Role == provider.RoleUser {
					apart = append(apart, text)
					continue
				}
				join = append(join, text)
			}
			if len(join) > 0 {
				e.Message.Content = joinReminder(e.Message.Content, strings.Join(join, "\n\n"))
			}
			out = append(out, e)
			outSeqs = append(outSeqs, seqs[i])
			for _, text := range apart {
				out = append(out, bot.Entry{
					Time:    e.Time,
					Message: provider.Message{Role: provider.RoleUser, Content: text},
				})
				outSeqs = append(outSeqs, seqs[i])
			}
			continue
		}
		out = append(out, e)
	}
	return out, outSeqs, nil
}

func joinReminder(body, note string) string {
	if note == "" {
		return body
	}
	if body == "" {
		return note
	}
	return body + "\n\n" + note
}

func clipReminder(s string) string {
	return clipLimit(s, reminderMax)
}

func clipLimit(s string, max int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	out, _ := clip.Bytes(s, max)
	return out
}

// reminderKeep is the point at which notes older than the live transcript
// window are dropped. The newest note of each kind is kept for cooldown.
const reminderKeep = 48

// reminderLog is the sidecar. An empty path keeps notes in memory for tests.
// The transcript body is never rewritten. Notes stay cached while the file's
// size and mtime match the last read, so a second process appending or
// rewriting the file is picked up on the next lookup. Compaction rewrites the
// file when the live window no longer needs the older lines.
type reminderLog struct {
	mu     sync.Mutex
	path   string
	notes  []Reminder
	loaded bool
	size   int64
	mtime  time.Time
	redact func(string) string
}

func (l *reminderLog) setRedact(fn func(string) string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.redact = fn
	l.mu.Unlock()
}

func (l *reminderLog) prepare(text string) string {
	if l == nil {
		return clipReminder(text)
	}
	l.mu.Lock()
	fn := l.redact
	l.mu.Unlock()
	if fn != nil {
		text = fn(text)
	}
	return clipLimit(text, reminderHardMax)
}

func (l *reminderLog) all() ([]Reminder, error) {
	if l == nil {
		return nil, nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.load(); err != nil {
		return nil, err
	}
	return append([]Reminder(nil), l.notes...), nil
}

// load reads the sidecar when it has not been read, or when size or mtime
// changed since the last read. The caller holds l.mu. Stat and the read share
// one shared file lock so a concurrent append cannot land between them.
func (l *reminderLog) load() error {
	if l.path == "" {
		l.loaded = true
		return nil
	}
	f, err := os.Open(l.path)
	if os.IsNotExist(err) {
		l.notes = nil
		l.loaded = true
		l.size = 0
		l.mtime = time.Time{}
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	if err := fsutil.LockFile(f, false, true); err != nil {
		return err
	}
	defer fsutil.UnlockFile(f)
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if l.loaded && st.Size() == l.size && st.ModTime().Equal(l.mtime) {
		return nil
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	notes, err := parseReminderNotes(l.path, data)
	if err != nil {
		return err
	}
	l.notes = notes
	l.size = st.Size()
	l.mtime = st.ModTime()
	l.loaded = true
	return nil
}

func parseReminderNotes(path string, data []byte) ([]Reminder, error) {
	if len(data) == 0 {
		return nil, nil
	}
	if data[len(data)-1] != '\n' {
		if i := bytes.LastIndexByte(data, '\n'); i < 0 {
			return nil, nil
		} else {
			data = data[:i+1]
		}
	}
	lines := bytes.Split(data, []byte("\n"))
	var notes []Reminder
	for i, line := range lines {
		if len(line) == 0 {
			continue
		}
		var n Reminder
		if err := json.Unmarshal(line, &n); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, i+1, err)
		}
		notes = append(notes, n)
	}
	return notes, nil
}

func (l *reminderLog) Append(seq int, kind, text string, at time.Time) error {
	if l == nil {
		return nil
	}
	text = l.prepare(text)
	if text == "" || kind == "" {
		return nil
	}
	n := Reminder{Seq: seq, Kind: kind, Text: text, At: at}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.load(); err != nil {
		return err
	}
	l.notes = append(l.notes, n)
	if l.path == "" {
		return nil
	}
	if err := fsutil.AppendJSONL(l.path, n); err != nil {
		return err
	}
	// Another writer may have appended between our cached read and this write.
	// Drop the cache and read the file back under its lock.
	l.loaded = false
	return l.load()
}

// Compact drops reminders that belong to transcript lines the next request
// will not send. The newest note of each kind stays, so cooldown and dedupe
// still see it. Below reminderKeep the file is left alone.
func (c *Channel) Compact(minSeq int) error {
	if c == nil || c.log == nil {
		return nil
	}
	return c.log.compact(minSeq)
}

func (l *reminderLog) compact(minSeq int) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.load(); err != nil {
		return err
	}
	if len(l.notes) <= reminderKeep {
		return nil
	}
	next := trimReminders(l.notes, minSeq)
	if len(next) == len(l.notes) {
		return nil
	}
	l.notes = next
	if l.path == "" {
		return nil
	}
	return l.rewrite()
}

func trimReminders(notes []Reminder, minSeq int) []Reminder {
	latest := map[string]int{}
	for i, n := range notes {
		j, ok := latest[n.Kind]
		if !ok || notes[j].At.Before(n.At) {
			latest[n.Kind] = i
		}
	}
	var out []Reminder
	for i, n := range notes {
		if minSeq > 0 && n.Seq < minSeq && latest[n.Kind] != i {
			continue
		}
		out = append(out, n)
	}
	return out
}

func (l *reminderLog) rewrite() error {
	tmp := l.path + ".next"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	for _, n := range l.notes {
		line, err := json.Marshal(n)
		if err != nil {
			f.Close()
			os.Remove(tmp)
			return err
		}
		if _, err := f.Write(append(line, '\n')); err != nil {
			f.Close()
			os.Remove(tmp)
			return err
		}
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, l.path); err != nil {
		return err
	}
	l.loaded = false
	return l.load()
}

func (k *Kernel) reminderPath() string {
	if k == nil || k.Bot == nil {
		return ""
	}
	return filepath.Join(bot.Dir(k.Bot.Root, k.Bot.Name()), "reminders.jsonl")
}

func (k *Kernel) reminders() *Channel {
	k.loadMu.Lock()
	defer k.loadMu.Unlock()
	if k.channel == nil {
		k.channel = newChannel(k.reminderPath())
	}
	k.channel.log.setRedact(k.redactor())
	return k.channel
}

// reminderKinds registers the live kinds for this wake. File changes fire
// from the freshness scan. Todo fires from todoNote when the list is stale.
func (k *Kernel) reminderKinds(ev link.Event) *Channel {
	return k.reminderKindsFor(ev, nil)
}

// reminderKindsFor is reminderKinds for turn t, whose context decides whether
// a project instruction set was already sent.
func (k *Kernel) reminderKindsFor(ev link.Event, t *turn) *Channel {
	// A fresh channel per wake shares the sidecar. The background loop and the
	// foreground wake both touch the log; they must not share the kind slice.
	ch := &Channel{log: k.reminders().log}
	ch.Register(Kind{
		Name:     KindPendingApproval,
		Cooldown: pendingCooldown,
		Trigger:  k.pendingTrigger(ev),
	})
	ch.Register(Kind{
		Name:     KindLoop,
		Cooldown: loopCooldown,
		Trigger:  nil,
	})
	ch.Register(Kind{
		Name:     KindBackgroundDone,
		Cooldown: 0,
		Trigger:  k.backgroundTrigger(),
	})
	ch.Register(Kind{
		Name:     KindFileChange,
		Cooldown: fileChangeCooldown,
		MaxBytes: maxDiffBytes + 2048,
		Trigger:  k.fileChangeTrigger(),
	})
	ch.Register(Kind{
		Name:     KindProject,
		MaxBytes: projectNoteMax,
		Trigger:  k.projectTrigger(t),
	})
	ch.Register(Kind{
		Name:    KindContext,
		Trigger: k.contextTrigger(),
	})
	ch.Register(Kind{
		Name:     KindTodo,
		Cooldown: todoCooldown,
		Trigger:  k.todoTrigger(),
	})
	ch.Register(Kind{
		Name:    KindShellJob,
		Trigger: k.shellTrigger(),
	})
	return ch
}

// stepBoundaryNotes records reminders that become true between tool steps:
// a changed project-instruction set, a context-window threshold, and an
// external file edit whose cooldown has elapsed. Each one is stored on the
// reminder channel so the next request replays the same bytes.
func (k *Kernel) stepBoundaryNotes(t *turn) string {
	if k == nil || t == nil {
		return ""
	}
	by := k.remindersByKind()
	seq := t.lastSeq()
	var parts []string
	ctx := context.WithValue(context.Background(), turnKey{}, t)
	if note := k.projectNoteFor(ctx, by[KindProject]); note != "" {
		if stored := k.storeReminder(seq, KindProject, note); stored != "" {
			t.projects.noteText(stored)
			parts = append(parts, stored)
		}
	}
	if note := k.contextNote(by[KindContext]); note != "" {
		if stored := k.storeReminder(seq, KindContext, note); stored != "" {
			parts = append(parts, stored)
		}
	}
	if cooledAt(by[KindFileChange], fileChangeCooldown, k.now()) {
		if note := k.fileChangeTrigger()(k.now(), by[KindFileChange]); note != "" {
			if stored := k.storeReminder(seq, KindFileChange, note); stored != "" {
				parts = append(parts, stored)
			}
		}
	}
	return strings.Join(parts, "\n\n")
}

// remindersByKind reads the sidecar once and groups notes by kind.
func (k *Kernel) remindersByKind() map[string][]Reminder {
	all, err := k.reminders().log.all()
	if err != nil {
		return nil
	}
	by := map[string][]Reminder{}
	for _, n := range all {
		by[n.Kind] = append(by[n.Kind], n)
	}
	return by
}

func cooledAt(prev []Reminder, d time.Duration, now time.Time) bool {
	if d <= 0 || len(prev) == 0 {
		return true
	}
	last := prev[len(prev)-1].At
	return !last.IsZero() && now.Sub(last) >= d
}

func (k *Kernel) storeReminder(seq int, kind, text string) string {
	text = k.reminders().log.prepare(text)
	if text == "" {
		return ""
	}
	if err := k.reminders().Record(seq, kind, text, k.now()); err != nil {
		return ""
	}
	return text
}

func (k *Kernel) kindPrev(kind string) []Reminder {
	all, err := k.reminders().log.all()
	if err != nil {
		return nil
	}
	var prev []Reminder
	for _, n := range all {
		if n.Kind == kind {
			prev = append(prev, n)
		}
	}
	return prev
}

// todoPrev is this kind's earlier firings, oldest first.
func (k *Kernel) todoPrev() []Reminder {
	all, err := k.reminders().log.all()
	if err != nil {
		return nil
	}
	var prev []Reminder
	for _, n := range all {
		if n.Kind == KindTodo {
			prev = append(prev, n)
		}
	}
	return prev
}

func (k *Kernel) pendingTrigger(ev link.Event) func(time.Time, []Reminder) string {
	return func(time.Time, []Reminder) string {
		if ev.Source == link.NameTimer {
			return ""
		}
		return pendingNote(k.openApprovals())
	}
}

func (k *Kernel) backgroundTrigger() func(time.Time, []Reminder) string {
	return func(_ time.Time, prev []Reminder) string {
		var seen strings.Builder
		for _, p := range prev {
			seen.WriteString(p.Text)
			seen.WriteByte('\n')
		}
		blob := seen.String()
		k.bgMu.Lock()
		defer k.bgMu.Unlock()
		var lines []string
		for _, bg := range k.finished {
			if bg.ID == "" || bg.Status == "" || strings.Contains(blob, bg.ID) {
				continue
			}
			lines = append(lines, "- "+bg.ID+"："+clipSummary(bg.Task, 80)+"（"+bg.Status+"）")
		}
		if len(lines) == 0 {
			return ""
		}
		return "<background_done>\n后台任务已结束。转告主人，不要重做这些事：\n" + strings.Join(lines, "\n") + "\n</background_done>"
	}
}

// newestSeq is the transcript sequence the new reminders bind to: the last
// line the projection will actually send.
func newestSeq(lines []bot.SeqEntry) int {
	for i := len(lines) - 1; i >= 0; i-- {
		e := lines[i].Entry
		if e.IsForkMark() || bot.IsApprovalCard(e.Message.Content) || code.IsCard(e.Message.Content) {
			continue
		}
		return lines[i].Seq
	}
	return 0
}
