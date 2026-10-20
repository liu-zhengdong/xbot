package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"xbot/internal/exec"
)

// recordExec takes a dispatch and runs nothing: a dispatch case grades what
// the foreground handed out, not what a subagent would do with it.
type recordExec struct {
	mu     sync.Mutex
	starts []exec.StartRequest
	events chan exec.Event
}

func (r *recordExec) Start(_ context.Context, req exec.StartRequest) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.starts = append(r.starts, req)
	return nil
}

func (r *recordExec) Control(context.Context, string, exec.Control) error { return nil }

func (r *recordExec) Events() <-chan exec.Event { return r.events }

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
