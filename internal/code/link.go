package code

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"xbot/internal/inbox"
	"xbot/internal/link"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

// Link is the coding-agent grant. Its tools edit local records and, when a
// backend is configured, run the task. Schemas stay off the model list until load.
type Link struct {
	rec         *Record
	inbox       string
	command     string
	runnerURL   string
	runnerToken string
	prov        provider.Provider
	transcript  func(string) error
	mu          sync.Mutex
	live        map[string]*liveRun
	tail        map[string]string
}

// Open binds the record store to a bot directory and posts terminal states to inboxPath.
func Open(botDir, inboxPath string) *Link {
	l := &Link{inbox: inboxPath}
	l.rec = &Record{Dir: filepath.Join(botDir, "code"), OnTerminal: l.post}
	return l
}

// Record is the store behind this link. Tests finish tasks through it.
func (l *Link) Record() *Record {
	if l == nil {
		return nil
	}
	return l.rec
}

func (l *Link) Name() string { return link.NameCoding }

func (l *Link) Tools() []tools.Tool {
	if l == nil || l.rec == nil {
		return nil
	}
	return []tools.Tool{
		startTool{l},
		steerTool{l},
		queueTool{l},
		interruptTool{l},
		cancelTool{l},
		showTool{l},
	}
}

func (l *Link) post(task Task) {
	if l == nil || l.inbox == "" || task.ID == "" {
		return
	}
	box, err := inbox.Open(l.inbox)
	if err != nil {
		return
	}
	text := task.Result
	if text == "" {
		text = task.Goal
	}
	ev := link.NewEvent(link.NameCoding, task.Status, task.ID+"\n"+text, task.ID, task.Updated)
	if task.Status == StatusInterrupted {
		ev.ID = "ev_" + task.ID + "_interrupted"
	}
	_ = box.Post(ev)
}

type startTool struct{ l *Link }

