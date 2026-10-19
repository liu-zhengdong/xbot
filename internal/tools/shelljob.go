package tools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"

	"xbot/internal/clip"
	"xbot/internal/fsutil"
)

const (
	// maxShellJobs is how many commands one bot may have in flight. A foreground
	// command counts until it exits or moves to the background.
	maxShellJobs = 32
	// defaultJobBytes is the tail kept in a background job's output file.
	defaultJobBytes = 256 << 10
	// maxJobRead is the most output one shell_output call returns.
	maxJobRead = 32 << 10
	// maxJobTail is the most lines a tail read returns.
	maxJobTail = 200
	// maxKeptJobs is how many finished jobs one bot keeps on disk.
	maxKeptJobs = 20
	// maxJobAge is how long a finished job is kept. Running jobs are not aged out.
	maxJobAge = 7 * 24 * time.Hour
)

// Job is one shell command tracked for this process. Status is running, done,
// cancelled, or lost. lost means a new process found the record still running
// and did not try to signal the old pid.
type Job struct {
	ID        string    `json:"id"`
	Command   string    `json:"command"`
	Status    string    `json:"status"`
	ExitCode  int       `json:"exit_code,omitempty"`
	PID       int       `json:"pid,omitempty"`
	Output    string    `json:"output"`
	Started   time.Time `json:"started"`
	Ended     time.Time `json:"ended,omitempty"`
	TimedOut  bool      `json:"timed_out,omitempty"`
	Notified  bool      `json:"notified,omitempty"`
	ProcStart int64     `json:"proc_start,omitempty"`
	// Bytes is the redacted stream length. Dropped is the prefix no longer in the file.
	Bytes   int `json:"bytes,omitempty"`
	Dropped int `json:"dropped,omitempty"`
	// Held is raw bytes still in memory because they might begin a secret.
	// It is live state, not stored: a restart has already lost that tail.
	Held int `json:"-"`
	// Cwd is the directory the command started in.
	Cwd string `json:"cwd,omitempty"`
	// Local is true only for a job this process started. It is not stored.
	Local bool `json:"-"`
}

// RunRequest is one shell invocation that may outlive its timeout.
// Exec is the script actually run. Empty means Command. CwdFile is removed
// when the process exits; OnCwd receives the directory a foreground command
// finished in, before Run returns.
type RunRequest struct {
	Command    string
	Exec       string
	Timeout    time.Duration
	MaxOutput  int
	Dir        string
	Env        []string
	Background bool
	CwdFile    string
	OnCwd      func(string)
}

// Jobs tracks shell commands for one bot directory. The same value lives for
// the serve process, so a job started in one turn is still there on the next.
// A new Jobs on the same directory, after Load, marks anything still running
// as lost and does not signal its pid.
type Jobs struct {
	Dir      string
	Redact   func(string) string
	OnDone   func(Job)
	Now      func() time.Time
	MaxBytes int
	// Forms lists the secret strings Redact replaces, including encoded forms.
	// The writer holds a suffix of pending output only when that suffix is a
	// prefix of one of these. Nil means nothing is held for a secret.
	Forms func() []string

	mu     sync.Mutex
	metaMu sync.Mutex
	jobs   map[string]*live
}

type live struct {
	mu       sync.Mutex
	job      Job
	cmd      *exec.Cmd
	w        *jobWriter
	done     chan struct{}
	detached bool
	killed   bool
	finished bool
	waitErr  error
	cwdFile  string
	onCwd    func(string)
	// claimCwd is set when finish still owns the session directory. Run's
	// timeout path waits for done instead of detaching once this is set, so
	// onCwd cannot run after Run has returned.
	claimCwd bool
}

// NewJobs records jobs under dir. Call Load before Run so a restarted process
// does not treat an old running record as live.
func NewJobs(dir string) *Jobs {
	return &Jobs{Dir: dir, jobs: map[string]*live{}}
}

func (j *Jobs) now() time.Time {
	if j != nil && j.Now != nil {
		return j.Now()
	}
	return time.Now()
}

func (j *Jobs) redact(s string) string {
	if j == nil || j.Redact == nil || s == "" {
		return s
	}
	return j.Redact(s)
}

