package code

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"xbot/internal/agent"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

const codingPersona = "You are a coding agent working in the given directory. Complete the task. Do not spawn subagents. When you finish, reply with a short summary and no tool call."

type outcome struct {
	Status string
	Text   string
}

type liveRun struct {
	mu       sync.Mutex
	cancel   context.CancelFunc
	stopStep func()
}

// ResolveCommand prefers an explicit path. Otherwise it uses the first of
// cursor-agent, codex, or claude that is on PATH.
func ResolveCommand(explicit string) string {
	explicit = strings.TrimSpace(explicit)
	if explicit != "" {
		return explicit
	}
	for _, name := range []string{"cursor-agent", "codex", "claude"} {
		path, err := exec.LookPath(name)
		if err == nil && path != "" {
			return path
		}
	}
	return ""
}

func commandArgs(bin, goal string) []string {
	switch filepath.Base(bin) {
	case "claude", "cursor-agent":
		return []string{"-p", goal}
	case "codex":
		return []string{"exec", goal}
	default:
		return []string{goal}
	}
}

// UseCommand runs this executable instead of a model.
func (l *Link) UseCommand(path string) {
	if l == nil {
		return
	}
	l.command = strings.TrimSpace(path)
}

// UseRunner sends tasks to an xbot runner. token is the runner bearer token.
func (l *Link) UseRunner(baseURL, token string) {
	if l == nil {
		return
	}
	l.runnerURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	l.runnerToken = token
}

// UseProvider runs tasks in this process with the bot's model.
func (l *Link) UseProvider(p provider.Provider) {
	if l == nil {
		return
	}
	l.prov = p
}

func (l *Link) hasBackend() bool {
	return l != nil && (l.command != "" || l.runnerURL != "" || l.prov != nil)
}

func (l *Link) launch(task Task) {
	if !l.hasBackend() || task.Status != StatusRunning {
		return
	}
	l.mu.Lock()
	if l.live[task.ID] != nil {
		l.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	run := &liveRun{cancel: cancel}
	if l.live == nil {
		l.live = map[string]*liveRun{}
	}
	l.live[task.ID] = run
	l.mu.Unlock()
	go l.execute(ctx, run, task)
}

func (l *Link) nudge(id, kind string) {
	l.mu.Lock()
	run := l.live[id]
	l.mu.Unlock()
	if run != nil {
		run.mu.Lock()
		if kind == KindCancel && run.cancel != nil {
			run.cancel()
		}
		if kind == KindInterrupt && run.stopStep != nil {
			run.stopStep()
		}
		run.mu.Unlock()
	}
	l.wake()
}

func (l *Link) wake() {
	if !l.hasBackend() {
		return
	}
	tasks, err := l.rec.List()
	if err != nil {
		return
	}
	for _, task := range tasks {
		if task.Status == StatusRunning {
			l.launch(task)
		}
	}
}

func (l *Link) execute(ctx context.Context, run *liveRun, task Task) {
	defer run.cancel()
	defer func() {
		l.mu.Lock()
		delete(l.live, task.ID)
		l.mu.Unlock()
		cur, err := l.rec.Get(task.ID)
		if err == nil && cur.Status == StatusRunning {
			return
		}
		l.wake()
	}()
	var out outcome
	switch {
	case l.command != "":
		out = runCommand(ctx, l.command, task, func(text string) {
			l.stream(task.ID, StatusRunning, text)
		})
	case l.runnerURL != "":
		out = runRemote(ctx, l.runnerURL, l.runnerToken, task, endpointOf(l.prov))
	default:
		out = runModel(ctx, run, l.prov, task, func() []Note {
			cur, err := l.rec.Get(task.ID)
			if err != nil {
				return nil
			}
			return cur.Notes
		})
	}
	cur, err := l.rec.Get(task.ID)
	if err == nil && cur.Status == StatusRunning {
		l.finalCard(task.ID, out.Status, out.Text)
		if _, ferr := l.rec.Finish(task.ID, out.Text, out.Status != StatusDone); ferr == nil {
			return
		}
		cur, err = l.rec.Get(task.ID)
	}
	if err != nil || !terminal(cur.Status) {
		return
	}
	text := cur.Result
	if strings.TrimSpace(text) == "" {
		text = out.Text
	}
	l.finalCard(task.ID, cur.Status, text)
}

// finalCard appends the card the UI keeps for this id. A cancel has already
// left the record, so this still runs when the process is no longer running.
func (l *Link) finalCard(id, status, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		text = status
	}
	if text == "" {
		return
	}
	l.stream(id, status, text)
}

