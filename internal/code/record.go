// Package code stores coding-agent tasks apart from subagents.
// This file is the local record. It does not call a model or clone a repo.
package code

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"xbot/internal/fsutil"
)

const (
	StatusQueued      = "queued"
	StatusRunning     = "running"
	StatusDone        = "done"
	StatusFailed      = "failed"
	StatusCancelled   = "cancelled"
	StatusInterrupted = "interrupted"

	KindSteer     = "steer"
	KindQueue     = "queue"
	KindInterrupt = "interrupt"
	KindCancel    = "cancel"

	maxGoal    = 16 << 10
	maxRepo    = 1024
	maxDir     = 4096
	maxNote    = 16 << 10
	maxSteps   = 10000
	maxTimeout = 24 * 60 * 60
)

// Task is one coding-agent record.
type Task struct {
	PID        int       `json:"pid,omitempty"`
	ProcStart  int64     `json:"proc_start,omitempty"`
	ID         string    `json:"id"`
	WorkDir    string    `json:"work_dir"`
	Repo       string    `json:"repo,omitempty"`
	MaxSteps   int       `json:"max_steps"`
	TimeoutSec int       `json:"timeout_sec"`
	Goal       string    `json:"goal"`
	Status     string    `json:"status"`
	Notes      []Note    `json:"notes,omitempty"`
	Result     string    `json:"result,omitempty"`
	Created    time.Time `json:"created"`
	Updated    time.Time `json:"updated"`
}

// Note is a steer, queue, or interrupt left on a task.
type Note struct {
	Kind string    `json:"kind"`
	Text string    `json:"text"`
	At   time.Time `json:"at"`
}

// Record is the local task store. OnTerminal runs after a task becomes done, failed, or cancelled.
type Record struct {
	mu         sync.Mutex
	Dir        string
	OnTerminal func(Task)
	Now        func() time.Time
}

// Start records a task. It runs immediately when nothing else is running, otherwise it waits in the queue.
func (r *Record) Start(in Task) (Task, error) {
	in.Goal = strings.TrimSpace(in.Goal)
	in.WorkDir = strings.TrimSpace(in.WorkDir)
	in.Repo = strings.TrimSpace(in.Repo)
	if err := validate(in); err != nil {
		return Task{}, err
	}
	var out Task
	err := r.lock(func() error {
		id, err := newID()
		if err != nil {
			return err
		}
		now := r.now()
		running, err := r.hasRunning()
		if err != nil {
			return err
		}
		status := StatusRunning
		if running {
			status = StatusQueued
		}
		out = Task{
			ID: id, WorkDir: in.WorkDir, Repo: in.Repo, PID: in.PID, ProcStart: in.ProcStart,
			MaxSteps: in.MaxSteps, TimeoutSec: in.TimeoutSec, Goal: in.Goal,
			Status: status, Created: now, Updated: now,
		}
		return r.write(out)
	})
	return out, err
}

// Control steers, queues, interrupts, or cancels one task.
func (r *Record) Control(id, kind, text string) (Task, error) {
	if !validID(id) {
		return Task{}, fmt.Errorf("code: bad id %q", id)
	}
	text = strings.TrimSpace(text)
	switch kind {
	case KindSteer, KindQueue, KindInterrupt:
		if text == "" {
			return Task{}, errors.New("code: text is empty")
		}
		if len(text) > maxNote {
			return Task{}, errors.New("code: text is too long")
		}
	case KindCancel:
	default:
		return Task{}, fmt.Errorf("code: unknown control %q", kind)
	}
	var out Task
	err := r.lock(func() error {
		task, err := r.read(id)
		if err != nil {
			return err
		}
		if terminal(task.Status) {
			return fmt.Errorf("code: %s is %s", id, task.Status)
		}
		now := r.now()
		switch kind {
		case KindCancel:
			task.Status = StatusCancelled
			task.Updated = now
			task.Result = text
			if err := r.write(task); err != nil {
				return err
			}
			if err := r.promote(); err != nil {
				return err
			}
			out = task
			return nil
		case KindSteer, KindQueue, KindInterrupt:
			if task.Status != StatusRunning {
				return fmt.Errorf("code: %s is %s", id, task.Status)
			}
			task.Notes = append(task.Notes, Note{Kind: kind, Text: text, At: now})
			task.Updated = now
			if err := r.write(task); err != nil {
				return err
			}
			out = task
			return nil
		default:
			return fmt.Errorf("code: unknown control %q", kind)
		}
	})
	if err != nil {
		return Task{}, err
	}
	if kind == KindCancel {
		r.emit(out)
	}
	return out, nil
}

// Finish marks a running task done or failed, then promotes the oldest queued task.
// The model has no tool for this. A backend calls it when the task ends.
func (r *Record) Finish(id, text string, failed bool) (Task, error) {
	if !validID(id) {
		return Task{}, fmt.Errorf("code: bad id %q", id)
	}
	text = strings.TrimSpace(text)
	var out Task
	err := r.lock(func() error {
		task, err := r.read(id)
		if err != nil {
			return err
		}
		if task.Status != StatusRunning {
			return fmt.Errorf("code: %s is %s", id, task.Status)
		}
		task.Status = StatusDone
		if failed {
			task.Status = StatusFailed
		}
		task.Result = text
		task.Updated = r.now()
		if err := r.write(task); err != nil {
			return err
		}
		if err := r.promote(); err != nil {
			return err
		}
		out = task
		return nil
	})
	if err != nil {
		return Task{}, err
	}
	r.emit(out)
	return out, nil
}

