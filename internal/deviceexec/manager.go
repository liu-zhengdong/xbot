package deviceexec

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"xbot/internal/browser"
	"xbot/internal/desktop"
	"xbot/internal/fsutil"
	"xbot/internal/tools"
	"xbot/internal/vault"
)

// Manager owns the original tools and Jobs for each foreign execution context.
// Configure Desktop and Browser before the first call to inject an existing link.
type Manager struct {
	Root           string
	DeviceID       string
	DeviceName     string
	targetDeviceID string
	Desktop        *desktop.Link
	Browser        *browser.Link
	mu             sync.Mutex
	contexts       map[string]*executionContext
	records        map[string]*record
	closed         bool
	browserLock    chan struct{}
}

type executionContext struct {
	source       Source
	dir          string
	jobs         *tools.Jobs
	tools        map[string]tools.Tool
	defaultShell string
	mu           sync.Mutex
	cwd          string
}

// record only maps a stable reference to an original Job; it never schedules work.
type record struct {
	Source      Source         `json:"source"`
	Ref         string         `json:"ref"`
	Fingerprint string         `json:"fingerprint"`
	Kind        string         `json:"kind"`
	Description string         `json:"description"`
	ID          string         `json:"id"`
	Started     time.Time      `json:"started"`
	Artifacts   []artifactFile `json:"artifacts"`
	Failure     string         `json:"failure,omitempty"`
	Completed   *tools.Job     `json:"completed,omitempty"`
}

type artifactFile struct {
	Artifact
	Path string `json:"path"`
}

func New(root, deviceID, deviceName string) (*Manager, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	m := &Manager{Root: root, DeviceID: deviceID, DeviceName: deviceName, targetDeviceID: deviceID, Desktop: desktop.Open(root), Browser: browser.Open(root), contexts: map[string]*executionContext{}, records: map[string]*record{}, browserLock: make(chan struct{}, 1)}
	entries, err := os.ReadDir(filepath.Join(root, "deviceexec", "calls"))
	if errors.Is(err, os.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(root, "deviceexec", "calls", entry.Name()))
		if err != nil {
			return nil, err
		}
		var rec record
		if err = json.Unmarshal(body, &rec); err != nil {
			return nil, err
		}
		if _, err = m.contextLocked(rec.Source); err != nil {
			return nil, err
		}
		m.records[callKey(rec.Source, rec.Ref)] = &rec
	}
	return m, nil
}

// SetDeviceName updates the authoritative account display name for task projections.
func (m *Manager) SetDeviceName(name string) {
	m.mu.Lock()
	m.DeviceName = name
	m.mu.Unlock()
}

func digest(v any) string {
	body, _ := json.Marshal(v)
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func contextKey(source Source) string {
	return digest([]string{source.Account, source.DeviceID, source.Bot, source.ContextID})
}
func callKey(source Source, ref string) string { return digest([]string{contextKey(source), ref}) }

func (m *Manager) save(rec *record) error {
	dir := filepath.Join(m.Root, "deviceexec", "calls")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	body, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return fsutil.WriteAtomic(filepath.Join(dir, callKey(rec.Source, rec.Ref)+".json"), body, 0600)
}

func (c *executionContext) directory() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if st, err := os.Stat(c.cwd); err != nil || !st.IsDir() {
		c.cwd = c.dir
	}
	return c.cwd
}

