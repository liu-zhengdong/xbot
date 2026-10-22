package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"xbot/internal/bot"
	"xbot/internal/exec"
)

// recordExec takes a dispatch and runs nothing: a dispatch case grades what
// the foreground handed out, not what a subagent would do with it.
type recordExec struct {
	mu     sync.Mutex
	starts []exec.StartRequest
	seeded []exec.Task
	live   map[string]bool
	events chan exec.Event
}

func (r *recordExec) Start(_ context.Context, req exec.StartRequest) (exec.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.starts = append(r.starts, req)
	if r.live == nil {
		r.live = map[string]bool{}
	}
	r.live[req.ID] = true
	return exec.Task{ID: req.ID, Task: req.Task, Status: "running"}, nil
}

func (r *recordExec) Control(_ context.Context, id string, _ exec.Control) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.live[id] {
		return fmt.Errorf("exec: unknown subagent %s", id)
	}
	return nil
}

func (r *recordExec) Events() <-chan exec.Event { return r.events }

func (r *recordExec) Tasks() ([]exec.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	list := append([]exec.Task{}, r.seeded...)
	for _, req := range r.starts {
		if r.live[req.ID] {
			list = append(list, exec.Task{ID: req.ID, Task: req.Task, Status: "running"})
		}
	}
	return list, nil
}

// gradeDispatch checks the dispatch calls the foreground made.
func gradeDispatch(want *DispatchCheck, used []ToolUse) []string {
	if want == nil {
		return nil
	}
	type call struct {
		Type string `json:"type"`
		Task string `json:"task"`
	}
	var calls []call
	for _, u := range used {
		if u.Name != "dispatch" {
			continue
		}
		var c call
		if json.Unmarshal([]byte(u.Args), &c) == nil {
			calls = append(calls, c)
		}
	}
	if want.None {
		if len(calls) > 0 {
			return []string{fmt.Sprintf("dispatched %d times; this needs no subagent", len(calls))}
		}
		return nil
	}
	if len(calls) == 0 {
		return []string{"no dispatch"}
	}
	var problems []string
	got := calls[0]
	if len(calls) > 1 {
		problems = append(problems, fmt.Sprintf("dispatched %d times for one task", len(calls)))
	}
	if want.Type != "" && got.Type != want.Type {
		problems = append(problems, fmt.Sprintf("dispatched as %q, want %q", got.Type, want.Type))
	}
	task := strings.ToLower(got.Task)
	for _, m := range want.TaskMentions {
		found := false
		for _, alt := range strings.Split(m, "|") {
			if alt = strings.ToLower(strings.TrimSpace(alt)); alt != "" && strings.Contains(task, alt) {
				found = true
				break
			}
		}
		if !found {
			problems = append(problems, fmt.Sprintf("task does not say %q", m))
		}
	}
	return problems
}

func seedDispatchAgents(b *bot.Bot, c Case) (*recordExec, error) {
	r := &recordExec{events: make(chan exec.Event), live: map[string]bool{}}
	if len(c.Setup.Agents) > 0 && !c.Dispatch {
		return nil, fmt.Errorf("agents fixture requires dispatch: true")
	}
	for _, a := range c.Setup.Agents {
		if !regexp.MustCompile(`^sa_[a-z0-9]{8,}$`).MatchString(a.ID) || a.Status != "running" {
			return nil, fmt.Errorf("invalid running agent fixture")
		}
		if r.live[a.ID] {
			return nil, fmt.Errorf("duplicate agent fixture %s", a.ID)
		}
		dir := b.AgentDir(a.ID)
		if err := os.MkdirAll(dir, 0700); err != nil {
			return nil, err
		}
		raw, err := json.Marshal(exec.Meta{ID: a.ID, Task: a.Task, Type: a.Type, Status: a.Status, Mode: exec.ModeBrief, Steps: 1})
		if err != nil {
			return nil, err
		}
		if err = os.WriteFile(filepath.Join(dir, "meta.json"), raw, 0600); err != nil {
			return nil, err
		}
		r.live[a.ID] = true
		r.seeded = append(r.seeded, exec.Task{ID: a.ID, Task: a.Task, Status: a.Status})
	}
	return r, nil
}