func (j *Jobs) outputCap() int {
	if j != nil && j.MaxBytes > 0 {
		return j.MaxBytes
	}
	return defaultJobBytes
}

// Load reads job records. A record still marked running is lost, unless its
// pid and proc_start still name a live process: that job belongs to a serve
// that is still up, and this process must not rewrite it or signal it.
// Loaded jobs are not Local. Finished jobs older than maxJobAge, and finished
// jobs past maxKeptJobs, are removed.
func (j *Jobs) Load() error {
	if j == nil || j.Dir == "" {
		return errors.New("shell: no job directory")
	}
	if err := os.MkdirAll(j.Dir, 0700); err != nil {
		return err
	}
	entries, err := os.ReadDir(j.Dir)
	if err != nil {
		return err
	}
	j.mu.Lock()
	if j.jobs == nil {
		j.jobs = map[string]*live{}
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		id := strings.TrimSuffix(name, ".json")
		if !validJobID(id) {
			continue
		}
		if _, ok := j.jobs[id]; ok {
			continue
		}
		job, err := j.readMeta(id)
		if err != nil {
			continue
		}
		if job.Status == "running" {
			if sameProcess(job.PID, job.ProcStart) {
				continue
			}
			job.Status = "lost"
			job.Ended = j.now()
			if err := j.writeMeta(job); err != nil {
				j.mu.Unlock()
				return err
			}
		}
		j.jobs[id] = &live{job: job, done: closedChan()}
	}
	j.mu.Unlock()
	j.prune()
	return nil
}

// Run starts command. Background returns as soon as the process is running.
// Otherwise it waits for Timeout. On timeout a command that is not an obvious
// sleep or poll is left running and the result names the job; the process is
// not killed. ctx ending before that still kills the process.
func (j *Jobs) Run(ctx context.Context, req RunRequest) (string, error) {
	if j == nil || j.Dir == "" {
		return "", errors.New("shell: no job directory")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = defaultShellTimeout
	}
	lv, err := j.start(req, req.Background)
	if err != nil {
		return "", err
	}
	if req.Background {
		return formatBackground(lv.snapshot(), "started in background"), nil
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-lv.done:
		return j.finishForeground(lv, req.MaxOutput, "")
	case <-ctx.Done():
		j.kill(lv)
		<-lv.done
		j.remove(lv)
		return "", ctx.Err()
	case <-timer.C:
		lv.mu.Lock()
		if lv.finished || lv.claimCwd {
			lv.mu.Unlock()
			<-lv.done
			return j.finishForeground(lv, req.MaxOutput, "")
		}
		if skipAutoBackground(req.Command) {
			lv.mu.Unlock()
			j.kill(lv)
			<-lv.done
			return j.finishForeground(lv, req.MaxOutput, "timed out after "+timeout.String())
		}
		lv.detached = true
		lv.job.TimedOut = true
		id, path, cwd := lv.job.ID, lv.job.Output, lv.job.Cwd
		lv.mu.Unlock()
		if err := j.publish(lv); err != nil {
			return "", err
		}
		return formatBackground(Job{ID: id, Output: path, Cwd: cwd}, "still running after "+timeout.String()+"; not killed"), nil
	}
}