func (startTool) Name() string { return "code_start" }
func (startTool) Description() string {
	return "Start a coding-agent task. work_dir is absolute. max_steps and timeout_sec belong to this task, not the subagent defaults. A second task waits in the queue. It runs on a local coding CLI (XBOT_CODE_AGENT, or cursor-agent, codex, claude on PATH), the xbot runner, or this bot's model. The owner confirms first because the task edits files. With none of those backends, only the record is written."
}
func (startTool) Parameters() any { return startSchema() }
func (startTool) Confirm(args json.RawMessage) (bool, string, error) {
	var in struct {
		WorkDir string `json:"work_dir"`
		Goal    string `json:"goal"`
	}
	if len(args) > 0 && string(args) != "null" {
		if err := json.Unmarshal(args, &in); err != nil {
			return false, "", err
		}
	}
	summary := "code start\n" + strings.TrimSpace(in.WorkDir) + "\n" + strings.TrimSpace(in.Goal) + "\nedits files"
	return false, summary, nil
}
func (t startTool) Run(_ context.Context, args json.RawMessage) (string, error) {
	var in struct {
		WorkDir    string `json:"work_dir"`
		Repo       string `json:"repo"`
		MaxSteps   int    `json:"max_steps"`
		TimeoutSec int    `json:"timeout_sec"`
		Goal       string `json:"goal"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	pid, procStart := 0, int64(0)
	if t.l.hasBackend() {
		pid, procStart = tools.CurrentProcess()
	}
	task, err := t.l.rec.Start(Task{
		PID: pid, ProcStart: procStart,
		WorkDir: in.WorkDir, Repo: in.Repo, MaxSteps: in.MaxSteps,
		TimeoutSec: in.TimeoutSec, Goal: in.Goal,
	})
	if err != nil {
		return "", err
	}
	if task.Status == StatusRunning {
		t.l.launch(task)
	}
	return task.Status + " " + task.ID, nil
}

type steerTool struct{ l *Link }

func (steerTool) Name() string { return "code_steer" }
func (steerTool) Description() string {
	return "Append a note to the running coding task. It does not drop work already recorded."
}
func (steerTool) Parameters() any { return noteSchema("extra instruction") }
func (steerTool) Confirm(json.RawMessage) (bool, string, error) {
	return false, "code_steer", nil
}
func (t steerTool) Run(_ context.Context, args json.RawMessage) (string, error) {
	return control(t.l, args, KindSteer)
}

type queueTool struct{ l *Link }

func (queueTool) Name() string { return "code_queue" }
func (queueTool) Description() string {
	return "Append a note the running coding task should handle after its current step."
}
func (queueTool) Parameters() any { return noteSchema("instruction to run after the current step") }
func (queueTool) Confirm(json.RawMessage) (bool, string, error) {
	return false, "code_queue", nil
}
func (t queueTool) Run(_ context.Context, args json.RawMessage) (string, error) {
	return control(t.l, args, KindQueue)
}

type interruptTool struct{ l *Link }

func (interruptTool) Name() string { return "code_interrupt" }
func (interruptTool) Description() string {
	return "Append an interrupt note to the running coding task."
}
func (interruptTool) Parameters() any { return noteSchema("what to do instead") }
func (interruptTool) Confirm(json.RawMessage) (bool, string, error) {
	return false, "code_interrupt", nil
}
func (t interruptTool) Run(_ context.Context, args json.RawMessage) (string, error) {
	return control(t.l, args, KindInterrupt)
}

type cancelTool struct{ l *Link }

func (cancelTool) Name() string { return "code_cancel" }
func (cancelTool) Description() string {
	return "Cancel a coding task. If it was running, the oldest queued task becomes running. A cancelled task posts an untrusted event."
}
func (cancelTool) Parameters() any {
	return objectSchema(map[string]any{
		"id": stringProp("task id"),
	}, []string{"id"})
}
func (cancelTool) Confirm(json.RawMessage) (bool, string, error) {
	return false, "code_cancel", nil
}
func (t cancelTool) Run(_ context.Context, args json.RawMessage) (string, error) {
	var in struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	task, err := t.l.rec.Control(in.ID, KindCancel, "")
	if err != nil {
		return "", err
	}
	t.l.nudge(task.ID, KindCancel)
	return task.Status + " " + task.ID, nil
}

type showTool struct{ l *Link }

func (showTool) Name() string { return "code_show" }
func (showTool) Description() string {
	return "Show coding-agent task records. Omit id to list every task."
}
func (showTool) Parameters() any {
	return objectSchema(map[string]any{
		"id": stringProp("task id; omit to list"),
	}, nil)
}
func (showTool) Confirm(json.RawMessage) (bool, string, error) {
	return false, "code_show", nil
}
func (t showTool) Run(_ context.Context, args json.RawMessage) (string, error) {
	var in struct {
		ID string `json:"id"`
	}
	if len(args) > 0 && string(args) != "null" {
		if err := json.Unmarshal(args, &in); err != nil {
			return "", err
		}
	}
	if strings.TrimSpace(in.ID) == "" {
		tasks, err := t.l.rec.List()
		if err != nil {
			return "", err
		}
		if len(tasks) == 0 {
			return "no coding tasks", nil
		}
		var b strings.Builder
		for _, task := range tasks {
			fmt.Fprintf(&b, "%s\t%s\t%s\n", task.ID, task.Status, oneLine(task.Goal))
		}
		return strings.TrimRight(b.String(), "\n"), nil
	}
	task, err := t.l.rec.Get(in.ID)
	if err != nil {
		return "", err
	}
	return formatTask(t.l, task), nil
}

func control(l *Link, args json.RawMessage, kind string) (string, error) {
	var in struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	task, err := l.rec.Control(in.ID, kind, in.Text)
	if err != nil {
		return "", err
	}
	l.nudge(task.ID, kind)
	return kind + " " + task.ID, nil
}

func formatTask(l *Link, task Task) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\nwork_dir: %s\nsteps: %d\ntimeout_sec: %d\ngoal: %s",
		task.Status, task.ID, task.WorkDir, task.MaxSteps, task.TimeoutSec, oneLine(task.Goal))
	if task.Repo != "" {
		fmt.Fprintf(&b, "\nrepo: %s", oneLine(task.Repo))
	}
	if task.Result != "" {
		fmt.Fprintf(&b, "\nresult: %s", oneLine(task.Result))
	} else if text := l.output(task.ID); text != "" {
		fmt.Fprintf(&b, "\noutput: %s", oneLine(text))
	}
	for _, note := range task.Notes {
		fmt.Fprintf(&b, "\n%s: %s", note.Kind, oneLine(note.Text))
	}
	return b.String()
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func startSchema() map[string]any {
	return objectSchema(map[string]any{
		"work_dir":    stringProp("absolute working directory"),
		"repo":        stringProp("repository URL or path; recorded only"),
		"max_steps":   intProp("step cap for this task"),
		"timeout_sec": intProp("time cap for this task, in seconds"),
		"goal":        stringProp("what the task should do"),
	}, []string{"work_dir", "max_steps", "timeout_sec", "goal"})
}

func noteSchema(desc string) map[string]any {
	return objectSchema(map[string]any{
		"id":   stringProp("task id"),
		"text": stringProp(desc),
	}, []string{"id", "text"})
}

func objectSchema(props map[string]any, required []string) map[string]any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{
		"type":                 "object",
		"properties":           props,
		"required":             required,
		"additionalProperties": false,
	}
}

func stringProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

func intProp(desc string) map[string]any {
	return map[string]any{"type": "integer", "description": desc}
}
