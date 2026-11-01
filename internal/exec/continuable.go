package exec

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"xbot/internal/bot"
	"xbot/internal/notable"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

var (
	ErrNotFound        = errors.New("exec: subagent not found")
	ErrNotContinuable  = errors.New("exec: subagent is not continuable")
	ErrActiveElsewhere = errors.New("exec: subagent is active in another executor process")
	ErrQueueFull       = errors.New("exec: subagent queue is full")
)

type Continuable interface {
	Stop(context.Context, string) (Task, error)
	Message(context.Context, string, Control) (Task, error)
	SendParent(context.Context, string, string) (Event, error)
}

type executingJobKey struct{}
type pendingInput struct {
	seq     int
	control Control
}

// This is only an address book for accepted work, never a second task table.
// Capacity and resource admission remain per Local, as for legacy execution;
// this map routes the same child ID and does not schedule new IDs across Kernels.
// Lock order is owners -> Local.mu -> job.recordMu/job.mu. Callbacks run unlocked.
var owners = struct {
	sync.Mutex
	local map[string]*Local
}{local: make(map[string]*Local)}

func canonicalDir(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return filepath.Clean(dir)
	}
	// New task directories do not exist yet; canonicalize the existing prefix.
	missing := []string{}
	for p := abs; ; p = filepath.Dir(p) {
		if real, err := filepath.EvalSymlinks(p); err == nil {
			for i := len(missing) - 1; i >= 0; i-- {
				real = filepath.Join(real, missing[i])
			}
			return real
		}
		if filepath.Dir(p) == p {
			return abs
		}
		missing = append(missing, filepath.Base(p))
	}
}

// ActiveTask reads the real admission owner without creating a Kernel or executor.
func ActiveTask(dir string) (Task, bool) {
	owners.Lock()
	l := owners.local[canonicalDir(dir)]
	if l == nil {
		owners.Unlock()
		return Task{}, false
	}
	l.mu.Lock()
	owners.Unlock()
	defer l.mu.Unlock()
	id := filepath.Base(filepath.Clean(dir))
	if j := l.jobs[id]; j != nil {
		return j.snapshot(statusRunning, 0), true
	}
	if i := l.waitingIndex(id); i >= 0 {
		return l.waiting[i].snapshot(statusQueued, i+1), true
	}
	return Task{}, false
}

func (j *job) snapshot(status string, position int) Task {
	var task Task
	_ = j.withStorageIO(func() error {
		if j.stopped && status == statusRunning {
			status = "stopping"
		}
		task = Task{ID: j.id, Task: j.task, Status: status, Position: position, Blocked: j.blocked, Continuable: j.request.Descriptor != nil, TurnSeq: j.turnSeq, ResultPath: j.resultPath}
		return nil
	})
	return task
}

func (l *Local) waitingCountLocked() int {
	n := len(l.waiting)
	for _, j := range l.jobs {
		if j.hasNextLocked() {
			n++
		}
	}
	return n
}
func (j *job) hasNextLocked() bool {
	for _, p := range j.pending {
		if j.closing || j.stopped || p.control.Kind == KindQueue {
			return true
		}
	}
	return false
}

func (l *Local) newJob(req StartRequest) (*job, error) {
	p, err := l.providerFor(req)
	if err != nil {
		return nil, err
	}
	steps := req.MaxSteps
	if steps <= 0 || steps > l.limits.MaxSteps {
		steps = l.limits.MaxSteps
	}
	timeout := l.limits.Timeout
	if req.TimeoutSec > 0 && time.Duration(req.TimeoutSec)*time.Second < timeout {
		timeout = time.Duration(req.TimeoutSec) * time.Second
	}
	j := &job{request: req, id: req.ID, task: req.Task, mode: req.Mode, kind: req.Type, persona: req.Persona,
		fork: copyMessages(req.Fork), maxSteps: steps, timeout: timeout, progressEvery: l.limits.ProgressEvery,
		dir: req.Dir, workspace: req.Workspace, resource: req.Resource, provider: p, approve: l.Approve, parent: l, ctrl: make(chan Control, 16)}
	j.refreshTools = func() []tools.Tool { return l.listTools(j.requestSnapshot()) }
	return j, nil
}

