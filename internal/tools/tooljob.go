package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"xbot/internal/clip"
	"xbot/internal/fsutil"
	"xbot/internal/notable"
	"xbot/internal/review"
)

const DefaultToolWait = 2 * time.Second
const JobEventType = "shell_job"

func CallID(ctx context.Context) string {
	if lv, ok := ctx.Value(callKey{}).(*live); ok {
		return lv.snapshot().ID
	}
	return ""
}

// CallResult is the fully processed result, including images on the fast path.
// Background images are persisted by the existing owner of the job callback.
type CallResult struct {
	Text    string
	Images  []AttachedImage
	Err     error
	Job     *Job // only the background receipt has a Job
	Notable []notable.Item
}

type CallRequest struct {
	Finalize           func(Job, CallResult) (Job, error)
	Name, Command, Dir string
	Wait               time.Duration
	Background         bool
	Run                func(context.Context) CallResult
	Prepare            func(context.Context, string) (context.Context, func())
	OnDone             func(Job)
}

type callKey struct{}
type waitSignalKey struct{}

func WithWaitSignal(ctx context.Context, signal <-chan struct{}) context.Context {
	return context.WithValue(ctx, waitSignalKey{}, signal)
}

// CallWait lets shell keep its existing shorter wait and explicit background
// arguments. Every other tool uses the same default without adding arguments.
func CallWait(t Tool, args json.RawMessage) (time.Duration, bool) {
	if p, ok := t.(interface {
		WaitPolicy(json.RawMessage) (time.Duration, bool)
	}); ok {
		d, bg := p.WaitPolicy(args)
		if d > 0 && d < DefaultToolWait {
			return d, bg
		}
		return DefaultToolWait, bg
	}
	return DefaultToolWait, false
}

