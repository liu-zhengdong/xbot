package kernel

import (
	"encoding/json"
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

// Reminder kinds wired now. Todo and file-change use the same Register path;
// nothing in the kernel fires them until a later milestone plugs in a trigger.
const (
	KindPendingApproval = "pending_approval"
	KindLoop            = "loop"
	KindBackgroundDone  = "background_done"
	KindTodo            = "todo"
	KindFileChange      = "file_change"

	pendingCooldown = 10 * time.Minute
	loopCooldown    = time.Minute
	reminderMax     = 2048
)

// Kind is one reminder. Cooldown is the minimum gap between firings.
// Trigger returns the note, or "" when the condition is not met. prev is this
// kind's earlier firings, oldest first, so a trigger can suppress repeats
// without rewriting them.
type Kind struct {
	Name     string
	Cooldown time.Duration
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

// Register adds or replaces a kind. Later milestones register todo and
// file_change the same way; the channel does not special-case names.
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
		text := clipReminder(k.Trigger(now, prev))
		if text == "" {
			continue
		}
		out = append(out, Reminder{Kind: k.Name, Text: text, At: now})
	}
	return out, nil
}

// Bind appends due reminders to the newest message and records them against seq
// so the next request can replay the same bytes. Earlier messages are not touched.
func (c *Channel) Bind(msgs []provider.Message, seq int, now time.Time) ([]provider.Message, error) {
	if c == nil {
		return msgs, nil
	}
	due, err := c.Due(now)
	if err != nil || len(due) == 0 {
		return msgs, err
	}
	var b strings.Builder
	for i, d := range due {
		due[i].Text = c.log.prepare(d.Text)
		if due[i].Text == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(due[i].Text)
	}
	if b.Len() == 0 {
		return msgs, nil
	}
	msgs = agent.AttachReminder(msgs, b.String())
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
func (c *Channel) Replay(entries []bot.Entry, seqs []int) error {
	if c == nil || len(entries) == 0 {
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
	if len(by) == 0 {
		return nil
	}
	for i := range entries {
		if i >= len(seqs) {
			break
		}
		texts := by[seqs[i]]
		if len(texts) == 0 {
			continue
		}
		entries[i].Message.Content = joinReminder(entries[i].Message.Content, strings.Join(texts, "\n\n"))
	}
	return nil
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
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	out, _ := clip.Bytes(s, reminderMax)
	return out
}

// reminderKeep is the point at which notes older than the live transcript
// window are dropped. The newest note of each kind is kept for cooldown.
const reminderKeep = 48

// reminderLog is the sidecar. An empty path keeps notes in memory for tests.
// The transcript body is never rewritten. Notes stay cached after the first
// read; compaction rewrites the file when the live window no longer needs
// the older lines.
type reminderLog struct {
	mu     sync.Mutex
	path   string
	notes  []Reminder
	loaded bool
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
	return clipReminder(text)
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

// load reads the file once. The caller holds l.mu.
func (l *reminderLog) load() error {
	if l.loaded || l.path == "" {
		l.loaded = true
		return nil
	}
	var notes []Reminder
	err := fsutil.ReadJSONL(l.path, func(line []byte) error {
		var n Reminder
		if err := json.Unmarshal(line, &n); err != nil {
			return err
		}
		notes = append(notes, n)
		return nil
	})
	if err != nil {
		return err
	}
	l.notes = notes
	l.loaded = true
	return nil
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
	return fsutil.AppendJSONL(l.path, n)
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
	return os.Rename(tmp, l.path)
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

// reminderKinds registers the live kinds for this wake. Todo and file-change
// stay unregistered until their milestones supply a trigger.
func (k *Kernel) reminderKinds(ev link.Event) *Channel {
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
	return ch
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