func (l *Local) boundDir(id string) (dir string, err error) {
	owners.Lock()
	defer owners.Unlock()
	err = l.directoryIO(id, func(_ string, path string, _ string) error { dir = path; return nil })
	return
}

// ownerFor resolves under the same address lock as MoveBot. No route callback
// takes an executor lock; the Local lock is acquired after the short callback.
func (l *Local) ownerFor(id string) (*Local, bool, error) {
	owners.Lock()
	var owner *Local
	err := l.directoryIO(id, func(_ string, dir string, _ string) error { owner = owners.local[canonicalDir(dir)]; return nil })
	if err != nil {
		owners.Unlock()
		return nil, false, err
	}
	if owner == nil {
		owners.Unlock()
		return nil, false, nil
	}
	owner.mu.Lock()
	owners.Unlock()
	j, _ := owner.findLocked(id)
	cont := j != nil && j.isContinuable()
	owner.mu.Unlock()
	return owner, cont, nil
}

func (l *Local) startContinuable(ctx context.Context, req StartRequest) (Task, error) {
	if l.WithCurrentIO == nil {
		dir, err := l.boundDir(req.ID)
		if err != nil {
			return Task{}, err
		}
		if canonicalDir(req.Dir) != canonicalDir(dir) {
			return Task{}, fmt.Errorf("exec: task directory is outside agents directory")
		}
	}
	d := *req.Descriptor
	d.Tools = append([]string(nil), d.Tools...)
	if d.Version != 1 || d.ID != req.ID || d.Mode != req.Mode || d.Bot != req.Bot {
		return Task{}, fmt.Errorf("exec: descriptor does not match request")
	}
	req.Descriptor = &d
	j, err := l.newJob(req)
	if err != nil {
		return Task{}, err
	}
	owners.Lock()
	defer owners.Unlock()
	l.mu.Lock()
	defer l.mu.Unlock()
	err = j.withStorageIO(func() error { return nil })
	if err != nil {
		return Task{}, err
	}
	key := canonicalDir(j.requestSnapshot().Dir)
	if owners.local[key] != nil {
		return Task{}, ErrActiveElsewhere
	}
	if (l.busyLocked(j.resource) || len(l.jobs) >= l.limits.MaxConcurrent) && l.waitingCountLocked() >= maxWaiting {
		return Task{}, ErrQueueFull
	}
	err = j.withStorageIO(func() error {
		if _, e := os.Stat(filepath.Join(j.dir, "descriptor.json")); e == nil {
			return fmt.Errorf("exec: identity already exists; send a message to continue")
		} else if !os.IsNotExist(e) {
			return e
		}
		if e := writeDescriptor(j.dir, *j.request.Descriptor); e != nil {
			return e
		}
		seed := initialMessages(j)
		if req.Mode == ModeFork {
			persona := req.Persona
			if persona == "" {
				persona = SubPersona
			}
			seed = append([]provider.Message{{Role: provider.RoleSystem, Content: provider.TextContent(persona)}}, copyMessages(req.Fork)...)
			seed = append(seed, provider.Message{Role: provider.RoleUser, Content: provider.TextContent(req.Task)})
		}
		for _, m := range seed[:len(seed)-1] {
			if _, e := j.recordLocked(SeqEntry{Entry: bot.Entry{Message: m}}); e != nil {
				return e
			}
		}
		c := Control{Kind: KindSteer, Text: req.Task, Sender: "bot:" + j.request.Bot}
		seq, e := j.recordLocked(SeqEntry{Entry: bot.Entry{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent(c.Text)}}, Kind: "message_received", Delivery: c.Kind, Sender: c.Sender})
		if e == nil {
			j.initial, j.turnSeq = pendingInput{seq, c}, seq
		}
		return e
	})
	if err != nil {
		return Task{}, err
	}
	owners.local[key] = l
	return l.admitLocked(j), nil
}

func (l *Local) admitLocked(j *job) Task {
	if l.busyLocked(j.resource) || len(l.jobs) >= l.limits.MaxConcurrent {
		l.waiting = append(l.waiting, j)
		j.writeStatus(statusQueued, "")
		return j.snapshot(statusQueued, len(l.waiting))
	}
	j.writeStatus(statusRunning, "")
	l.launchLocked(j)
	return j.snapshot(statusRunning, 0)
}

