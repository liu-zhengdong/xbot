package exec

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"xbot/internal/work"
)

// withStorageIO is the short path/record fence. Runtime callbacks run after it
// returns; fn performs only in-memory updates and local record I/O.
func (j *job) withStorageIO(fn func() error) error {
	j.recordMu.Lock()
	defer j.recordMu.Unlock()
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.parent != nil && j.parent.WithCurrentIO != nil {
		return j.parent.WithCurrentIO(func(name, agents, workspace string) error {
			j.rebaseCurrentLocked(name, agents, workspace)
			return fn()
		})
	}
	return fn()
}

func cloneRequest(req StartRequest) StartRequest {
	req.Fork = copyMessages(req.Fork)
	if req.Descriptor != nil {
		d := *req.Descriptor
		d.Tools = append([]string(nil), d.Tools...)
		req.Descriptor = &d
	}
	return req
}

func (j *job) requestSnapshot() StartRequest {
	j.recordMu.Lock()
	defer j.recordMu.Unlock()
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.parent != nil && j.parent.WithCurrentIO != nil {
		_ = j.parent.WithCurrentIO(func(name, agents, workspace string) error { j.rebaseCurrentLocked(name, agents, workspace); return nil })
	}
	return cloneRequest(j.request)
}

func (j *job) isContinuable() bool {
	j.recordMu.Lock()
	defer j.recordMu.Unlock()
	return j.request.Descriptor != nil
}

func rebasePath(path, old, next string) string {
	if path == "" || old == "" || old == next {
		return path
	}
	rel, err := filepath.Rel(old, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return path
	}
	return filepath.Join(next, rel)
}

func workForAgents(agents string) string {
	botDir := filepath.Dir(agents)
	return work.BotDir(filepath.Dir(filepath.Dir(botDir)), filepath.Base(botDir))
}

func rebaseRequest(req *StartRequest, oldAgents, newAgents, oldWork, newWork string) {
	oldBot, newBot := filepath.Dir(oldAgents), filepath.Dir(newAgents)
	move := func(path string) string { return rebasePath(rebasePath(path, oldBot, newBot), oldWork, newWork) }
	req.Dir = move(req.Dir)
	req.JobDir = move(req.JobDir)
	req.Workspace = move(req.Workspace)
	req.Bot = filepath.Base(newBot)
	for _, prefix := range []string{"dir:", "repo:"} {
		if strings.HasPrefix(req.Resource, prefix) {
			req.Resource = prefix + move(strings.TrimPrefix(req.Resource, prefix))
			break
		}
	}
	if req.Descriptor != nil {
		req.Descriptor.Bot = req.Bot
		req.Descriptor.CWD = move(req.Descriptor.CWD)
	}
}

// recordMu and mu are held; no callback or executor lock is acquired here.
func (j *job) rebaseLocked(oldAgents, newAgents, oldWork, newWork string) {
	rebaseRequest(&j.request, oldAgents, newAgents, oldWork, newWork)
	oldBot, newBot := filepath.Dir(oldAgents), filepath.Dir(newAgents)
	move := func(path string) string { return rebasePath(rebasePath(path, oldBot, newBot), oldWork, newWork) }
	j.dir = move(j.dir)
	j.workspace = move(j.workspace)
	j.resource = j.request.Resource
	j.resultPath = move(j.resultPath)
}

func (j *job) rebaseCurrentLocked(name, agents, workspace string) {
	if agents == "" || j.dir == "" {
		return
	}
	oldAgents := filepath.Dir(j.dir)
	if filepath.Clean(oldAgents) == filepath.Clean(agents) && (name == "" || j.request.Bot == name) {
		return
	}
	j.rebaseLocked(oldAgents, agents, workForAgents(oldAgents), workspace)
	if name != "" {
		j.request.Bot = name
		if j.request.Descriptor != nil {
			j.request.Descriptor.Bot = name
		}
	}
}

func (l *Local) directoryIO(id string, fn func(string, string, string) error) error {
	io := func(name, agents, workspace string) error {
		if agents == "" {
			return ErrNotContinuable
		}
		dir := filepath.Join(agents, id)
		st, err := os.Lstat(dir)
		if err == nil && (!st.IsDir() || st.Mode()&os.ModeSymlink != 0) {
			return ErrNotFound
		}
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		return fn(name, dir, workspace)
	}
	if l.WithCurrentIO != nil {
		return l.WithCurrentIO(io)
	}
	return io(filepath.Base(filepath.Dir(l.AgentsDir)), l.AgentsDir, "")
}

// MoveBot serializes the physical move with accepted executors and their short
// storage I/O. It does not wait for model/tool execution or change admission.
func MoveBot(oldAgents, newAgents string, move func() (bool, error)) (bool, error) {
	owners.Lock()
	defer owners.Unlock()
	oldKey, newKey := canonicalDir(oldAgents), canonicalDir(newAgents)
	locals := []*Local{}
	seen := map[*Local]bool{}
	for key, l := range owners.local {
		if rebasePath(key, oldKey, newKey) != key && !seen[l] {
			seen[l] = true
			locals = append(locals, l)
		}
	}
	for _, l := range locals {
		l.mu.Lock()
	}
	defer func() {
		for i := len(locals) - 1; i >= 0; i-- {
			locals[i].mu.Unlock()
		}
	}()
	jobs := []*job{}
	for _, l := range locals {
		for _, j := range l.jobs {
			jobs = append(jobs, j)
		}
		jobs = append(jobs, l.waiting...)
	}
	for _, j := range jobs {
		j.recordMu.Lock()
		j.mu.Lock()
	}
	defer func() {
		for i := len(jobs) - 1; i >= 0; i-- {
			jobs[i].mu.Unlock()
			jobs[i].recordMu.Unlock()
		}
	}()
	moved, err := move()
	if !moved {
		return false, err
	}
	for _, j := range jobs {
		if rebasePath(canonicalDir(j.dir), oldKey, newKey) != canonicalDir(j.dir) {
			j.rebaseLocked(oldAgents, newAgents, workForAgents(oldAgents), workForAgents(newAgents))
		}
	}
	for _, l := range locals {
		l.AgentsDir = rebasePath(l.AgentsDir, oldAgents, newAgents)
	}
	for key, l := range owners.local {
		if next := rebasePath(key, oldKey, newKey); next != key {
			delete(owners.local, key)
			owners.local[next] = l
		}
	}
	return true, errors.Join(err, rebaseStoredPaths(oldAgents, newAgents))
}

func rebaseStoredPaths(oldAgents, newAgents string) error {
	dirs, err := os.ReadDir(newAgents)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var errs []error
	for _, entry := range dirs {
		if !entry.IsDir() || !idRe.MatchString(entry.Name()) {
			continue
		}
		dir := filepath.Join(newAgents, entry.Name())
		if d, e := ReadDescriptor(dir); e == nil {
			req := StartRequest{Bot: d.Bot, Descriptor: &d}
			rebaseRequest(&req, oldAgents, newAgents, workForAgents(oldAgents), workForAgents(newAgents))
			errs = append(errs, writeDescriptor(dir, *req.Descriptor))
		}
		if m, e := ReadMeta(dir); e == nil {
			m.ResultPath = rebasePath(m.ResultPath, filepath.Dir(oldAgents), filepath.Dir(newAgents))
			errs = append(errs, writeMeta(dir, m))
		}
	}
	return errors.Join(errs...)
}

func (l *Local) hasAgentsDir() bool { l.mu.Lock(); defer l.mu.Unlock(); return l.AgentsDir != "" }