func (m *Manager) contextLocked(source Source) (*executionContext, error) {
	key := contextKey(source)
	if c := m.contexts[key]; c != nil {
		return c, nil
	}
	base := filepath.Join(m.Root, "deviceexec", "contexts", key)
	dir := filepath.Join(base, "work")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	jobs := tools.NewJobs(filepath.Join(base, "jobs"))
	jobs.Redact = vault.Redact
	jobs.Forms = vault.RegisteredForms
	if err := jobs.Load(); err != nil {
		return nil, err
	}
	c := &executionContext{source: source, dir: dir, cwd: dir, jobs: jobs, tools: map[string]tools.Tool{}}
	if body, err := os.ReadFile(filepath.Join(base, "cwd")); err == nil && len(body) > 0 {
		c.cwd = string(body)
	}
	for _, sh := range tools.NewShells() {
		if c.defaultShell == "" {
			c.defaultShell = sh.Name()
		}
		sh.Dir = dir
		sh.BindJobs(jobs)
		sh.PersistBackgroundCwd = true
		sh.SetMarkerDir(dir)
		sh.BindSession(func(context.Context) (string, string) { return c.directory(), "" }, func(_ context.Context, path string) (string, string) {
			c.mu.Lock()
			defer c.mu.Unlock()
			c.cwd = path
			if err := fsutil.WriteAtomic(filepath.Join(base, "cwd"), []byte(path), 0600); err != nil {
				return path, "could not persist cwd: " + err.Error()
			}
			return path, ""
		})
		sh.BindEnvironment(func(env []string) []string {
			out := make([]string, 0, len(env))
			for _, v := range env {
				name, _, _ := strings.Cut(v, "=")
				switch name {
				case "XBOT_ENDPOINT", "XBOT_CALL_ID", "XBOT_BOT", "XBOT_ROOT":
					continue
				}
				out = append(out, v)
			}
			return out
		})
		c.tools[sh.Name()] = sh
	}
	read := &tools.ReadFile{Root: dir}
	read.BindRoot(func(context.Context) string { return c.directory() })
	write := &tools.WriteFile{Root: dir}
	write.BindRoot(func(context.Context) string { return c.directory() })
	edit := &tools.EditFile{Root: dir}
	edit.BindRoot(func(context.Context) string { return c.directory() })
	for _, tool := range []tools.Tool{read, write, edit} {
		c.tools[tool.Name()] = tool
	}
	m.contexts[key] = c
	return c, nil
}

func validate(source Source, call Call) error {
	if source.Owner || source.Account == "" || source.DeviceID == "" || source.Bot == "" || source.ContextID == "" || call.Ref == "" || len(call.Ref) > 128 {
		return ErrInvalid
	}
	if len(call.Description) > 1024 || len(call.Steps) > 32 || len(call.Artifacts) > 32 {
		return ErrInvalid
	}
	if len(call.Steps) > 0 && (call.Kind != "desktop" || call.Tool != "" || len(call.Args) > 0) {
		return ErrInvalid
	}
	ops := call.Steps
	if len(ops) == 0 {
		ops = []Operation{{Tool: call.Tool, Args: call.Args}}
	}
	for _, op := range ops {
		if len(op.Args) == 0 || !json.Valid(op.Args) || strings.TrimSpace(string(op.Args))[0] != '{' {
			return ErrInvalid
		}
		readonly := false
		allowed := false
		switch call.Kind {
		case "exec":
			allowed = op.Tool == "" || op.Tool == "sh" || op.Tool == "bash" || op.Tool == "zsh" || op.Tool == "powershell" || op.Tool == "cmd" || op.Tool == "git-bash"
		case "read":
			allowed = op.Tool == "read_file"
			readonly = allowed
		case "write":
			allowed = op.Tool == "write_file" || op.Tool == "edit_file"
		case "desktop":
			switch op.Tool {
			case "desktop_snapshot", "desktop_screenshot":
				allowed = true
				readonly = true
			case "desktop_click", "desktop_type", "desktop_set_value", "desktop_scroll", "desktop_key":
				allowed = true
			}
		case "browser":
			switch op.Tool {
			case "browser_snapshot":
				allowed = true
				readonly = true
			case "browser_open", "browser_click", "browser_stop":
				allowed = true
			}
		}
		if !allowed {
			return ErrInvalid
		}
		if source.Readonly && !readonly {
			return ErrReadonly
		}
	}
	body, err := json.Marshal(call)
	if err != nil || len(body) > MaxRequestBytes {
		return ErrInvalid
	}
	return nil
}

