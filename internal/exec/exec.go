// Package exec runs subagents. Local is in-process goroutines.
// The same Executor is implemented over HTTP by Remote, so a cloud runner is a deployment of Server.
package exec

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"xbot/internal/agent"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

const (
	ModeBrief = "brief"
	ModeFork  = "fork"

	KindSteer     = "steer"
	KindQueue     = "queue"
	KindInterrupt = "interrupt"
	KindCancel    = "cancel"

	EvProgress = "progress"
	EvBlocked  = "blocked"
	EvDone     = "done"
	EvFailed   = "failed"

	statusQueued    = "queued"
	statusRunning   = "running"
	statusDone      = "done"
	statusFailed    = "failed"
	statusCancelled = "cancelled"
)

// Limits bound one executor. Zero values use the defaults.
// ProgressEvery below zero emits progress after every step.
type Limits struct {
	MaxConcurrent int
	MaxSteps      int
	Timeout       time.Duration
	ProgressEvery time.Duration
}

func (l Limits) norm() Limits {
	if l.MaxConcurrent <= 0 {
		l.MaxConcurrent = 4
	}
	if l.MaxSteps <= 0 {
		l.MaxSteps = 16
	}
	if l.Timeout <= 0 {
		l.Timeout = 10 * time.Minute
	}
	if l.ProgressEvery == 0 {
		l.ProgressEvery = 2 * time.Second
	}
	return l
}

// Endpoint is the protocol and model the runner should call.
// The API key crosses the localhost socket and is covered by the runner token.
type Endpoint struct {
	Protocol     string `json:"protocol,omitempty"`
	HostedSearch bool   `json:"hosted_web_search,omitempty"`
	Thinking     string `json:"thinking,omitempty"`
	BaseURL      string `json:"base_url"`
	APIKey       string `json:"api_key"`
	Model        string `json:"model"`
}

// StartRequest begins one subagent.
type StartRequest struct {
	JobDir     string             `json:"job_dir,omitempty"`
	ID         string             `json:"id"`
	Bot        string             `json:"bot,omitempty"`
	Task       string             `json:"task"`
	Mode       string             `json:"mode"`
	Fork       []provider.Message `json:"fork,omitempty"`
	MaxSteps   int                `json:"max_steps,omitempty"`
	TimeoutSec int                `json:"timeout_sec,omitempty"`
	Dir        string             `json:"dir"`
	// Workspace is the Bot workspace boundary for completed-effect reporting.
	// Dir remains the separate task-record directory; neither changes tool cwd.
	Workspace string   `json:"workspace,omitempty"`
	Model     Endpoint `json:"model,omitempty"`
	// Type is the subagent type (ADR 0016); it is recorded in meta.json.
	Type string `json:"type,omitempty"`
	// Persona replaces SubPersona as a brief subagent's system prompt.
	Persona string `json:"persona,omitempty"`
	// Resource is what this subagent writes (a repo or a directory). Two
	// with the same resource never run at once: the later one waits,
	// status queued, until the earlier ends. Empty never waits.
	Resource string `json:"resource,omitempty"`
}

// Control is a message to a running subagent.
type Control struct {
	JobDir string `json:"job_dir,omitempty"`
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Text   string `json:"text,omitempty"`
}

// Event is something the subagent reports to the main inbox.
type Event struct {
	Images  []string  `json:"images,omitempty"`
	ID      string    `json:"id"`
	AgentID string    `json:"agent_id"`
	Kind    string    `json:"kind"`
	Text    string    `json:"text"`
	At      time.Time `json:"at"`
}

// Executor starts subagents and controls them. Events is the stream back to main.
type Executor interface {
	Start(ctx context.Context, req StartRequest) (Task, error)
	Control(ctx context.Context, id string, c Control) error
	Events() <-chan Event
	// Tasks returns only accepted running and queued work, in stable order.
	Tasks() ([]Task, error)
}

// Task is a live executor snapshot. Position is 1-based in the waiting queue,
// and zero for running work. The executor alone owns admission and ordering.
type Task struct {
	Blocked  bool   `json:"blocked,omitempty"`
	ID       string `json:"id"`
	Task     string `json:"task"`
	Status   string `json:"status"`
	Position int    `json:"position,omitempty"`
}

// Local runs each subagent as a goroutine.
type Local struct {
	RunTool func(StartRequest, context.Context, tools.Tool, json.RawMessage, func(context.Context) tools.CallResult) tools.CallResult
	JobsFor func(StartRequest) (*tools.Jobs, error)
	Limits  Limits
	Prov    provider.Provider
	Approve agent.Approver
	ToolSet func(id, dir string) []tools.Tool
	// ToolsFor, when set, wins over ToolSet: it sees the whole request, so the
	// set can follow the subagent's type.
	ToolsFor func(req StartRequest) []tools.Tool
	// Wrap, when set, wraps the job's provider. pre is the number of messages
	// the job starts with; the kernel uses it to keep the job's requests under
	// the context lines.
	Wrap     func(req StartRequest, p provider.Provider, pre int) provider.Provider
	mu       sync.Mutex
	jobs     map[string]*job
	waiting  []*job // capacity/resource waiters, oldest first
	events   chan Event
	limits   Limits
	prepared bool
}