// Output reads a job's output. offset is a byte offset into the redacted
// stream, not into the retained file. tail > 0 returns the last tail lines.
// When the retained file no longer has the requested prefix, the result says
// how much was dropped. The result is capped.
func (j *Jobs) Output(id string, offset, tail int) (string, error) {
	lv := j.get(id)
	if lv == nil {
		return "", fmt.Errorf("shell: no such job %s", id)
	}
	job := lv.snapshot()
	var written, discarded, held int
	if lv.w != nil {
		_ = lv.w.Flush()
		written, discarded = lv.w.span()
		held = lv.w.held()
	} else {
		written, discarded = job.Bytes, job.Dropped
	}
	body, err := os.ReadFile(job.Output)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if written < discarded+len(body) {
		written = discarded + len(body)
	}
	if offset < 0 {
		offset = 0
	}
	droppedNote := false
	start := offset
	if tail > 0 {
		if tail > maxJobTail {
			tail = maxJobTail
		}
		start = discarded
		droppedNote = discarded > 0
	} else if start < discarded {
		start = discarded
		droppedNote = true
	}
	fileOff := start - discarded
	if fileOff < 0 {
		fileOff = 0
	}
	if fileOff > len(body) {
		fileOff = len(body)
	}
	var chunk string
	next := start
	truncated := false
	if tail > 0 {
		chunk = tailLines(string(body), tail)
		next = written
	} else {
		chunk = string(body[fileOff:])
		next = start + len(chunk)
		// A replaced tail must not pull next_offset backward.
		if fileOff+len(chunk) >= len(body) && next < written {
			next = written
		}
	}
	if len(chunk) > maxJobRead {
		chunk = chunk[:maxJobRead]
		truncated = true
		if tail <= 0 {
			next = start + len(chunk)
		}
	}
	chunk = clip.Valid(chunk)
	// The file was redacted as it was written. A value the vault learned since
	// is caught here; offsets still count the stream as written.
	chunk = j.redact(chunk)
	var b strings.Builder
	fmt.Fprintf(&b, "status: %s\noffset: %d\nnext_offset: %d\n", job.Status, offset, next)
	if held > 0 {
		fmt.Fprintf(&b, "held_bytes: %d\n", held)
	}
	if droppedNote {
		fmt.Fprintf(&b, "dropped_before: %d\n[dropped] output before byte %d is gone\n", discarded, discarded)
	}
	if chunk != "" {
		b.WriteString(strings.TrimRight(chunk, "\n"))
		b.WriteByte('\n')
	}
	if truncated {
		b.WriteString("[truncated]\n")
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// MarkNotified records that this job was put on the reminder channel or woken.
// A later process will not announce it again.
func (j *Jobs) MarkNotified(id string) {
	lv := j.get(id)
	if lv == nil {
		return
	}
	j.metaMu.Lock()
	defer j.metaMu.Unlock()
	lv.mu.Lock()
	if lv.job.Notified {
		lv.mu.Unlock()
		return
	}
	lv.job.Notified = true
	job := lv.job
	lv.mu.Unlock()
	_ = j.writeMeta(job)
}

// Shutdown kills every process group this Jobs value still owns. It does not
// signal a job that was only loaded from disk.
func (j *Jobs) Shutdown() {
	if j == nil {
		return
	}
	j.mu.Lock()
	var owned []*live
	for _, lv := range j.jobs {
		job := lv.snapshot()
		if job.Local && job.Status == "running" && lv.cmd != nil && lv.cmd.Process != nil {
			owned = append(owned, lv)
		}
	}
	j.mu.Unlock()
	for _, lv := range owned {
		j.kill(lv)
	}
	for _, lv := range owned {
		<-lv.done
	}
}

// Status returns one job. An unknown id is an error. Bytes and Held for a
// running job come from the live writer, not the record on disk.
func (j *Jobs) Status(id string) (Job, error) {
	lv := j.get(id)
	if lv == nil {
		return Job{}, fmt.Errorf("shell: no such job %s", id)
	}
	return j.freshen(lv), nil
}

// List returns every job this process still tracks, oldest first.
func (j *Jobs) List() []Job {
	if j == nil {
		return nil
	}
	j.mu.Lock()
	lives := make([]*live, 0, len(j.jobs))
	for _, lv := range j.jobs {
		lives = append(lives, lv)
	}
	j.mu.Unlock()
	out := make([]Job, 0, len(lives))
	for _, lv := range lives {
		out = append(out, j.freshen(lv))
	}
	sortJobs(out)
	return out
}

// freshen fills Bytes and Held from the writer while the job is still running.
func (j *Jobs) freshen(lv *live) Job {
	job := lv.snapshot()
	if lv == nil || lv.w == nil || job.Status != "running" {
		return job
	}
	if err := lv.w.Flush(); err != nil {
		return job
	}
	written, dropped := lv.w.span()
	job.Bytes = written
	job.Dropped = dropped
	job.Held = lv.w.held()
	return job
}

// Cancel kills the job's process group. A job that is not running is reported
// as it is. A lost job has no process in this serve, so it is not signalled.
func (j *Jobs) Cancel(id string) (string, error) {
	lv := j.get(id)
	if lv == nil {
		return "", fmt.Errorf("shell: no such job %s", id)
	}
	job := lv.snapshot()
	if job.Status != "running" {
		// A job that has just exited may still be writing its record and
		// running OnDone. A loaded job's done is already closed.
		<-lv.done
		return "status " + job.Status, nil
	}
	if lv.cmd == nil || lv.cmd.Process == nil {
		return "status " + job.Status, nil
	}
	j.kill(lv)
	<-lv.done
	return "cancelled " + id, nil
}

func (j *Jobs) start(req RunRequest, detach bool) (*live, error) {
	if err := os.MkdirAll(j.Dir, 0700); err != nil {
		return nil, err
	}
	j.mu.Lock()
	if j.jobs == nil {
		j.jobs = map[string]*live{}
	}
	if j.countLocked() >= maxShellJobs {
		j.mu.Unlock()
		return nil, errors.New("shell: too many background jobs")
	}
	id, err := newJobID(j.jobs)
	if err != nil {
		j.mu.Unlock()
		return nil, err
	}
	path := filepath.Join(j.Dir, id+".log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0600)
	if err != nil {
		j.mu.Unlock()
		return nil, err
	}
	w := newJobWriter(f, j.outputCap(), j.redact)
	w.forms = j.Forms
	script := req.Exec
	if script == "" {
		script = req.Command
	}
	cmd := exec.Command("sh", "-c", script)
	setGroup(cmd)
	if req.Dir != "" {
		cmd.Dir = req.Dir
	}
	if req.Env != nil {
		cmd.Env = req.Env
	}
	cmd.Stdout = w
	cmd.Stderr = w
	lv := &live{
		job: Job{
			ID:      id,
			Command: clipCommand(j.redact(req.Command)),
			Status:  "running",
			Output:  path,
			Started: j.now(),
			Cwd:     req.Dir,
		},
		cwdFile:  req.CwdFile,
		onCwd:    req.OnCwd,
		cmd:      cmd,
		w:        w,
		done:     make(chan struct{}),
		detached: detach,
	}
	lv.job.Local = true
	j.jobs[id] = lv
	j.mu.Unlock()
	if err := cmd.Start(); err != nil {
		_ = w.Close()
		j.remove(lv)
		return nil, fmt.Errorf("shell: %w", err)
	}
	lv.mu.Lock()
	lv.job.PID = cmd.Process.Pid
	lv.job.ProcStart = processStartTicks(cmd.Process.Pid)
	lv.mu.Unlock()
	go func() {
		err := cmd.Wait()
		_ = w.Close()
		j.finish(lv, err)
	}()
	if err := j.publish(lv); err != nil {
		j.kill(lv)
		<-lv.done
		j.remove(lv)
		return nil, err
	}
	return lv, nil
}

// publish writes the current record. A finish that already recorded a terminal
// status is left as it is, so a late "running" write cannot clobber it.
func (j *Jobs) publish(lv *live) error {
	j.metaMu.Lock()
	defer j.metaMu.Unlock()
	job := lv.snapshot()
	if job.Status != "running" {
		return nil
	}
	return j.writeMeta(job)
}

func (j *Jobs) finish(lv *live, waitErr error) {
	lv.mu.Lock()
	if lv.finished {
		lv.mu.Unlock()
		return
	}
	cwdFile := lv.cwdFile
	onCwd := lv.onCwd
	report := !lv.detached
	if report {
		lv.claimCwd = true
	} else {
		onCwd = nil
	}
	lv.mu.Unlock()
	var ended string
	if cwdFile != "" {
		if report {
			ended = readCwdFile(cwdFile)
		}
		_ = os.Remove(cwdFile)
	}
	j.metaMu.Lock()
	lv.mu.Lock()
	if lv.finished {
		lv.mu.Unlock()
		j.metaMu.Unlock()
		return
	}
	// Timeout sets detached only while claimCwd is still false. If it won,
	// this exit must not move the session directory.
	if lv.detached {
		report = false
		onCwd = nil
		ended = ""
	}
	lv.finished = true
	lv.waitErr = waitErr
	detached := lv.detached
	if lv.killed {
		lv.job.Status = "cancelled"
	} else {
		lv.job.Status = "done"
	}
	lv.job.ExitCode = exitCode(waitErr)
	lv.job.Ended = j.now()
	if lv.w != nil {
		written, dropped := lv.w.span()
		lv.job.Bytes = written
		lv.job.Dropped = dropped
	}
	job := lv.job
	lv.mu.Unlock()
	err := j.writeMeta(job)
	if report && ended != "" && onCwd != nil {
		onCwd(ended)
	}
	j.metaMu.Unlock()
	// done closes after OnDone and prune, so Cancel and Shutdown return only
	// once this job has stopped writing: OnDone may post a wake and mark the
	// record notified.
	defer close(lv.done)
	if err != nil {
		return
	}
	if detached && j.OnDone != nil {
		j.OnDone(job)
	}
	j.prune()
}

func (j *Jobs) finishForeground(lv *live, maxOut int, status string) (string, error) {
	if status == "" {
		lv.mu.Lock()
		err := lv.waitErr
		code := lv.job.ExitCode
		lv.mu.Unlock()
		var exit *exec.ExitError
		if err != nil && !errors.As(err, &exit) {
			j.remove(lv)
			return "", fmt.Errorf("shell: %w", err)
		}
		if code == 0 {
			status = "exit 0"
		} else {
			status = fmt.Sprintf("exit %d", code)
		}
	}
	text, truncated := j.readCapped(lv, maxOut)
	j.remove(lv)
	return formatShell(status, text, truncated), nil
}

func (j *Jobs) readCapped(lv *live, maxOut int) (string, bool) {
	if lv.w != nil {
		_ = lv.w.Flush()
	}
	body, err := os.ReadFile(lv.snapshot().Output)
	if err != nil {
		return "", false
	}
	text := clip.Valid(string(body))
	truncated := false
	if maxOut <= 0 {
		maxOut = maxShellOutput
	}
	if len(text) > maxOut {
		text = text[:maxOut]
		truncated = true
	}
	return strings.TrimRight(text, "\n"), truncated
}

func (j *Jobs) kill(lv *live) {
	if lv == nil {
		return
	}
	lv.mu.Lock()
	if lv.finished || lv.cmd == nil || lv.cmd.Process == nil {
		lv.mu.Unlock()
		return
	}
	lv.killed = true
	cmd := lv.cmd
	lv.mu.Unlock()
	_ = killProcess(cmd)
}

// prune drops finished jobs older than maxJobAge and, past that, the oldest
// finished jobs until maxKeptJobs remain. Running jobs stay.
func (j *Jobs) prune() {
	if j == nil {
		return
	}
	now := j.now()
	j.mu.Lock()
	defer j.mu.Unlock()
	type item struct {
		id  string
		job Job
	}
	var done []item
	for id, lv := range j.jobs {
		job := lv.snapshot()
		if job.Status == "" || job.Status == "running" {
			continue
		}
		done = append(done, item{id, job})
	}
	for i := 1; i < len(done); i++ {
		for k := i; k > 0; k-- {
			a, b := done[k-1].job, done[k].job
			if a.Ended.Before(b.Ended) || (a.Ended.Equal(b.Ended) && a.ID <= b.ID) {
				break
			}
			done[k-1], done[k] = done[k], done[k-1]
		}
	}
	drop := func(it item) {
		delete(j.jobs, it.id)
		if it.job.Output != "" {
			_ = os.Remove(it.job.Output)
		}
		_ = os.Remove(j.metaPath(it.id))
	}
	var stay []item
	for _, it := range done {
		if !it.job.Ended.IsZero() && now.Sub(it.job.Ended) > maxJobAge {
			drop(it)
			continue
		}
		stay = append(stay, it)
	}
	if extra := len(stay) - maxKeptJobs; extra > 0 {
		for _, it := range stay[:extra] {
			drop(it)
		}
	}
}

func (j *Jobs) remove(lv *live) {
	if lv == nil {
		return
	}
	id := lv.snapshot().ID
	path := lv.snapshot().Output
	j.mu.Lock()
	delete(j.jobs, id)
	j.mu.Unlock()
	if path != "" {
		_ = os.Remove(path)
	}
	_ = os.Remove(j.metaPath(id))
}

func (j *Jobs) get(id string) *live {
	if j == nil || !validJobID(id) {
		return nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.jobs[id]
}

func (j *Jobs) countLocked() int {
	n := 0
	for _, lv := range j.jobs {
		if lv.snapshot().Status == "running" {
			n++
		}
	}
	return n
}

func (lv *live) snapshot() Job {
	if lv == nil {
		return Job{}
	}
	lv.mu.Lock()
	defer lv.mu.Unlock()
	return lv.job
}

func (j *Jobs) metaPath(id string) string {
	return filepath.Join(j.Dir, id+".json")
}

func (j *Jobs) readMeta(id string) (Job, error) {
	body, err := os.ReadFile(j.metaPath(id))
	if err != nil {
		return Job{}, err
	}
	var job Job
	if err := json.Unmarshal(body, &job); err != nil {
		return Job{}, err
	}
	if job.ID == "" {
		job.ID = id
	}
	return job, nil
}

func (j *Jobs) writeMeta(job Job) error {
	want := filepath.Clean(filepath.Join(j.Dir, job.ID+".log"))
	if job.Output != "" && filepath.Clean(job.Output) != want {
		return errors.New("shell: job output is outside the bot directory")
	}
	raw, err := json.Marshal(job)
	if err != nil {
		return err
	}
	return fsutil.WriteAtomic(j.metaPath(job.ID), append(raw, '\n'), 0600)
}

func newJobID(have map[string]*live) (string, error) {
	for i := 0; i < 8; i++ {
		var b [4]byte
		if _, err := rand.Read(b[:]); err != nil {
			return "", err
		}
		id := "sh_" + hex.EncodeToString(b[:])
		if _, ok := have[id]; !ok {
			return id, nil
		}
	}
	return "", errors.New("shell: could not allocate a job id")
}

func validJobID(id string) bool {
	if len(id) != 11 || !strings.HasPrefix(id, "sh_") {
		return false
	}
	for _, r := range id[3:] {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

func clipCommand(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 500 {
		return s[:500]
	}
	return s
}

func formatBackground(job Job, note string) string {
	cwd := ""
	if job.Cwd != "" {
		cwd = "\ncwd: " + job.Cwd
	}
	return fmt.Sprintf("background %s\noutput: %s%s\n%s\nUse shell_output to read (offset or tail), shell_status to check, and shell_cancel to stop. Do not poll in a tight loop.", job.ID, job.Output, cwd, note)
}

func tailLines(s string, n int) string {
	if n <= 0 || s == "" {
		return ""
	}
	s = strings.TrimRight(s, "\n")
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func sortJobs(jobs []Job) {
	for i := 1; i < len(jobs); i++ {
		for k := i; k > 0; k-- {
			if !jobs[k].Started.Before(jobs[k-1].Started) && (jobs[k].Started != jobs[k-1].Started || jobs[k].ID >= jobs[k-1].ID) {
				break
			}
			jobs[k], jobs[k-1] = jobs[k-1], jobs[k]
		}
	}
}

func closedChan() chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

// skipAutoBackground reports commands that are just waiting or polling.
// Those stay in the foreground so a repeated call trips the breaker instead
// of hiding behind a background job. An explicit run_in_background still starts.
func skipAutoBackground(command string) bool {
	s := strings.TrimSpace(command)
	for {
		field := s
		if i := strings.IndexAny(s, " \t\n"); i >= 0 {
			field = s[:i]
		}
		if !envAssign(field) {
			break
		}
		s = strings.TrimSpace(s[len(field):])
	}
	if s == "" {
		return false
	}
	field := s
	if i := strings.IndexAny(s, " \t\n;&|"); i >= 0 {
		field = s[:i]
	}
	switch filepath.Base(field) {
	case "sleep", "watch":
		return true
	}
	head := field
	if strings.HasPrefix(s, "while ") || strings.HasPrefix(s, "while\t") || strings.HasPrefix(s, "until ") || strings.HasPrefix(s, "until\t") {
		head = strings.Fields(s)[0]
	}
	if (head == "while" || head == "until") && strings.Contains(s, "sleep") {
		return true
	}
	return false
}

func envAssign(s string) bool {
	if s == "" || s[0] == '=' {
		return false
	}
	i := strings.IndexByte(s, '=')
	if i <= 0 {
		return false
	}
	for _, r := range s[:i] {
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	r := rune(s[0])
	return r == '_' || unicode.IsLetter(r)
}