func (j *job) receive(c Control) (int, error) {
	return j.record(SeqEntry{Entry: bot.Entry{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent(c.Text)}}, Kind: "message_received", Delivery: c.Kind, Sender: c.Sender})
}
func (j *job) markInput(kind string, p pendingInput) error {
	_, err := j.record(SeqEntry{Entry: bot.Entry{Origin: bot.OriginInternal}, Kind: kind, InputSeq: p.seq, TurnSeq: j.turnSeq})
	return err
}

func (l *Local) Message(ctx context.Context, id string, c Control) (Task, error) {
	l.prepare()
	if err := ctx.Err(); err != nil {
		return Task{}, err
	}
	if !idRe.MatchString(id) {
		return Task{}, fmt.Errorf("exec: bad id")
	}
	if c.Kind == "" {
		c.Kind = KindSteer
	}
	if c.Kind != KindSteer && c.Kind != KindQueue && c.Kind != KindInterrupt {
		return Task{}, fmt.Errorf("exec: bad message delivery")
	}
	c.Text = strings.TrimSpace(c.Text)
	if c.Text == "" || len(c.Text) > 16<<10 {
		return Task{}, fmt.Errorf("exec: message must contain 1..16384 bytes")
	}
	owners.Lock()
	var dir string
	var owner *Local
	err := l.directoryIO(id, func(name, path, _ string) error {
		dir = path
		if c.Sender == "" {
			c.Sender = "bot:" + name
		}
		owner = owners.local[canonicalDir(path)]
		return nil
	})
	if err != nil {
		owners.Unlock()
		return Task{}, err
	}
	if owner != nil {
		owner.mu.Lock()
		owners.Unlock()
		j, position := owner.findLocked(id)
		if j == nil {
			owner.mu.Unlock()
			return Task{}, ErrNotFound
		}
		if !j.isContinuable() {
			owner.mu.Unlock()
			return Task{}, ErrNotContinuable
		}
		if len(j.pending) >= 16 {
			owner.mu.Unlock()
			return Task{}, ErrQueueFull
		}
		next := j.closing || j.stopped || c.Kind == KindQueue
		if next && position == 0 && !j.hasNextLocked() && owner.waitingCountLocked() >= maxWaiting {
			owner.mu.Unlock()
			return Task{}, ErrQueueFull
		}
		seq, e := j.receive(c)
		if e != nil {
			owner.mu.Unlock()
			return Task{}, e
		}
		j.pending = append(j.pending, pendingInput{seq, c})
		var cancel context.CancelFunc
		if c.Kind == KindInterrupt && !j.closing && !j.stopped {
			j.mu.Lock()
			cancel = j.stepCancel
			j.mu.Unlock()
		}
		select {
		case j.ctrl <- Control{}:
		default:
		}
		status := statusRunning
		if position > 0 {
			status = statusQueued
		}
		task := j.snapshot(status, position)
		owner.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		return task, nil
	}
	l.mu.Lock()
	var d Descriptor
	err = l.directoryIO(id, func(_ string, path, _ string) error {
		dir = path
		var e error
		d, e = ReadDescriptor(path)
		if e != nil {
			return e
		}
		m, e := ReadMeta(path)
		if e != nil {
			return e
		}
		if m.Status == statusRunning || m.Status == statusQueued || m.Status == "stopping" {
			if tools.SameProcess(m.PID, m.ProcStart) {
				return ErrActiveElsewhere
			}
			_, e = interruptContinuableIO(path, m, nil)
		}
		return e
	})
	l.mu.Unlock()
	owners.Unlock()
	if err != nil {
		return Task{}, err
	}
	if l.Restore == nil {
		return Task{}, fmt.Errorf("exec: restore is unavailable")
	}
	req, err := l.Restore(ctx, d)
	if err != nil {
		return Task{}, err
	}
	req.ID, req.Bot, req.Dir, req.Type, req.Mode, req.Descriptor = id, d.Bot, dir, d.Type, d.Mode, &d
	req.MaxSteps, req.TimeoutSec, req.Task = d.MaxSteps, d.TimeoutSec, c.Text
	j, err := l.newJob(req)
	if err != nil {
		if l.ReleaseScope != nil {
			l.ReleaseScope(req)
		}
		return Task{}, err
	}
	unused := true
	defer func() {
		if unused && l.ReleaseScope != nil {
			l.ReleaseScope(req)
		}
	}()
	owners.Lock()
	l.mu.Lock()
	var key string
	err = l.directoryIO(id, func(_ string, path, _ string) error { key = canonicalDir(path); return nil })
	if err != nil {
		l.mu.Unlock()
		owners.Unlock()
		return Task{}, err
	}
	if other := owners.local[key]; other != nil {
		l.mu.Unlock()
		owners.Unlock()
		if other != l && l.ReleaseScope != nil {
			l.ReleaseScope(req)
		}
		unused = false
		return l.Message(ctx, id, c)
	}
	err = j.withStorageIO(func() error {
		// Re-read dependencies after Restore; rename may have changed only the paths.
		current, e := ReadDescriptor(j.dir)
		if e != nil {
			return e
		}
		m, e := ReadMeta(j.dir)
		if e != nil {
			return e
		}
		if (m.Status == statusRunning || m.Status == statusQueued || m.Status == "stopping") && tools.SameProcess(m.PID, m.ProcStart) {
			return ErrActiveElsewhere
		}
		current.Provider, current.Model, current.Thinking, current.ReasoningEffort = d.Provider, d.Model, d.Thinking, d.ReasoningEffort
		current.Tools = append([]string(nil), d.Tools...)
		j.request.Descriptor = &current
		j.request.Bot = current.Bot
		entries, e := readRecords(j.dir)
		if e != nil {
			return e
		}
		j.recordSeq = len(entries)
		return nil
	})
	if err == nil && (l.busyLocked(j.resource) || len(l.jobs) >= l.limits.MaxConcurrent) && l.waitingCountLocked() >= maxWaiting {
		err = ErrQueueFull
	}
	if err != nil {
		l.mu.Unlock()
		owners.Unlock()
		return Task{}, err
	}
	seq, err := j.receive(c)
	if err != nil {
		l.mu.Unlock()
		owners.Unlock()
		return Task{}, err
	}
	j.initial, j.turnSeq = pendingInput{seq, c}, seq
	owners.local[key] = l
	unused = false
	task := l.admitLocked(j)
	l.mu.Unlock()
	owners.Unlock()
	return task, nil
}

