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

	"xbot/internal/clip"
	"xbot/internal/fsutil"
	"xbot/internal/notable"
	"xbot/internal/review"
)

const (
	// maxShellJobs bounds actual running tool calls, foreground and background.
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

// Job is one tool call tracked for this process. Status is running, done,
// cancelled, or lost. lost means a new process found the record still running
// and did not try to signal the old pid.
type Job struct {
	Description string         `json:"description,omitempty"`
	Background  bool           `json:"background,omitempty"`
	Notable     []notable.Item `json:"notable,omitempty"`
	Tool        string         `json:"tool,omitempty"`
	Result      string         `json:"result,omitempty"`
	Images      []string       `json:"images,omitempty"`

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
	Name        string
	Interpreter interpreter
	Description string
	onStart     func()
	Command     string
	Exec        string
	Timeout     time.Duration
	MaxOutput   int
	Dir         string
	Env         []string
	Background  bool
	CwdFile     string
	OnCwd       func(string)
}

// Jobs tracks tool calls for one bot directory. The same value lives for
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
	dirMu  sync.RWMutex
	jobs   map[string]*live
}

type live struct {
	finalize                   func(Job, CallResult) (Job, error)
	mu                         sync.Mutex
	job                        Job
	cmd                        *exec.Cmd
	w                          *jobWriter
	done                       chan struct{}
	detached, killed, finished bool
	cancel                     context.CancelFunc
	result                     CallResult
	onDone                     func(Job)
	onCwd                      func(string)
	finalCwd                   string
	cleanup                    func()
}

// NewJobs records jobs under dir. Call Load before Run so a restarted process
// does not treat an old running record as live.
func NewJobs(dir string) *Jobs {
	return &Jobs{Dir: dir, jobs: map[string]*live{}}
}

// dir is Dir read under dirMu, since Move can change it while calls run.
func (j *Jobs) dir() string {
	j.dirMu.RLock()
	defer j.dirMu.RUnlock()
	return j.Dir
}

// Move points this table at dir after a bot rename moved its directory there.
// Jobs whose output sat in the old directory follow it, so calls still running,
// the rename tool itself included, finish in the new directory and never
// recreate the old one. Moving to the current directory does nothing.
func (j *Jobs) Move(dir string) {
	if j == nil || dir == "" {
		return
	}
	j.metaMu.Lock()
	defer j.metaMu.Unlock()
	j.dirMu.Lock()
	old := j.Dir
	j.Dir = dir
	j.dirMu.Unlock()
	if old == dir {
		return
	}
	j.mu.Lock()
	lives := make([]*live, 0, len(j.jobs))
	for _, lv := range j.jobs {
		lives = append(lives, lv)
	}
	j.mu.Unlock()
	for _, lv := range lives {
		lv.mu.Lock()
		if id := lv.job.ID; lv.job.Output == filepath.Join(old, id+".log") {
			lv.job.Output = filepath.Join(dir, id+".log")
		}
		lv.mu.Unlock()
	}
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
// A record whose output path no longer sits in this directory, as a bot
// rename leaves behind, is repointed here before it is marked lost; one
// record that still cannot be written is skipped, not fatal.
// Loaded jobs are not Local. Finished jobs older than maxJobAge, and finished
// jobs past maxKeptJobs, are removed.
func (j *Jobs) Load() error {
	if j == nil || j.dir() == "" {
		return errors.New("shell: no job directory")
	}
	dir := j.dir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
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
			if SameProcess(job.PID, job.ProcStart) {
				j.jobs[id] = &live{job: job, done: closedChan()}
				continue
			}
			job.Status = "lost"
			job.Ended = j.now()
			if err := j.writeMeta(job); err != nil {
				// A renamed bot moved this directory with the record in it; the
				// output path inside still names the old one. Pull the output back
				// to this directory and write once more; if that also fails, drop
				// this one record rather than the whole table.
				job.Output = filepath.Join(dir, id+".log")
				if err := j.writeMeta(job); err != nil {
					continue
				}
			}
		}
		j.jobs[id] = &live{job: job, done: closedChan()}
	}
	j.mu.Unlock()
	j.prune(nil)
	return nil
}