// Submit admits a call once and immediately returns the original Job receipt.
// An intent is persisted before admission so a crash never permits a blind replay.
func (m *Manager) Submit(ctx context.Context, source Source, call Call) (Task, error) {
	if err := validate(source, call); err != nil {
		return Task{}, err
	}
	if err := ctx.Err(); err != nil {
		return Task{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Task{}, errors.New("device execution manager closed")
	}
	key := callKey(source, call.Ref)
	fingerprint := digest(call)
	if rec := m.records[key]; rec != nil {
		if rec.Fingerprint != fingerprint {
			return Task{}, ErrConflict
		}
		return m.taskLocked(rec), nil
	}
	for _, rec := range m.records {
		if rec.Ref == call.Ref {
			return Task{}, ErrConflict
		}
	}
	c, err := m.contextLocked(source)
	if err != nil {
		return Task{}, err
	}
	ops := append([]Operation(nil), call.Steps...)
	if len(ops) == 0 {
		ops = []Operation{{Tool: call.Tool, Args: append(json.RawMessage(nil), call.Args...)}}
	}
	available := make(map[string]tools.Tool, len(c.tools)+12)
	for name, t := range c.tools {
		available[name] = t
	}
	for _, t := range m.Desktop.Tools() {
		available[t.Name()] = t
	}
	for _, t := range m.Browser.Tools() {
		available[t.Name()] = t
	}
	for i := range ops {
		ops[i].Args = append(json.RawMessage(nil), ops[i].Args...)
		if call.Kind == "exec" {
			if ops[i].Tool == "" {
				ops[i].Tool = c.defaultShell
			}
			var args map[string]json.RawMessage
			_ = json.Unmarshal(ops[i].Args, &args)
			if _, ok := args["description"]; !ok && call.Description != "" {
				args["description"], _ = json.Marshal(call.Description)
				ops[i].Args, _ = json.Marshal(args)
			}
		}
		if available[ops[i].Tool] == nil {
			return Task{}, fmt.Errorf("device capability unavailable: %s", ops[i].Tool)
		}
	}
	rec := &record{Source: source, Ref: call.Ref, Fingerprint: fingerprint, Kind: call.Kind, Description: vault.Redact(call.Description), Started: time.Now(), Artifacts: []artifactFile{}}
	if err = m.save(rec); err != nil {
		return Task{}, err
	}
	m.records[key] = rec
	var prepareErr error
	name := ops[0].Tool
	if len(call.Steps) > 0 {
		name = "device_desktop_sequence"
	}
	result := c.jobs.RunCall(ctx, tools.CallRequest{Name: name, Command: rec.Description, Dir: c.directory(), Background: true,
		Finalize: func(job tools.Job, result tools.CallResult) (tools.Job, error) {
			if call.Kind == "exec" && job.Status == "done" {
				line, _, _ := strings.Cut(result.Text, "\n")
				if strings.HasPrefix(line, "exit ") && line != "exit 0" || strings.HasPrefix(line, "timed out after ") {
					job.Status = "failed"
					job.Result = line
				}
			}
			return job, nil
		},
		OnDone: func(job tools.Job) {
			m.mu.Lock()
			rec.Completed = &job
			_ = m.save(rec)
			m.mu.Unlock()
		},
		Prepare: func(ctx context.Context, id string) (context.Context, func()) {
			rec.ID = id
			prepareErr = m.save(rec)
			return WithSource(ctx, source), func() {}
		},
		Run: func(ctx context.Context) tools.CallResult {
			if prepareErr != nil {
				return tools.CallResult{Err: prepareErr}
			}
			var lock chan struct{}
			if call.Kind == "browser" {
				lock = m.browserLock
			}
			if lock != nil {
				select {
				case lock <- struct{}{}:
					defer func() { <-lock }()
				case <-ctx.Done():
					return tools.CallResult{Err: ctx.Err()}
				}
			}
			var output strings.Builder
			paths := append([]string(nil), call.Artifacts...)
			runOperations := func(operationCtx context.Context) error {
				for _, op := range ops {
					if err := operationCtx.Err(); err != nil {
						return err
					}
					text, err := available[op.Tool].Run(operationCtx, op.Args)
					if output.Len() > 0 {
						output.WriteByte('\n')
					}
					output.WriteString(text)
					if err != nil {
						return err
					}
					switch op.Tool {
					case "desktop_screenshot":
						paths = append(paths, strings.TrimSpace(text))
					case "write_file", "edit_file":
						if p, ok := available[op.Tool].(tools.PathContext); ok {
							paths = append(paths, p.ResolvedPathContext(operationCtx, op.Args))
						}
					}
				}
				return nil
			}
			var operationErr error
			if call.Kind == "desktop" {
				operationErr = desktop.Sequence(ctx, runOperations)
			} else {
				operationErr = runOperations(ctx)
			}
			if operationErr != nil {
				return tools.CallResult{Text: output.String(), Err: operationErr}
			}
			artifacts := make([]artifactFile, 0, len(paths))
			seen := map[string]bool{}
			for _, path := range paths {
				resolved, err := tools.ResolveRoot(c.directory(), path)
				if err != nil {
					return tools.CallResult{Text: output.String(), Err: err}
				}
				if seen[resolved] {
					continue
				}
				seen[resolved] = true
				st, err := os.Stat(resolved)
				if err != nil {
					return tools.CallResult{Text: output.String(), Err: err}
				}
				if !st.Mode().IsRegular() {
					return tools.CallResult{Text: output.String(), Err: errors.New("artifact is not a regular file")}
				}
				artifacts = append(artifacts, artifactFile{Artifact: Artifact{ID: fmt.Sprintf("a%d", len(artifacts)+1), Name: filepath.Base(resolved), Size: st.Size()}, Path: resolved})
			}
			m.mu.Lock()
			rec.Artifacts = artifacts
			err := m.save(rec)
			m.mu.Unlock()
			return tools.CallResult{Text: output.String(), Err: err}
		},
	})
	if result.Err != nil {
		rec.Failure = vault.Redact(result.Err.Error())
		_ = m.save(rec)
		return m.taskLocked(rec), result.Err
	}
	if prepareErr != nil {
		return m.taskLocked(rec), prepareErr
	}
	return m.taskLocked(rec), nil
}

func (m *Manager) taskLocked(rec *record) Task {
	task := Task{ID: rec.ID, Ref: rec.Ref, DeviceID: m.targetDeviceID, DeviceName: m.DeviceName, Bot: rec.Source.Bot, ContextID: rec.Source.ContextID, Kind: rec.Kind, Description: rec.Description, Status: "failed", StartedAt: rec.Started, Error: rec.Failure, Artifacts: []Artifact{}}
	for _, a := range rec.Artifacts {
		task.Artifacts = append(task.Artifacts, a.Artifact)
	}
	c := m.contexts[contextKey(rec.Source)]
	var job tools.Job
	found := rec.Completed != nil
	if found {
		job = *rec.Completed
	}
	if c != nil && rec.ID != "" {
		if current, err := c.jobs.Status(rec.ID); err == nil {
			job = current
			found = true
		}
	}
	if found {
		task.StartedAt = job.Started
		if !job.Ended.IsZero() {
			ended := job.Ended
			task.FinishedAt = &ended
		}
		switch job.Status {
		case "running":
			task.Status = "running"
		case "done":
			task.Status = "completed"
		case "cancelled":
			task.Status = "cancelled"
		default:
			task.Status = "failed"
		}
		if job.ExitCode != 0 && task.Status == "completed" {
			task.Status = "failed"
			task.Error = fmt.Sprintf("exit %d", job.ExitCode)
		}
		if task.Status == "failed" && task.Error == "" {
			task.Error = job.Result
			if task.Error == "" {
				task.Error = "execution result unavailable"
			}
		}
		return task
	}
	if task.Error == "" {
		task.Error = "execution acknowledgement unavailable; call was not replayed"
	}
	return task
}

// List returns at most 200 recent calls with optional field filters.
// Nil lists recent calls across contexts for the trusted owner UI.
func (m *Manager) List(filter *Source) []Task {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Task{}
	for _, rec := range m.records {
		s := rec.Source
		if filter != nil && ((filter.Account != "" && filter.Account != s.Account) || (!filter.Owner && ((filter.DeviceID != "" && filter.DeviceID != s.DeviceID) || (filter.Bot != "" && filter.Bot != s.Bot) || (filter.ContextID != "" && filter.ContextID != s.ContextID)))) {
			continue
		}
		out = append(out, m.taskLocked(rec))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.Before(out[j].StartedAt) })
	if len(out) > 200 {
		out = out[len(out)-200:]
	}
	return out
}

