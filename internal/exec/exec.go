// Package exec runs subagents. Local is in-process goroutines.
// The same Executor is implemented over HTTP by Remote, so a cloud runner is a deployment of Server.
package exec

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
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

// Endpoint is an OpenAI-compatible model the runner should call.
// The API key crosses the localhost socket and is covered by the runner token.
type Endpoint struct {
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
	Model   string `json:"model"`
}

// StartRequest begins one subagent.
type StartRequest struct {
	ID         string             `json:"id"`
	Bot        string             `json:"bot,omitempty"`
	Task       string             `json:"task"`
	Mode       string             `json:"mode"`
	Fork       []provider.Message `json:"fork,omitempty"`
	MaxSteps   int                `json:"max_steps,omitempty"`
	TimeoutSec int                `json:"timeout_sec,omitempty"`
	Dir        string             `json:"dir"`
	Model      Endpoint           `json:"model,omitempty"`
}

// Control is a message to a running subagent.
type Control struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Text string `json:"text,omitempty"`
}

// Event is something the subagent reports to the main inbox.
type Event struct {
	ID      string    `json:"id"`
	AgentID string    `json:"agent_id"`
	Kind    string    `json:"kind"`
	Text    string    `json:"text"`
	At      time.Time `json:"at"`
}

// Executor starts subagents and controls them. Events is the stream back to main.
type Executor interface {
	Start(ctx context.Context, req StartRequest) error
	Control(ctx context.Context, id string, c Control) error
	Events() <-chan Event
}

// Local runs each subagent as a goroutine.
type Local struct {
	Limits   Limits
	Prov     provider.Provider
	Approve  agent.Approver
	ToolSet  func(dir string) []tools.Tool
	mu       sync.Mutex
	jobs     map[string]*job
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

// Start launches a subagent. It returns when the goroutine is running, not when the task finishes.
func (l *Local) Start(ctx context.Context, req StartRequest) error {
	l.prepare()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateStart(req); err != nil {
		return err
	}
	p, err := l.providerFor(req)
	if err != nil {
		return err
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
	var list []tools.Tool
	if l.ToolSet != nil {
		list = l.ToolSet(req.Dir)
	}
	j := &job{
		id:            req.ID,
		task:          req.Task,
		mode:          req.Mode,
		fork:          append([]provider.Message(nil), req.Fork...),
		maxSteps:      steps,
		timeout:       timeout,
		progressEvery: l.limits.ProgressEvery,
		dir:           req.Dir,
		provider:      p,
		tools:         list,
		approve:       l.Approve,
		parent:        l,
		ctrl:          make(chan Control, 16),
	}
	l.mu.Lock()
	if len(l.jobs) >= l.limits.MaxConcurrent {
		l.mu.Unlock()
		return fmt.Errorf("exec: max concurrent subagents (%d)", l.limits.MaxConcurrent)
	}
	if _, ok := l.jobs[req.ID]; ok {
		l.mu.Unlock()
		return fmt.Errorf("exec: %s is already running", req.ID)
	}
	l.jobs[req.ID] = j
	l.mu.Unlock()
	go func() {
		defer func() {
			l.mu.Lock()
			delete(l.jobs, j.id)
			l.mu.Unlock()
		}()
		runLoop(j)
	}()
	return nil
}

// Control delivers steer, queue, interrupt, or cancel.
func (l *Local) Control(ctx context.Context, id string, c Control) error {
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
	return &provider.Client{
		BaseURL: req.Model.BaseURL,
		APIKey:  req.Model.APIKey,
		Model:   req.Model.Model,
	}, nil
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
