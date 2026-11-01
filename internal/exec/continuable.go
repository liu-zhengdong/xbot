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
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.stopped && status == statusRunning {
		status = "stopping"
	}
	return Task{ID: j.id, Task: j.task, Status: status, Position: position, Blocked: j.blocked, Continuable: j.request.Descriptor != nil, TurnSeq: j.turnSeq, ResultPath: j.resultPath}
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
	j.refreshTools = func() []tools.Tool { return l.listTools(req) }
	return j, nil
}

func (l *Local) boundDir(id string) (string, error) {
	if l.AgentsDir == "" {
		return "", ErrNotContinuable
	}
	dir := filepath.Join(l.AgentsDir, id)
	st, err := os.Lstat(dir)
	if err == nil && (!st.IsDir() || st.Mode()&os.ModeSymlink != 0) {
		return "", ErrNotFound
	}
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	return dir, nil
}

func (l *Local) startContinuable(ctx context.Context, req StartRequest) (Task, error) {
	dir, err := l.boundDir(req.ID)
	if err != nil {
		return Task{}, err
	}
	if canonicalDir(req.Dir) != canonicalDir(dir) {
		return Task{}, fmt.Errorf("exec: task directory is outside agents directory")
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
	key := canonicalDir(req.Dir)
	owners.Lock()
	defer owners.Unlock()
	l.mu.Lock()
	defer l.mu.Unlock()
	if owners.local[key] != nil {
		return Task{}, ErrActiveElsewhere
	}
	if _, err := os.Stat(filepath.Join(req.Dir, "descriptor.json")); err == nil {
		return Task{}, fmt.Errorf("exec: identity already exists; send a message to continue")
	} else if !os.IsNotExist(err) {
		return Task{}, err
	}
	if l.busyLocked(j.resource) || len(l.jobs) >= l.limits.MaxConcurrent {
		if l.waitingCountLocked() >= maxWaiting {
			return Task{}, ErrQueueFull
		}
	}
	if err := writeDescriptor(j.dir, d); err != nil {
		return Task{}, err
	}
	seed := initialMessages(j)
	// A fork always starts with the child's own rules and route overlay.
	if req.Mode == ModeFork {
		persona := req.Persona
		if persona == "" {
			persona = SubPersona
		}
		seed = append([]provider.Message{{Role: provider.RoleSystem, Content: provider.TextContent(persona)}}, copyMessages(req.Fork)...)
		seed = append(seed, provider.Message{Role: provider.RoleUser, Content: provider.TextContent(req.Task)})
	}
	for _, m := range seed[:len(seed)-1] {
		if _, err := j.record(SeqEntry{Entry: bot.Entry{Message: m}}); err != nil {
			return Task{}, err
		}
	}
	c := Control{Kind: KindSteer, Text: req.Task, Sender: "bot:" + req.Bot}
	seq, err := j.receive(c)
	if err != nil {
		return Task{}, err
	}
	j.initial, j.turnSeq = pendingInput{seq, c}, seq
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
	if c.Sender == "" {
		c.Sender = "bot:" + filepath.Base(filepath.Dir(l.AgentsDir))
	}
	if l.AgentsDir == "" {
		return Task{}, ErrNotContinuable
	}
	dir, err := l.boundDir(id)
	if err != nil {
		return Task{}, err
	}
	key := canonicalDir(dir)
	owners.Lock()
	if owner := owners.local[key]; owner != nil {
		owner.mu.Lock()
		owners.Unlock()
		defer owner.mu.Unlock()
		j, position := owner.findLocked(id)
		if j == nil {
			return Task{}, ErrActiveElsewhere
		}
		if j.request.Descriptor == nil {
			return Task{}, ErrNotContinuable
		}
		if len(j.pending) >= 16 {
			return Task{}, ErrQueueFull
		}
		next := j.closing || j.stopped || c.Kind == KindQueue
		if next && position == 0 && !j.hasNextLocked() && owner.waitingCountLocked() >= maxWaiting {
			return Task{}, ErrQueueFull
		}
		seq, err := j.receive(c)
		if err != nil {
			return Task{}, err
		}
		j.pending = append(j.pending, pendingInput{seq, c})
		if c.Kind == KindInterrupt && !j.closing && !j.stopped {
			j.mu.Lock()
			cancel := j.stepCancel
			j.mu.Unlock()
			if cancel != nil {
				cancel()
			}
		}
		select {
		case j.ctrl <- Control{}:
		default:
		}
		status := statusRunning
		if position > 0 {
			status = statusQueued
		}
		return j.snapshot(status, position), nil
	}
	owners.Unlock()
	d, err := ReadDescriptor(dir)
	if err != nil {
		return Task{}, err
	}
	m, err := ReadMeta(dir)
	if err != nil {
		return Task{}, err
	}
	if (m.Status == statusRunning || m.Status == statusQueued || m.Status == "stopping") && tools.SameProcess(m.PID, m.ProcStart) {
		return Task{}, ErrActiveElsewhere
	}
	if m.Status == statusRunning || m.Status == statusQueued || m.Status == "stopping" {
		if _, err := Interrupt(dir); err != nil {
			return Task{}, err
		}
	}
	if l.Restore == nil {
		return Task{}, fmt.Errorf("exec: restore is unavailable")
	}
	req, err := l.Restore(ctx, d)
	if err != nil {
		return Task{}, err
	}
	// The descriptor owns identity and policy; current runtime owns tools/credentials.
	req.ID, req.Bot, req.Dir, req.Type, req.Mode, req.Descriptor = id, d.Bot, dir, d.Type, d.Mode, &d
	req.MaxSteps, req.TimeoutSec, req.Task = d.MaxSteps, d.TimeoutSec, c.Text
	j, err := l.newJob(req)
	if err != nil {
		if l.ReleaseScope != nil {
			l.ReleaseScope(req)
		}
		return Task{}, err
	}
	entries, err := readRecords(dir)
	if err != nil {
		return Task{}, err
	}
	j.recordSeq = len(entries)
	owners.Lock()
	l.mu.Lock()
	// Another request may have claimed it while the current runtime was assembled.
	if owners.local[key] != nil {
		l.mu.Unlock()
		owners.Unlock()
		return l.Message(ctx, id, c)
	}
	defer owners.Unlock()
	defer l.mu.Unlock()
	if (l.busyLocked(j.resource) || len(l.jobs) >= l.limits.MaxConcurrent) && l.waitingCountLocked() >= maxWaiting {
		return Task{}, ErrQueueFull
	}
	seq, err := j.receive(c)
	if err != nil {
		return Task{}, err
	}
	j.initial, j.turnSeq = pendingInput{seq, c}, seq
	owners.local[key] = l
	return l.admitLocked(j), nil
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
	if l.AgentsDir == "" {
		return Task{}, ErrNotContinuable
	}
	dir, err := l.boundDir(id)
	if err != nil {
		return Task{}, err
	}
	owners.Lock()
	owner := owners.local[canonicalDir(dir)]
	if owner == nil {
		owners.Unlock()
		if _, err := ReadDescriptor(dir); err != nil {
			return Task{}, err
		}
		m, err := ReadMeta(dir)
		if err != nil {
			return Task{}, err
		}
		if (m.Status == statusRunning || m.Status == statusQueued || m.Status == "stopping") && tools.SameProcess(m.PID, m.ProcStart) {
			return Task{}, ErrActiveElsewhere
		}
		if m.Status == statusRunning || m.Status == statusQueued || m.Status == "stopping" {
			m, err = Interrupt(dir)
		}
		return Task{ID: id, Task: m.Task, Status: m.Status, Continuable: true, TurnSeq: m.TurnSeq, ResultPath: m.ResultPath}, err
	}
	owner.mu.Lock()
	owners.Unlock()
	j, position := owner.findLocked(id)
	if j == nil {
		owner.mu.Unlock()
		return Task{}, ErrNotFound
	}
	if err := j.dropPendingLocked(); err != nil {
		owner.mu.Unlock()
		return Task{}, err
	}
	j.stopped = true
	if position > 0 {
		// Keep it in admission while its cancellation is recorded and callbacks finish.
		j.closing = true
		if err := j.markInput("message_dropped", j.initial); err != nil {
			owner.mu.Unlock()
			return Task{}, err
		}
		owner.mu.Unlock()
		j.finish(statusCancelled, EvFailed, "cancelled before it started")
		return Task{ID: id, Task: j.task, Status: statusCancelled, Continuable: true, TurnSeq: j.turnSeq, ResultPath: j.resultPath}, nil
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
	dir := filepath.Join(l.AgentsDir, id)
	if _, ok := ActiveTask(dir); !ok {
		if _, err := ReadDescriptor(dir); errors.Is(err, ErrNotFound) || errors.Is(err, ErrNotContinuable) {
			return false, nil
		} else if err != nil {
			return true, err
		}
	}
	if c.Kind == KindCancel {
		_, err := l.Stop(ctx, id)
		return true, err
	}
	_, err := l.Message(ctx, id, c)
	return true, err
}

func (j *job) dropPendingLocked() error {
	for _, p := range j.pending {
		if err := j.markInput("message_dropped", p); err != nil {
			return err
		}
	}
	j.pending = nil
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
	entries, err := readRecords(j.dir)
	if err != nil {
		return nil, err
	}
	msgs := projectRecords(entries)
	if j.parent.Wrap != nil {
		j.provider = j.parent.Wrap(j.request, j.provider, len(msgs))
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
	j.closing = true
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
	entries, err := readRecords(j.dir)
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
	if j.request.Descriptor != nil {
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
	if !ok || j.parent != l || j.id != id || j.request.Descriptor == nil {
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

func (j *job) finishContinuable(status, kind, text string) {
	l := j.parent
	l.mu.Lock()
	if j.initErr != nil {
		status, kind, text = statusFailed, EvFailed, j.initErr.Error()
	}
	if j.stopped {
		status, kind, text = statusCancelled, EvFailed, "cancelled"
	}
	j.closing = true
	// This lock is the failure/stop cutoff. Explicit input accepted after it
	// belongs to a new activation and is never discarded with the old turn.
	if status != statusDone && !j.stopped {
		if err := j.dropPendingLocked(); err != nil {
			text += "\n" + err.Error()
		}
	} else if status == statusDone {
		keep := j.pending[:0]
		for _, p := range j.pending {
			if p.control.Kind == KindQueue {
				keep = append(keep, p)
			} else {
				// A steer accepted after the final drain must become a new turn.
				p.control.Kind = KindQueue
				keep = append(keep, p)
			}
		}
		j.pending = keep
	}
	l.mu.Unlock()
	fail := func(err error) {
		if err != nil {
			status, kind = statusFailed, EvFailed
			text += "\n" + err.Error()
		}
	}
	fail(j.sealCalls())
	if l.CurrentCWD != nil {
		d := *j.request.Descriptor
		if cwd := l.CurrentCWD(j.request); cwd != "" {
			d.CWD = cwd
		}
		err := writeDescriptor(j.dir, d)
		fail(err)
		if err == nil {
			j.request.Descriptor.CWD = d.CWD
		}
	}
	line := notable.Line(j.notable.Items())
	path, report, err := turnResult(j.dir, j.turnSeq, text, line)
	fail(err)
	if err != nil {
		report = "保存任务结果失败：" + err.Error()
	}
	j.mu.Lock()
	j.resultPath = path
	j.mu.Unlock()
	_, err = j.record(SeqEntry{Entry: bot.Entry{Origin: bot.OriginInternal}, Kind: "turn_end", TurnSeq: j.turnSeq, Status: status, ResultPath: path})
	fail(err)
	j.mu.Lock()
	j.blocked = false
	steps := j.steps
	j.mu.Unlock()
	j.writeStatus(status, clipText(text, reportBytes))
	if l.ReleaseScope != nil {
		l.ReleaseScope(j.request)
	}

	// Assemble outside all executor locks. The old activation continues to
	// own the identity while closing input can still be accepted.
	var next *job
	l.mu.Lock()
	hasNext := len(j.pending) > 0
	l.mu.Unlock()
	if hasNext {
		req := j.request
		var restoreErr error
		if l.Restore != nil {
			req, restoreErr = l.Restore(context.Background(), *j.request.Descriptor)
		}
		req.ID, req.Bot, req.Dir, req.Type, req.Mode, req.Descriptor = j.id, j.request.Bot, j.dir, j.kind, j.mode, j.request.Descriptor
		req.MaxSteps, req.TimeoutSec = j.request.Descriptor.MaxSteps, j.request.Descriptor.TimeoutSec
		if restoreErr == nil {
			next, restoreErr = l.newJob(req)
		}
		if restoreErr != nil {
			next = &job{request: req, id: j.id, dir: j.dir, parent: l, ctrl: make(chan Control, 16), timeout: j.timeout, maxSteps: j.maxSteps, initErr: restoreErr}
		}
	}
	owners.Lock()
	l.mu.Lock()
	// Input may arrive while callbacks run, including after the earlier sample.
	if len(j.pending) > 0 && next == nil {
		l.mu.Unlock()
		owners.Unlock()
		// Restore must run unlocked, and the owner remains closing throughout.
		req := j.request
		var restoreErr error
		if l.Restore != nil {
			req, restoreErr = l.Restore(context.Background(), *j.request.Descriptor)
		}
		req.ID, req.Bot, req.Dir, req.Type, req.Mode, req.Descriptor = j.id, j.request.Bot, j.dir, j.kind, j.mode, j.request.Descriptor
		if restoreErr == nil {
			next, restoreErr = l.newJob(req)
		}
		if restoreErr != nil {
			next = &job{request: req, id: j.id, dir: j.dir, parent: l, ctrl: make(chan Control, 16), timeout: j.timeout, maxSteps: j.maxSteps, initErr: restoreErr}
		}
		owners.Lock()
		l.mu.Lock()
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
		next.recordSeq = j.recordSeq
		l.waiting = append(l.waiting, next)
		next.writeStatus(statusQueued, "")
	} else {
		delete(owners.local, canonicalDir(j.dir))
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