// RunCall separates the lifetime of a call from the foreground's wait. Only
// live.mu may choose completion, detachment, or cancellation. A detached call
// keeps the captured context values, but no longer follows the waiter's cancel.
func (j *Jobs) RunCall(ctx context.Context, req CallRequest) CallResult {
	if j == nil || j.dir() == "" || req.Run == nil {
		return CallResult{Err: errors.New("tool job: missing directory or execution")}
	}
	if err := ctx.Err(); err != nil {
		return CallResult{Err: err}
	}
	if err := os.MkdirAll(j.dir(), 0700); err != nil {
		return CallResult{Err: err}
	}
	j.mu.Lock()
	if j.jobs == nil {
		j.jobs = map[string]*live{}
	}
	if j.countLocked() >= maxShellJobs {
		j.mu.Unlock()
		return CallResult{Err: errors.New("tool job: too many background jobs")}
	}
	id, err := newJobID(j.jobs)
	if err != nil {
		j.mu.Unlock()
		return CallResult{Err: err}
	}
	path := filepath.Join(j.dir(), id+".log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		j.mu.Unlock()
		return CallResult{Err: err}
	}
	w := newJobWriter(f, j.outputCap(), j.redact)
	w.forms = j.Forms
	pid, start := CurrentProcess()
	lv := &live{job: Job{ID: id, Tool: req.Name, Command: clipCommand(j.redact(req.Command)), Status: "running", Output: path, Started: j.now(), Cwd: req.Dir, Local: true, PID: pid, ProcStart: start}, w: w, done: make(chan struct{}), detached: req.Background, onDone: req.OnDone}
	if lv.onDone == nil {
		lv.onDone = j.OnDone
	}
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	lv.cancel = cancel
	j.jobs[id] = lv
	j.mu.Unlock()
	if err := j.writeMeta(lv.snapshot()); err != nil {
		cancel()
		_ = w.Close()
		j.remove(lv)
		return CallResult{Err: err}
	}
	runCtx = context.WithValue(runCtx, callKey{}, lv)
	cleanup := func() {}
	if req.Prepare != nil {
		runCtx, cleanup = req.Prepare(runCtx, id)
	}
	lv.cleanup = cleanup
	lv.finalize = req.Finalize
	stopParent := context.AfterFunc(ctx, func() {
		lv.mu.Lock()
		if !lv.detached && !lv.finished {
			lv.killed = true
			cancel()
		}
		lv.mu.Unlock()
	})
	go func() {
		defer cancel()
		defer stopParent()

		res := func() (res CallResult) {
			defer func() {
				if r := recover(); r != nil {
					res = CallResult{Err: fmt.Errorf("tool panic: %v", r)}
				}
			}()
			if err := runCtx.Err(); err != nil {
				return CallResult{Err: err}
			}
			return req.Run(runCtx)
		}()
		j.completeCall(lv, res)
	}()
	if req.Background {
		job := lv.snapshot()
		return CallResult{Text: formatBackground(job, "started in background"), Job: &job}
	}
	wait := req.Wait
	if wait <= 0 || wait > DefaultToolWait {
		wait = DefaultToolWait
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	signal, _ := ctx.Value(waitSignalKey{}).(<-chan struct{})
	select {
	case <-lv.done:
	case <-ctx.Done():
		lv.mu.Lock()
		if !lv.finished {
			lv.killed = true
			cancel()
			lv.mu.Unlock()
			return CallResult{Err: ctx.Err()}
		}
		lv.mu.Unlock()
		<-lv.done
	case <-timer.C:
		if res, ok := j.detachCall(lv, true); ok {
			return res
		}
		<-lv.done
	case <-signal:
		if res, ok := j.detachCall(lv, false); ok {
			return res
		}
		<-lv.done
	}
	lv.mu.Lock()
	res := lv.result
	lv.mu.Unlock()
	j.remove(lv)
	return res
}

func (j *Jobs) detachCall(lv *live, timed bool) (CallResult, bool) {
	lv.mu.Lock()
	if lv.finished || lv.killed {
		lv.mu.Unlock()
		return CallResult{}, false
	}
	lv.detached = true
	lv.job.Background = true
	lv.job.TimedOut = timed
	job := lv.job
	lv.mu.Unlock()
	// Serialize with completion so a late running write cannot replace a terminal record.
	j.metaMu.Lock()
	current := lv.snapshot()
	err := j.writeMeta(current)
	j.metaMu.Unlock()
	if err != nil {
		lv.cancel()
		return CallResult{Err: err}, true
	}
	return CallResult{Text: formatBackground(job, "call continues; not killed"), Job: &job}, true
}

func (j *Jobs) completeCall(lv *live, res CallResult) {
	lv.mu.Lock()
	// This is the completion commit point; no late timer or owner word can
	// take the answer away while output and cwd are being published.
	lv.finished = true
	detached := lv.detached
	lv.result = res
	if res.Err != nil {
		lv.result.Text = "error: " + res.Err.Error()
	}
	lv.result.Text = j.redact(lv.result.Text)
	status := "done"
	if res.Err != nil || strings.HasPrefix(strings.TrimSpace(res.Text), "error:") {
		status = "failed"
	}
	if lv.killed {
		status = "cancelled"
	}
	job := lv.job
	job.Status = status
	job.Ended = j.now()
	job.Background = detached
	job.Notable = res.Notable
	onCwd, ended := lv.onCwd, lv.finalCwd
	lv.mu.Unlock()
	defer func() {
		if lv.cleanup != nil {
			lv.cleanup()
		}
		close(lv.done)
	}()
	_ = lv.w.Close()
	text := j.redact(lv.result.Text)
	res.Text = text
	res.Err = nil
	// Shell keeps its streamed log. Other tools publish their processed text
	// to the same bounded output file, never another result store.
	if !review.IsShellTool(job.Tool) {
		body, _ := clip.Bytes(text, j.outputCap())
		job.Bytes = len(body)
		if err := fsutil.WriteAtomic(job.Output, []byte(body), 0600); err != nil {
			job.Status = "failed"
			text = "保存工具结果失败：" + j.redact(err.Error())
			res.Text = text
			lv.mu.Lock()
			lv.result.Err = err
			lv.mu.Unlock()
		}
	}
	if len(text) <= 4096 {
		job.Result = text
	}
	if detached && lv.finalize != nil {
		var err error
		job, err = lv.finalize(job, res)
		if err != nil {
			job.Status = "failed"
			job.Result = "保存工具结果失败：" + j.redact(err.Error())
		}
	}
	if !detached && onCwd != nil && ended != "" {
		onCwd(ended)
	}
	if review.IsShellTool(job.Tool) {
		job.Bytes, job.Dropped = lv.w.span()
	}
	j.metaMu.Lock()
	lv.mu.Lock()
	lv.job = job
	lv.mu.Unlock()
	err := j.writeMeta(job)
	j.metaMu.Unlock()
	if err != nil {
		lv.mu.Lock()
		lv.result.Err = err
		lv.job.Status = "failed"
		lv.job.Result = "保存工具状态失败：" + j.redact(err.Error())
		job = lv.job
		lv.mu.Unlock()
	}
	if detached && lv.onDone != nil {
		lv.onDone(job)
	}
	if detached && lv.finalize != nil {
		lv.mu.Lock()
		lv.result.Images = nil
		lv.mu.Unlock()
	}
	j.prune(lv)
}

// runProcess is only the shell process adapter. It has no timer or separate
// background decision; RunCall owns both for shell and every other tool.
func (j *Jobs) runProcess(ctx context.Context, lv *live, req RunRequest) (string, error) {
	script := req.Exec
	if script == "" {
		script = req.Command
	}
	in := req.Interpreter
	if in.name == "" {
		in = interpreter{name: "sh", path: "sh"}
	}
	cmd := in.command(ctx, script)
	setGroup(cmd)
	cmd.Cancel = func() error { return killProcess(cmd) }
	cmd.Dir = req.Dir
	cmd.Env = req.Env
	cmd.Stdout = lv.w
	cmd.Stderr = lv.w
	if err := cmd.Start(); err != nil {
		if req.onStart != nil {
			req.onStart()
		}
		return "", err
	}
	lv.mu.Lock()
	lv.cmd = cmd
	lv.job.Command = clipCommand(j.redact(req.Command))
	lv.job.Description = clipCommand(j.redact(req.Description))
	lv.job.PID = cmd.Process.Pid
	lv.job.ProcStart = processStartTicks(cmd.Process.Pid)
	lv.mu.Unlock()
	if req.onStart != nil {
		req.onStart()
	}
	err := cmd.Wait()
	if closeErr := lv.w.Close(); closeErr != nil {
		return "", closeErr
	}
	code := exitCode(err)
	lv.mu.Lock()
	lv.job.ExitCode = code
	if req.CwdFile != "" {
		lv.finalCwd = readCwdFile(req.CwdFile)
	}
	if !lv.detached && req.OnCwd != nil {
		lv.onCwd = req.OnCwd
	}
	lv.mu.Unlock()
	if req.CwdFile != "" {
		_ = os.Remove(req.CwdFile)
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		return "", err
	}
	body, readErr := os.ReadFile(lv.snapshot().Output)
	if readErr != nil {
		return "", readErr
	}
	max := req.MaxOutput
	if max <= 0 {
		max = maxShellOutput
	}
	text, truncated := clip.Bytes(clip.Valid(string(body)), max)
	return formatShell(fmt.Sprintf("exit %d", code), strings.TrimRight(text, "\n"), truncated), nil
}