// Get reads one task.
func (r *Record) Get(id string) (Task, error) {
	if !validID(id) {
		return Task{}, fmt.Errorf("code: bad id %q", id)
	}
	var out Task
	err := r.lock(func() error {
		task, err := r.read(id)
		if err != nil {
			return err
		}
		out = task
		return nil
	})
	return out, err
}

// List returns every task, oldest first.
func (r *Record) List() ([]Task, error) {
	var out []Task
	err := r.lock(func() error {
		tasks, err := r.list()
		if err != nil {
			return err
		}
		out = tasks
		return nil
	})
	return out, err
}

func (r *Record) emit(task Task) {
	if r != nil && r.OnTerminal != nil && terminal(task.Status) {
		r.OnTerminal(task)
	}
}

func (r *Record) promote() error {
	tasks, err := r.list()
	if err != nil {
		return err
	}
	for _, task := range tasks {
		if task.Status == StatusRunning {
			return nil
		}
	}
	for _, task := range tasks {
		if task.Status != StatusQueued {
			continue
		}
		task.Status = StatusRunning
		task.Updated = r.now()
		return r.write(task)
	}
	return nil
}

func (r *Record) hasRunning() (bool, error) {
	tasks, err := r.list()
	if err != nil {
		return false, err
	}
	for _, task := range tasks {
		if task.Status == StatusRunning {
			return true, nil
		}
	}
	return false, nil
}

func (r *Record) list() ([]Task, error) {
	entries, err := os.ReadDir(r.dir())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Task
	for _, ent := range entries {
		name := ent.Name()
		if ent.IsDir() || !strings.HasPrefix(name, "ca_") || !strings.HasSuffix(name, ".json") {
			continue
		}
		task, err := r.read(strings.TrimSuffix(name, ".json"))
		if err != nil {
			return nil, err
		}
		out = append(out, task)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Created.Equal(out[j].Created) {
			return out[i].ID < out[j].ID
		}
		return out[i].Created.Before(out[j].Created)
	})
	return out, nil
}

func (r *Record) read(id string) (Task, error) {
	raw, err := os.ReadFile(r.path(id))
	if errors.Is(err, os.ErrNotExist) {
		return Task{}, fmt.Errorf("code: %s not found", id)
	}
	if err != nil {
		return Task{}, err
	}
	var task Task
	if err := json.Unmarshal(raw, &task); err != nil {
		return Task{}, err
	}
	if task.ID != id {
		return Task{}, errors.New("code: broken record")
	}
	return task, nil
}

func (r *Record) write(task Task) error {
	raw, err := fsutil.Marshal(task)
	if err != nil {
		return err
	}
	return fsutil.WriteAtomic(r.path(task.ID), raw, 0600)
}

func (r *Record) lock(fn func() error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return fsutil.WithLock(filepath.Join(r.dir(), ".lock"), fn)
}

// Move serializes the directory rename with every record operation.
func (r *Record) Move(dir string, rename func() error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return fsutil.WithLock(filepath.Join(r.dir(), ".lock"), func() error {
		if err := rename(); err != nil {
			return err
		}
		r.Dir = dir
		return nil
	})
}

func (r *Record) dir() string {
	if r == nil || r.Dir == "" {
		return "code"
	}
	return r.Dir
}

func (r *Record) path(id string) string {
	return filepath.Join(r.dir(), id+".json")
}

func (r *Record) now() time.Time {
	if r != nil && r.Now != nil {
		return r.Now().UTC()
	}
	return time.Now().UTC()
}

func terminal(status string) bool {
	return status == StatusInterrupted || status == StatusDone || status == StatusFailed || status == StatusCancelled
}

func validate(in Task) error {
	if in.Goal == "" {
		return errors.New("code: goal is empty")
	}
	if len(in.Goal) > maxGoal {
		return errors.New("code: goal is too long")
	}
	if in.WorkDir == "" || !filepath.IsAbs(in.WorkDir) {
		return errors.New("code: work_dir must be absolute")
	}
	if len(in.WorkDir) > maxDir {
		return errors.New("code: work_dir is too long")
	}
	if len(in.Repo) > maxRepo {
		return errors.New("code: repo is too long")
	}
	if in.MaxSteps < 1 || in.MaxSteps > maxSteps {
		return errors.New("code: max_steps is out of range")
	}
	if in.TimeoutSec < 1 || in.TimeoutSec > maxTimeout {
		return errors.New("code: timeout_sec is out of range")
	}
	return nil
}

func newID() (string, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return "ca_" + hex.EncodeToString(buf[:]), nil
}

func validID(id string) bool {
	if len(id) != 19 || !strings.HasPrefix(id, "ca_") {
		return false
	}
	for _, r := range id[3:] {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f':
		default:
			return false
		}
	}
	return true
}