func (l *Local) findLocked(id string) (*job, int) {
	if j := l.jobs[id]; j != nil {
		return j, 0
	}
	if i := l.waitingIndex(id); i >= 0 {
		return l.waiting[i], i + 1
	}
	return nil, 0
}

func (l *Local) Stop(ctx context.Context, id string) (Task, error) {
	l.prepare()
	if err := ctx.Err(); err != nil {
		return Task{}, err
	}
	if !idRe.MatchString(id) {
		return Task{}, fmt.Errorf("exec: bad id")
	}
	owners.Lock()
	var owner *Local
	err := l.directoryIO(id, func(_ string, dir string, _ string) error { owner = owners.local[canonicalDir(dir)]; return nil })
	if err != nil {
		owners.Unlock()
		return Task{}, err
	}
	if owner == nil {
		l.mu.Lock()
		var task Task
		err = l.directoryIO(id, func(_ string, dir string, _ string) error {
			if _, e := ReadDescriptor(dir); e != nil {
				return e
			}
			m, e := ReadMeta(dir)
			if e != nil {
				return e
			}
			if m.Status == statusRunning || m.Status == statusQueued || m.Status == "stopping" {
				if tools.SameProcess(m.PID, m.ProcStart) {
					return ErrActiveElsewhere
				}
				m, e = interruptContinuableIO(dir, m, nil)
			}
			task = Task{ID: id, Task: m.Task, Status: m.Status, Continuable: true, TurnSeq: m.TurnSeq, ResultPath: m.ResultPath}
			return e
		})
		l.mu.Unlock()
		owners.Unlock()
		return task, err
	}
	owner.mu.Lock()
	owners.Unlock()
	j, position := owner.findLocked(id)
	if j == nil {
		owner.mu.Unlock()
		return Task{}, ErrNotFound
	}
	if !j.isContinuable() {
		owner.mu.Unlock()
		return Task{}, ErrNotContinuable
	}
	if err := j.dropPendingLocked(); err != nil {
		owner.mu.Unlock()
		return Task{}, err
	}
	if j.stopped {
		task := j.snapshot("stopping", 0)
		owner.mu.Unlock()
		return task, nil
	}
	j.stopped = true
	if position > 0 {
		j.closing = true
		if err := j.markInput("message_dropped", j.initial); err != nil {
			owner.mu.Unlock()
			return Task{}, err
		}
		task := j.snapshot(statusCancelled, position)
		owner.mu.Unlock()
		j.finish(statusCancelled, EvFailed, "cancelled before it started")
		return task, nil
	}
	j.mu.Lock()
	j.cancelReq = true
	cancel := j.stepCancel
	j.mu.Unlock()
	task := j.snapshot(statusRunning, 0)
	owner.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	select {
	case j.ctrl <- Control{}:
	default:
	}
	return task, nil
}