// RunningCount counts all original live Jobs, independently of the recent task list.
func (m *Manager) RunningCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, c := range m.contexts {
		for _, job := range c.jobs.List() {
			if job.Status == "running" {
				count++
			}
		}
	}
	return count
}

func (m *Manager) lookupLocked(source Source, ref string) (*record, error) {
	if !source.Owner {
		if rec := m.records[callKey(source, ref)]; rec != nil {
			return rec, nil
		}
		return nil, ErrNotFound
	}
	if source.Account == "" {
		return nil, ErrNotFound
	}
	var found *record
	for _, rec := range m.records {
		if rec.Source.Account == source.Account && rec.Ref == ref {
			if found != nil {
				return nil, ErrConflict
			}
			found = rec
		}
	}
	if found == nil {
		return nil, ErrNotFound
	}
	return found, nil
}

func (m *Manager) Detail(source Source, ref string, offset int64, limit int) (Detail, error) {
	if offset < 0 {
		return Detail{}, ErrInvalid
	}
	if limit <= 0 {
		limit = DefaultOutputLimit
	}
	if limit > MaxOutputLimit {
		limit = MaxOutputLimit
	}
	m.mu.Lock()
	rec, err := m.lookupLocked(source, ref)
	if err != nil {
		m.mu.Unlock()
		return Detail{}, err
	}
	detail := Detail{Task: m.taskLocked(rec), Offset: offset, NextOffset: offset}
	c := m.contexts[contextKey(rec.Source)]
	id := rec.ID
	completed := rec.Completed
	m.mu.Unlock()
	if id == "" {
		return detail, nil
	}
	job, err := c.jobs.Status(id)
	if err != nil {
		if completed == nil {
			return detail, nil
		}
		job = *completed
	}
	detail.Total = int64(job.Bytes)
	detail.DroppedBefore = int64(job.Dropped)
	if offset < int64(job.Dropped) {
		offset = int64(job.Dropped)
	}
	fileOffset := offset - int64(job.Dropped)
	lookbehind := min(fileOffset, int64(utf8.UTFMax-1))
	chunk, err := readChunk(job.Output, fileOffset-lookbehind, limit+utf8.UTFMax+int(lookbehind))
	if errors.Is(err, os.ErrNotExist) {
		detail.OutputExpired = true
		return detail, nil
	}
	if err != nil {
		return Detail{}, err
	}
	start := min(int(lookbehind), len(chunk.Data))
	for i := 0; i < start; i++ {
		_, size := utf8.DecodeRune(chunk.Data[i:])
		if size > 1 && i+size > start {
			return Detail{}, fmt.Errorf("%w: output offset splits a UTF-8 code point", ErrInvalid)
		}
	}
	raw := chunk.Data[start:]
	consumed := 0
	var normalized strings.Builder
	for consumed < len(raw) {
		r, size := utf8.DecodeRune(raw[consumed:])
		if normalized.Len()+utf8.RuneLen(r) > limit {
			break
		}
		normalized.WriteRune(r)
		consumed += size
	}
	if consumed == 0 && len(raw) > 0 {
		return Detail{}, fmt.Errorf("%w: output limit cannot hold the first normalized code point", ErrInvalid)
	}
	detail.Output = vault.Redact(normalized.String())
	for len(detail.Output) > limit {
		_, size := utf8.DecodeLastRune(raw[:consumed])
		consumed -= size
		if consumed == 0 {
			return Detail{}, fmt.Errorf("%w: output limit cannot hold redacted text", ErrInvalid)
		}
		detail.Output = vault.Redact(string([]rune(string(raw[:consumed]))))
	}
	detail.NextOffset = offset + int64(consumed)
	detail.Total = max(detail.Total, int64(job.Dropped)+chunk.Total)
	return detail, nil
}