// NewLocal returns an executor with the given limits.
func NewLocal(lim Limits) *Local {
	return &Local{
		Limits: lim,
		jobs:   map[string]*job{},
		events: make(chan Event, 64),
	}
}

func (l *Local) prepare() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.prepared {
		return
	}
	if l.jobs == nil {
		l.jobs = map[string]*job{}
	}
	if l.events == nil {
		l.events = make(chan Event, 64)
	}
	l.limits = l.Limits.norm()
	l.prepared = true
}

// Events is the stream of progress, blocked, done, and failed.
func (l *Local) Events() <-chan Event {
	l.prepare()
	return l.events
}

// listTools builds one subagent's tool set for the request. ToolsFor wins over
// ToolSet because it sees the whole request, so the set can follow the type.
func (l *Local) listTools(req StartRequest) []tools.Tool {
	if l.ToolsFor != nil {
		return l.ToolsFor(req)
	}
	if l.ToolSet != nil {
		return l.ToolSet(req.ID, req.Dir)
	}
	return nil
}

// Start launches a subagent. It returns when the goroutine is running, not when the task finishes.
func (l *Local) Start(ctx context.Context, req StartRequest) (Task, error) {
	l.prepare()
	if err := ctx.Err(); err != nil {
		return Task{}, err
	}
	if err := validateStart(req); err != nil {
		return Task{}, err
	}
	p, err := l.providerFor(req)
	if err != nil {
		return Task{}, err
	}
	steps := req.MaxSteps
	if steps <= 0 || steps > l.limits.MaxSteps {
		steps = l.limits.MaxSteps
	}
	timeout := l.limits.Timeout
	if req.TimeoutSec > 0 {
		want := time.Duration(req.TimeoutSec) * time.Second
		if want < timeout {
			timeout = want
		}
	}
	list := l.listTools(req)
	j := &job{
		request:       req,
		id:            req.ID,
		task:          req.Task,
		mode:          req.Mode,
		kind:          req.Type,
		persona:       req.Persona,
		fork:          append([]provider.Message(nil), req.Fork...),
		maxSteps:      steps,
		timeout:       timeout,
		progressEvery: l.limits.ProgressEvery,
		dir:           req.Dir,
		workspace:     req.Workspace,
		resource:      req.Resource,
		provider:      p,
		tools:         list,
		refreshTools:  func() []tools.Tool { return l.listTools(req) },
		approve:       l.Approve,
		parent:        l,
		ctrl:          make(chan Control, 16),
	}
	if l.Wrap != nil {
		j.provider = l.Wrap(req, j.provider, len(initialMessages(j)))
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.jobs[req.ID]; ok || l.waitingIndex(req.ID) >= 0 {
		return Task{}, fmt.Errorf("exec: %s is already running", req.ID)
	}
	if l.busyLocked(j.resource) || len(l.jobs) >= l.limits.MaxConcurrent {
		if len(l.waiting) >= maxWaiting {
			return Task{}, fmt.Errorf("exec: %d subagents already queued", maxWaiting)
		}
		// The record exists from the start, so read_subagent and the
		// status list show it as queued.
		if err := os.MkdirAll(j.dir, 0700); err != nil {
			return Task{}, err
		}
		pid, start := tools.CurrentProcess()
		if err := writeMeta(j.dir, Meta{PID: pid, ProcStart: start, ID: j.id, Status: statusQueued, Task: j.task, Mode: j.mode, Type: j.kind}); err != nil {
			return Task{}, err
		}
		l.waiting = append(l.waiting, j)
		return Task{ID: j.id, Task: j.task, Status: statusQueued, Position: len(l.waiting)}, nil
	}
	l.launchLocked(j)
	return Task{ID: j.id, Task: j.task, Status: statusRunning, Blocked: j.isBlocked()}, nil
}

// maxWaiting caps the shared capacity/resource waiting queue.
const maxWaiting = 8

// busyLocked reports whether a running job holds res. l.mu must be held.
func (l *Local) busyLocked(res string) bool {
	if res == "" {
		return false
	}
	for _, other := range l.jobs {
		if other.resource == res {
			return true
		}
	}
	return false
}

func (l *Local) waitingIndex(id string) int {
	for i, w := range l.waiting {
		if w.id == id {
			return i
		}
	}
	return -1
}

// launchLocked starts j. When it ends, the oldest waiting job whose
// resource is free starts in its place. l.mu must be held.
func (l *Local) launchLocked(j *job) {
	l.jobs[j.id] = j
	go func() {
		runLoop(j)
		l.mu.Lock()
		defer l.mu.Unlock()
		delete(l.jobs, j.id)
		l.drainLocked()
	}()
}

// drainLocked fills free slots with the oldest resource-eligible waiter.
// A blocked resource never prevents independent work from using a free slot.
func (l *Local) drainLocked() {
	for len(l.jobs) < l.limits.MaxConcurrent {
		next := -1
		for i, w := range l.waiting {
			if !l.busyLocked(w.resource) {
				next = i
				break
			}
		}
		if next < 0 {
			return
		}
		w := l.waiting[next]
		l.waiting = append(l.waiting[:next], l.waiting[next+1:]...)
		l.launchLocked(w)
	}
}

// Tasks reads live admission state without scanning completed histories.
func (l *Local) Tasks() ([]Task, error) {
	l.prepare()
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Task, 0, len(l.jobs)+len(l.waiting))
	for _, j := range l.jobs {
		out = append(out, Task{ID: j.id, Task: j.task, Status: statusRunning, Blocked: j.isBlocked()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	for i, j := range l.waiting {
		out = append(out, Task{ID: j.id, Task: j.task, Status: statusQueued, Position: i + 1})
	}
	return out, nil
}

// Control delivers steer, queue, interrupt, or cancel.
func (l *Local) Control(ctx context.Context, id string, c Control) error {
	if strings.HasPrefix(id, "sh_") && c.JobDir != "" && l.JobsFor != nil {
		jobs, err := l.JobsFor(StartRequest{JobDir: c.JobDir})
		if err != nil {
			return err
		}
		_, err = jobs.Cancel(id)
		return err
	}
	l.prepare()
	if err := ctx.Err(); err != nil {
		return err
	}
	c.ID = id
	if c.Kind != KindCancel && strings.TrimSpace(c.Text) == "" {
		return errors.New("exec: control text is empty")
	}
	l.mu.Lock()
	j := l.jobs[id]
	if j == nil {
		if i := l.waitingIndex(id); i >= 0 {
			w := l.waiting[i]
			if c.Kind == KindCancel {
				// It never started: nothing to stop, only the queue entry.
				l.waiting = append(l.waiting[:i], l.waiting[i+1:]...)
				l.mu.Unlock()
				w.finish(statusCancelled, EvFailed, "cancelled before it started")
				return nil
			}
			j = w
		}
	}
	l.mu.Unlock()
	if j == nil {
		return fmt.Errorf("exec: %s is not running", id)
	}
	return j.control(c)
}

func (l *Local) providerFor(req StartRequest) (provider.Provider, error) {
	if l.Prov != nil {
		return l.Prov, nil
	}
	if req.Model.BaseURL == "" || req.Model.APIKey == "" || req.Model.Model == "" {
		return nil, errors.New("exec: model endpoint is incomplete")
	}
	return provider.New(provider.Endpoint{Protocol: req.Model.Protocol, HostedSearch: req.Model.HostedSearch,
		BaseURL: req.Model.BaseURL, APIKey: req.Model.APIKey, Model: req.Model.Model, Thinking: req.Model.Thinking})
}

func (l *Local) emit(ev Event) {
	if ev.At.IsZero() {
		ev.At = time.Now().UTC()
	}
	if ev.ID == "" {
		ev.ID = newID("ag_")
	}
	l.events <- ev
}

func (j *job) control(c Control) error {
	switch c.Kind {
	case KindSteer, KindQueue, KindInterrupt, KindCancel:
	default:
		return fmt.Errorf("exec: unknown control %q", c.Kind)
	}
	if c.Kind == KindInterrupt || c.Kind == KindCancel {
		j.mu.Lock()
		cancel := j.stepCancel
		if cancel != nil {
			if c.Kind == KindInterrupt {
				j.interrupt = c.Text
			} else {
				j.cancelReq = true
			}
			j.mu.Unlock()
			cancel()
			return nil
		}
		j.mu.Unlock()
	}
	select {
	case j.ctrl <- c:
		return nil
	default:
		return errors.New("exec: control queue is full")
	}
}

var idRe = regexp.MustCompile(`^sa_[a-z0-9]{8,32}$`)

func validateStart(req StartRequest) error {
	if !idRe.MatchString(req.ID) {
		return fmt.Errorf("exec: bad id %q", req.ID)
	}
	req.Task = strings.TrimSpace(req.Task)
	if req.Task == "" {
		return errors.New("exec: task is empty")
	}
	if len(req.Task) > 16<<10 {
		return errors.New("exec: task is too long")
	}
	switch req.Mode {
	case ModeBrief, ModeFork:
	default:
		return fmt.Errorf("exec: bad mode %q", req.Mode)
	}
	if req.Mode == ModeFork && len(req.Fork) == 0 {
		return errors.New("exec: fork context is empty")
	}
	if req.Dir == "" || !filepath.IsAbs(req.Dir) {
		return errors.New("exec: dir must be absolute")
	}
	return nil
}

func newID(prefix string) string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%s%d", prefix, time.Now().UnixNano())
	}
	return prefix + hex.EncodeToString(b[:])
}

// ActiveCount is an in-memory snapshot; queued jobs do not count as running.
func (l *Local) ActiveCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.jobs)
}