func (l *Local) controlContinuable(ctx context.Context, id string, c Control) (bool, error) {
	owner, cont, err := l.ownerFor(id)
	if err != nil {
		return true, err
	}
	if owner != nil && !cont {
		return false, nil
	}
	if owner == nil {
		owners.Lock()
		err = l.directoryIO(id, func(_ string, dir string, _ string) error { _, e := ReadDescriptor(dir); return e })
		owners.Unlock()
		if errors.Is(err, ErrNotFound) || errors.Is(err, ErrNotContinuable) {
			return false, nil
		}
		if err != nil {
			return true, err
		}
	}
	if c.Kind == KindCancel {
		_, err = l.Stop(ctx, id)
	} else {
		_, err = l.Message(ctx, id, c)
	}
	return true, err
}

func (j *job) dropPendingLocked() error { return j.dropBeforeLocked(j.recordCount()) }

func (j *job) dropBeforeLocked(cutoff int) error {
	left := j.pending[:0]
	for _, p := range j.pending {
		if p.seq > cutoff {
			left = append(left, p)
			continue
		}
		if err := j.markInput("message_dropped", p); err != nil {
			return err
		}
	}
	j.pending = left
	return nil
}

func (j *job) beginContinuable() ([]provider.Message, error) {
	if j.initErr != nil {
		return nil, j.initErr
	}
	if err := j.sealCalls(); err != nil {
		return nil, err
	}
	if err := j.markInput("message_consumed", j.initial); err != nil {
		return nil, err
	}
	entries, err := j.readRecords()
	if err != nil {
		return nil, err
	}
	msgs := projectRecords(entries)
	if j.parent.Wrap != nil {
		j.provider = j.parent.Wrap(j.requestSnapshot(), j.provider, len(msgs))
	}
	return msgs, nil
}

func (j *job) drainContinuable(msgs *[]provider.Message, stepsLeft *int) (bool, bool) {
	l := j.parent
	l.mu.Lock()
	defer l.mu.Unlock()
	if j.stopped || j.initErr != nil {
		return true, false
	}
	steered := false
	left := j.pending[:0]
	for _, p := range j.pending {
		if p.control.Kind == KindQueue {
			left = append(left, p)
			continue
		}
		if err := j.markInput("message_consumed", p); err != nil {
			j.initErr = err
			return true, false
		}
		e := SeqEntry{Entry: bot.Entry{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent(p.control.Text)}}, Sender: p.control.Sender}
		*msgs = append(*msgs, inputMessage(e))
		if p.control.Kind == KindInterrupt {
			*stepsLeft = j.maxSteps
		}
		steered = true
	}
	j.pending = left
	return false, steered
}

func (j *job) closeReady(msgs *[]provider.Message, stepsLeft *int) bool {
	j.parent.mu.Lock()
	for _, p := range j.pending {
		if p.control.Kind != KindQueue && !j.stopped {
			j.parent.mu.Unlock()
			j.drainContinuable(msgs, stepsLeft)
			return false
		}
	}
	j.closing, j.closingSeq = true, j.recordCount()
	j.parent.mu.Unlock()
	return true
}

func (j *job) resumeInterrupt(msgs *[]provider.Message, stepsLeft *int) bool {
	j.parent.mu.Lock()
	hasInterrupt := false
	for _, p := range j.pending {
		if p.control.Kind == KindInterrupt {
			hasInterrupt = true
		}
	}
	j.parent.mu.Unlock()
	if !hasInterrupt {
		return false
	}
	if err := j.sealCalls(); err != nil {
		j.initErr = err
		return false
	}
	if stop, _ := j.drainContinuable(msgs, stepsLeft); stop {
		return false
	}
	entries, err := j.readRecords()
	if err != nil {
		j.initErr = err
		return false
	}
	*msgs = projectRecords(entries)
	return true
}