// Run starts command. Background returns as soon as the process is running.
// Otherwise the common call runner owns the wait, capped at two seconds.
// After a background receipt the process no longer follows the parent context.
func (j *Jobs) Run(ctx context.Context, req RunRequest) (string, error) {
	if lv, ok := ctx.Value(callKey{}).(*live); ok {
		out, err := j.runProcess(ctx, lv, req)
		lv.mu.Lock()
		cb, dir := lv.onCwd, lv.finalCwd
		lv.onCwd = nil
		lv.mu.Unlock()
		if cb != nil && dir != "" {
			cb(dir)
		}
		return out, err
	}
	if req.Name == "" {
		req.Name = "sh"
	}
	ready := make(chan struct{})
	var once sync.Once
	req.onStart = func() { once.Do(func() { close(ready) }) }
	res := j.RunCall(ctx, CallRequest{Name: req.Name, Command: req.Command, Dir: req.Dir, Wait: req.Timeout, Background: req.Background,
		Run: func(ctx context.Context) CallResult {
			lv := ctx.Value(callKey{}).(*live)
			defer req.onStart()
			out, err := j.runProcess(ctx, lv, req)
			return CallResult{Text: out, Err: err}
		}})
	if res.Job != nil {
		if lv := j.get(res.Job.ID); lv != nil {
			select {
			case <-ready:
			case <-lv.done:
			}
		}
	}
	return res.Text, res.Err
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
	job := j.freshen(lv)
	var written, discarded, held int
	if lv.w != nil && (review.IsShellTool(job.Tool) || job.Status == "running") {
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

// Deliver serializes transport acknowledgement with the existing metadata
// writer. An inbox can deduplicate the stable id after a crash between send
// and acknowledgement; a failed send leaves Notified false for the next flush.
func (j *Jobs) Deliver(id string, send func(Job, CallResult) error) error {
	lv := j.get(id)
	if lv == nil {
		return fmt.Errorf("tool job: no such job %s", id)
	}
	j.metaMu.Lock()
	defer j.metaMu.Unlock()
	lv.mu.Lock()
	job, res := lv.job, lv.result
	if !job.Local {
		if disk, err := j.readMeta(id); err == nil {
			job = disk
			lv.job = disk
		}
	}
	lv.mu.Unlock()
	if job.Notified || job.Status == "running" {
		return nil
	}
	if err := send(job, res); err != nil {
		return err
	}
	lv.mu.Lock()
	lv.job.Notified = true
	lv.result.Images = nil
	job = lv.job
	lv.mu.Unlock()
	return j.writeMeta(job)
}

// Shutdown cancels and joins every call this Jobs value owns. Shell adapters
// kill their process group; other tools must actually return before this ends.
// A job loaded from another execution process is never signalled.
func (j *Jobs) Shutdown() {
	if j == nil {
		return
	}
	j.mu.Lock()
	var owned []*live
	for _, lv := range j.jobs {
		if job := lv.snapshot(); job.Local {
			owned = append(owned, lv)
		}
	}
	j.mu.Unlock()
	for _, lv := range owned {
		_, _ = j.Cancel(lv.snapshot().ID)
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
	if !job.Local && lv != nil {
		if disk, err := j.readMeta(job.ID); err == nil {
			lv.mu.Lock()
			lv.job = disk
			lv.mu.Unlock()
			return disk
		}
	}
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

// Cancel requests the owned call to stop. Shell adapters kill the process
// group; a noncooperative tool remains running until it really returns.
func (j *Jobs) Cancel(id string) (string, error) {
	lv := j.get(id)
	if lv == nil {
		return "", fmt.Errorf("shell: no such job %s", id)
	}
	_ = j.freshen(lv)
	lv.mu.Lock()
	if lv.finished || lv.job.Status != "running" {
		status := lv.job.Status
		lv.mu.Unlock()
		<-lv.done
		return "status " + status, nil
	}
	cancel := lv.cancel
	if cancel == nil {
		lv.mu.Unlock()
		return "", ErrForeignJob
	}
	lv.killed = true
	lv.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	select {
	case <-lv.done:
		return "cancelled " + id, nil
	case <-time.After(250 * time.Millisecond):
		return "cancellation requested " + id, nil
	}
}

// prune drops finished jobs older than maxJobAge and, past that, the oldest
// finished jobs until maxKeptJobs remain. Running jobs stay.
func (j *Jobs) prune(settled *live) {
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
		if job.Local && !job.Background {
			continue
		}
		if lv != settled {
			select {
			case <-lv.done:
			default:
				continue
			}
		}

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
	lv := j.jobs[id]
	if lv == nil {
		if job, err := j.readMeta(id); err == nil {
			lv = &live{job: job, done: closedChan()}
			j.jobs[id] = lv
		}
	}
	return lv
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
	return filepath.Join(j.dir(), id+".json")
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
	// The file name is the validated id; a body naming another id is not
	// this record, and its id must never build a path.
	if job.ID != id {
		return Job{}, fmt.Errorf("shell: job record %s names id %q", id, job.ID)
	}
	return job, nil
}

func (j *Jobs) writeMeta(job Job) error {
	want := filepath.Clean(filepath.Join(j.dir(), job.ID+".log"))
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

// CurrentProcess reuses the shell job's process identity for task ownership.
// Platforms without start ticks can only recognize this process instance.
var selfProcessStart = func() int64 {
	n := processStartTicks(os.Getpid())
	if n == 0 {
		return -time.Now().UnixNano()
	}
	return n
}()

func CurrentProcess() (int, int64) { return os.Getpid(), selfProcessStart }
func SameProcess(pid int, start int64) bool {
	if pid == os.Getpid() && start == selfProcessStart {
		return true
	}
	return sameProcess(pid, start)
}

var ErrForeignJob = errors.New("tool job belongs to another execution process")