func runModel(ctx context.Context, run *liveRun, p provider.Provider, task Task, notes func() []Note) outcome {
	if p == nil {
		return outcome{Status: StatusFailed, Text: "code: no model"}
	}
	if err := checkout(ctx, task.Repo, task.WorkDir); err != nil {
		return outcome{Status: StatusFailed, Text: err.Error()}
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(task.TimeoutSec)*time.Second)
	defer cancel()
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: provider.TextContent(codingPersona)},
		{Role: provider.RoleUser, Content: provider.TextContent(taskText(task))},
	}
	seen := 0
	var last string
	for step := 0; step < task.MaxSteps; step++ {
		if ctx.Err() != nil {
			return ctxOutcome(ctx)
		}
		if notes != nil {
			batch := notes()
			if len(batch) > seen {
				msgs = append(msgs, provider.Message{Role: provider.RoleUser, Content: provider.TextContent(renderNotes(batch[seen:]))})
				seen = len(batch)
			}
		}
		stepCtx, stepCancel := context.WithCancel(ctx)
		if run != nil {
			run.mu.Lock()
			run.stopStep = stepCancel
			run.mu.Unlock()
		}
		produced, err := agent.Run(stepCtx, p, workTools(task.WorkDir), msgs, agent.Options{
			MaxSteps: 1,
			OneRound: true,
			Approve:  approveTask,
		})
		stepCancel()
		if run != nil {
			run.mu.Lock()
			run.stopStep = nil
			run.mu.Unlock()
		}
		if err != nil {
			if ctx.Err() != nil {
				return ctxOutcome(ctx)
			}
			if stepCtx.Err() != nil {
				continue
			}
			return outcome{Status: StatusFailed, Text: err.Error()}
		}
		msgs = append(msgs, produced...)
		last = lastText(produced)
		if !calledTool(produced) {
			if strings.TrimSpace(last) == "" {
				last = "done"
			}
			return outcome{Status: StatusDone, Text: last}
		}
	}
	if strings.TrimSpace(last) == "" {
		last = "max steps reached"
	}
	return outcome{Status: StatusFailed, Text: last}
}

func approveTask(context.Context, string, string) (bool, error) { return true, nil }

func ctxOutcome(ctx context.Context) outcome {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return outcome{Status: StatusFailed, Text: "timed out"}
	}
	return outcome{Status: StatusCancelled, Text: "cancelled"}
}

func workTools(dir string) []tools.Tool {
	sh := tools.NewShell()
	sh.Dir = dir
	return []tools.Tool{sh, &tools.ReadFile{Root: dir}, &tools.WriteFile{Root: dir}, &tools.EditFile{Root: dir}}
}

func taskText(task Task) string {
	var b strings.Builder
	fmt.Fprintf(&b, "work_dir: %s\nmax_steps: %d\ntimeout_sec: %d\n", task.WorkDir, task.MaxSteps, task.TimeoutSec)
	if task.Repo != "" {
		fmt.Fprintf(&b, "repo: %s\n", task.Repo)
	}
	b.WriteString(task.Goal)
	return b.String()
}

func renderNotes(notes []Note) string {
	var b strings.Builder
	for _, note := range notes {
		fmt.Fprintf(&b, "%s: %s\n", note.Kind, note.Text)
	}
	return strings.TrimRight(b.String(), "\n")
}

func lastText(msgs []provider.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == provider.RoleAssistant && strings.TrimSpace(msgs[i].Text()) != "" {
			return strings.TrimSpace(msgs[i].Text())
		}
	}
	return ""
}