func (j *job) waitContinuable(ctx context.Context) error {
	for {
		j.parent.mu.Lock()
		ready := j.stopped
		for _, p := range j.pending {
			if p.control.Kind != KindQueue {
				ready = true
			}
		}
		j.parent.mu.Unlock()
		if ready {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-j.ctrl:
		}
	}
}

func (j *job) reportBlocked(text string) error {
	ev := Event{AgentID: j.id, Kind: EvBlocked, Text: clipText(text, 2048), TurnSeq: j.turnSeq}
	if j.isContinuable() {
		seq, err := j.record(SeqEntry{Entry: bot.Entry{Origin: bot.OriginInternal}, Kind: "blocked", TurnSeq: j.turnSeq})
		if err != nil {
			return err
		}
		ev.ID = fmt.Sprintf("subagent:%s:turn:%d:blocked:%d", j.id, j.turnSeq, seq)
	}
	j.parent.emit(ev)
	return nil
}

func (l *Local) SendParent(ctx context.Context, id, text string) (Event, error) {
	if err := ctx.Err(); err != nil {
		return Event{}, err
	}
	j, ok := ctx.Value(executingJobKey{}).(*job)
	if !ok || j.parent != l || j.id != id || !j.isContinuable() {
		return Event{}, fmt.Errorf("exec: parent message requires the running child context")
	}
	text = strings.TrimSpace(text)
	if text == "" || len(text) > 16<<10 {
		return Event{}, fmt.Errorf("exec: message must contain 1..16384 bytes")
	}
	l.mu.Lock()
	if l.jobs[id] != j || j.closing || j.stopped {
		l.mu.Unlock()
		return Event{}, ErrNotFound
	}
	seq, err := j.record(SeqEntry{Entry: bot.Entry{Message: provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent(text)}, Origin: bot.OriginInternal}, Kind: "message", TurnSeq: j.turnSeq, Sender: id})
	l.mu.Unlock()
	if err != nil {
		return Event{}, err
	}
	ev := Event{ID: fmt.Sprintf("subagent:%s:message:%d", id, seq), AgentID: id, Kind: "message", Text: text, TurnSeq: j.turnSeq, At: time.Now().UTC()}
	l.emit(ev)
	return ev, nil
}

// TerminalEventID is also used by startup recovery, so each turn reports once.
func TerminalEventID(id string, turnSeq int) string {
	return fmt.Sprintf("subagent:%s:turn:%d:end", id, turnSeq)
}

func (j *job) restoreNext() *job {
	l := j.parent
	current := j.requestSnapshot()
	req := cloneRequest(current)
	var err error
	if l.Restore != nil {
		req, err = l.Restore(context.Background(), *current.Descriptor)
	}
	req.ID, req.Bot, req.Dir, req.Type, req.Mode, req.Descriptor = j.id, current.Bot, current.Dir, j.kind, j.mode, current.Descriptor
	req.MaxSteps, req.TimeoutSec = current.Descriptor.MaxSteps, current.Descriptor.TimeoutSec
	if err == nil {
		if next, e := l.newJob(req); e == nil {
			return next
		} else {
			err = e
		}
	}
	return &job{request: req, id: j.id, dir: current.Dir, workspace: req.Workspace, resource: req.Resource, parent: l, ctrl: make(chan Control, 16), timeout: j.timeout, maxSteps: j.maxSteps, initErr: err}
}