func (m *Manager) Stop(source Source, ref string) (Task, error) {
	m.mu.Lock()
	rec, err := m.lookupLocked(source, ref)
	if err != nil {
		m.mu.Unlock()
		return Task{}, err
	}
	task := m.taskLocked(rec)
	if task.Status != "running" {
		m.mu.Unlock()
		return task, nil
	}
	c := m.contexts[contextKey(rec.Source)]
	id := rec.ID
	m.mu.Unlock()
	if id != "" {
		if _, err := c.jobs.Cancel(id); err != nil {
			return Task{}, err
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.taskLocked(rec), nil
}

func readChunk(path string, offset int64, limit int) (Chunk, error) {
	f, err := os.Open(path)
	if err != nil {
		return Chunk{}, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return Chunk{}, err
	}
	if !st.Mode().IsRegular() {
		return Chunk{}, ErrInvalid
	}
	if offset > st.Size() {
		offset = st.Size()
	}
	remaining := st.Size() - offset
	if int64(limit) > remaining {
		limit = int(remaining)
	}
	body := make([]byte, limit)
	n, err := f.ReadAt(body, offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return Chunk{}, err
	}
	return Chunk{Data: body[:n], Offset: offset, NextOffset: offset + int64(n), Total: st.Size()}, nil
}

func (m *Manager) Download(source Source, ref, artifactID string, offset int64, limit int) (Chunk, error) {
	if offset < 0 {
		return Chunk{}, ErrInvalid
	}
	if limit <= 0 || limit > MaxArtifactChunk {
		limit = MaxArtifactChunk
	}
	m.mu.Lock()
	rec, err := m.lookupLocked(source, ref)
	if err != nil {
		m.mu.Unlock()
		return Chunk{}, err
	}
	path := ""
	for _, a := range rec.Artifacts {
		if a.ID == artifactID {
			path = a.Path
			break
		}
	}
	m.mu.Unlock()
	if path == "" {
		return Chunk{}, ErrNotFound
	}
	return readChunk(path, offset, limit)
}

func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	contexts := make([]*executionContext, 0, len(m.contexts))
	for _, c := range m.contexts {
		contexts = append(contexts, c)
	}
	m.mu.Unlock()
	for _, c := range contexts {
		c.jobs.Close()
	}
}