func calledTool(msgs []provider.Message) bool {
	for _, m := range msgs {
		if m.Role == provider.RoleAssistant && len(m.Calls()) > 0 {
			return true
		}
	}
	return false
}

func runCommand(ctx context.Context, command string, task Task, emit func(string)) outcome {
	if err := checkout(ctx, task.Repo, task.WorkDir); err != nil {
		return outcome{Status: StatusFailed, Text: err.Error()}
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(task.TimeoutSec)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, command, commandArgs(command, task.Goal)...)
	cmd.Dir = task.WorkDir
	cmd.Env = codeEnv(task)
	cmd.SysProcAttr = codeProcAttr()
	cmd.Cancel = func() error { return killCode(cmd) }
	cmd.WaitDelay = time.Second
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return outcome{Status: StatusFailed, Text: err.Error()}
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return outcome{Status: StatusFailed, Text: err.Error()}
	}
	if err := cmd.Start(); err != nil {
		return outcome{Status: StatusFailed, Text: err.Error()}
	}
	var mu sync.Mutex
	var b strings.Builder
	add := func(line string) {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			return
		}
		mu.Lock()
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(line)
		text := clipText(b.String())
		mu.Unlock()
		if emit != nil {
			emit(text)
		}
	}
	var wg sync.WaitGroup
	scan := func(r io.Reader) {
		defer wg.Done()
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for sc.Scan() {
			add(sc.Text())
		}
	}
	wg.Add(2)
	go scan(stdout)
	go scan(stderr)
	wg.Wait()
	err = cmd.Wait()
	mu.Lock()
	text := clipText(strings.TrimSpace(b.String()))
	mu.Unlock()
	if ctx.Err() != nil && !errors.Is(ctx.Err(), context.Canceled) {
		if text == "" {
			text = "timed out"
		}
		return outcome{Status: StatusFailed, Text: text}
	}
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		return outcome{Status: StatusFailed, Text: text}
	}
	if text == "" {
		text = "exited"
	}
	return outcome{Status: StatusDone, Text: text}
}

func codeEnv(task Task) []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"TMPDIR=" + os.Getenv("TMPDIR"),
		"LANG=" + os.Getenv("LANG"),
		"XBOT_CODE_ID=" + task.ID,
		"XBOT_CODE_GOAL=" + task.Goal,
		"XBOT_CODE_REPO=" + task.Repo,
		fmt.Sprintf("XBOT_CODE_MAX_STEPS=%d", task.MaxSteps),
		fmt.Sprintf("XBOT_CODE_TIMEOUT_SEC=%d", task.TimeoutSec),
	}
}

func clipText(s string) string {
	if len(s) > 4000 {
		return s[:4000]
	}
	return s
}

// checkout clones repo into dir when dir has no .git yet. An empty repo only creates dir.
func checkout(ctx context.Context, repo, dir string) error {
	if dir == "" || !filepath.IsAbs(dir) {
		return errors.New("code: work_dir must be absolute")
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		return nil
	}
	if strings.TrimSpace(repo) == "" {
		return os.MkdirAll(dir, 0700)
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0700); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "git", "clone", "--", repo, dir)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("code: clone: %s", clipText(msg))
	}
	return nil
}

func endpointOf(p provider.Provider) Endpoint {
	c, ok := provider.EndpointOf(p)
	if !ok {
		return Endpoint{}
	}
	return Endpoint{Protocol: c.Protocol, HostedSearch: c.HostedSearch, Thinking: c.Thinking, BaseURL: c.BaseURL, APIKey: c.APIKey, Model: c.Model}
}

// Endpoint is the protocol and model a remote runner should call.
type Endpoint struct {
	Protocol     string `json:"protocol,omitempty"`
	HostedSearch bool   `json:"hosted_web_search,omitempty"`
	Thinking     string `json:"thinking,omitempty"`
	BaseURL      string `json:"base_url,omitempty"`
	APIKey       string `json:"api_key,omitempty"`
	Model        string `json:"model,omitempty"`
}