func (j *job) finishContinuable(status, kind, text string) {
	l := j.parent
	l.mu.Lock()
	if j.initErr != nil {
		status, kind, text = statusFailed, EvFailed, j.initErr.Error()
	}
	if j.stopped {
		status, kind, text = statusCancelled, EvFailed, "cancelled"
	}
	if !j.closing {
		j.closing, j.closingSeq = true, j.recordCount()
	}
	cutoff, stopped := j.closingSeq, j.stopped
	// Explicit input after closing belongs to a new activation, including
	// when saving this turn's CWD or conclusion subsequently fails.
	if status != statusDone && !stopped {
		if err := j.dropBeforeLocked(cutoff); err != nil {
			text += "\n" + err.Error()
		}
	}
	l.mu.Unlock()
	req := j.requestSnapshot()
	cwd := ""
	if l.CurrentCWD != nil {
		cwd = l.CurrentCWD(req)
	}
	line := notable.Line(j.notable.Items())
	report := ""
	var failures []error
	failIO := func(err error) {
		if err != nil {
			failures = append(failures, err)
			status, kind = statusFailed, EvFailed
			text += "\n" + err.Error()
		}
	}
	var steps int
	var release StartRequest
	ioErr := j.withStorageIO(func() error {
		failIO(j.sealCallsLocked())
		d := *j.request.Descriptor
		if cwd != "" {
			d.CWD = rebasePath(cwd, workForAgents(filepath.Dir(req.Dir)), j.workspace)
		}
		if e := writeDescriptor(j.dir, d); e != nil {
			failIO(e)
		} else {
			j.request.Descriptor.CWD = d.CWD
		}
		path, result, e := turnResult(j.dir, j.turnSeq, text, line)
		failIO(e)
		report = result
		if e != nil {
			report = "saving task result failed: " + e.Error()
		}
		j.resultPath = path
		_, e = j.recordLocked(SeqEntry{Entry: bot.Entry{Origin: bot.OriginInternal}, Kind: "turn_end", TurnSeq: j.turnSeq, Status: status, ResultPath: path})
		failIO(e)
		j.blocked = false
		steps = j.steps
		failIO(j.writeStatusLocked(status, clipText(text, reportBytes)))
		release = cloneRequest(j.request)
		return nil
	})
	failIO(ioErr)
	if len(failures) > 0 && !stopped {
		l.mu.Lock()
		if e := j.dropBeforeLocked(cutoff); e != nil {
			text += "\n" + e.Error()
		}
		l.mu.Unlock()
	}
	if release.ID == "" {
		release = req
	}
	if l.ReleaseScope != nil {
		l.ReleaseScope(release)
	}

	// Assemble outside all executor locks. The old activation continues to
	// own the identity while closing input can still be accepted.
	var next *job
	for {
		owners.Lock()
		l.mu.Lock()
		if len(j.pending) == 0 && next != nil {
			l.mu.Unlock()
			owners.Unlock()
			if l.ReleaseScope != nil {
				l.ReleaseScope(next.requestSnapshot())
			}
			next = nil
			continue
		}
		if len(j.pending) == 0 || next != nil {
			break
		}
		l.mu.Unlock()
		owners.Unlock()
		next = j.restoreNext()
	}
	if next != nil {
		current := j.requestSnapshot()
		if e := next.withStorageIO(func() error {
			next.rebaseCurrentLocked(current.Bot, filepath.Dir(current.Dir), current.Workspace)
			return nil
		}); e != nil {
			next.initErr = e
		}
	}
	delete(l.jobs, j.id)
	if i := l.waitingIndex(j.id); i >= 0 {
		l.waiting = append(l.waiting[:i], l.waiting[i+1:]...)
	}
	if len(j.pending) > 0 {
		first := j.pending[0]
		next.initial, next.turnSeq, next.task = first, first.seq, first.control.Text
		next.request.Task = first.control.Text
		next.pending = append([]pendingInput(nil), j.pending[1:]...)
		for i := range next.pending {
			next.pending[i].control.Kind = KindQueue
		}
		next.recordSeq = j.recordCount()
		l.waiting = append(l.waiting, next)
		next.writeStatus(statusQueued, "")
	} else {
		key := canonicalDir(j.requestSnapshot().Dir)
		if owners.local[key] == l {
			delete(owners.local, key)
		}
	}
	l.mu.Unlock()
	owners.Unlock()
	l.emit(Event{ID: TerminalEventID(j.id, j.turnSeq), AgentID: j.id, Kind: kind, TurnSeq: j.turnSeq, Text: fmt.Sprintf("任务 %s 结束：%s，%d 步。\n", j.id, status, steps) + report})
	l.mu.Lock()
	l.drainLocked()
	idle := len(l.jobs) == 0 && len(l.waiting) == 0
	l.mu.Unlock()
	if idle && l.OnIdle != nil {
		l.OnIdle()
	}
}
