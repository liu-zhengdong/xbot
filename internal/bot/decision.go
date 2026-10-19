package bot

import (
	"encoding/json"
	"path/filepath"
	"time"

	"xbot/internal/fsutil"
)

// Decision states appended to a bot's dangerous-operation review log.
const (
	DecisionAllowed  = "allowed"
	DecisionDeclined = "declined"
	DecisionExpired  = "expired"
)

// Decision is one review outcome: what the kernel asked about, what the owner
// decided, and when. The summary is expected to be redacted already.
type Decision struct {
	Time    time.Time `json:"time"`
	Tool    string    `json:"tool"`
	State   string    `json:"state"`
	Summary string    `json:"summary"`
}

// DecisionPath is the append-only review log for this bot.
func (b *Bot) DecisionPath() string {
	return filepath.Join(b.dir(), "decisions.jsonl")
}

// LogDecision appends one review outcome. The file is 0600 and created on the
// first write. The caller redacts the summary before calling. The timestamp is
// written in local time with its offset (RFC3339), not converted to UTC.
func (b *Bot) LogDecision(state, tool, summary string, at time.Time) error {
	if at.IsZero() {
		at = time.Now()
	}
	return fsutil.AppendJSONL(b.DecisionPath(), Decision{
		Time:    at.Local(),
		Tool:    tool,
		State:   state,
		Summary: summary,
	})
}

// Trace kinds written for eval scoring. Stop is a circuit-breaker hard stop.
// Usage is one model call's token count. Fit is a request shrunk inside a turn
// to stay under the context lines; its reason says how.
const (
	TraceStop  = "stop"
	TraceUsage = "usage"
	TraceFit   = "fit"
)

// Trace is one append-only trace event. Reason is a stable code, never a secret.
// The token fields are set only for kind usage.
type Trace struct {
	Time              time.Time `json:"time"`
	Kind              string    `json:"kind"`
	Reason            string    `json:"reason"`
	Prompt            int       `json:"prompt,omitempty"`
	Completion        int       `json:"completion,omitempty"`
	Cached            int       `json:"cached,omitempty"`
	TurnCompletion    int       `json:"turn_completion,omitempty"`
	SessionPrompt     int       `json:"session_prompt,omitempty"`
	SessionCompletion int       `json:"session_completion,omitempty"`
	SessionCached     int       `json:"session_cached,omitempty"`
	Window            int       `json:"window,omitempty"`
	Estimated         bool      `json:"estimated,omitempty"`
	// Branch is set on a usage record from a background branch. Its counts
	// are that call's own; the foreground's latest counts are not touched.
	Branch string `json:"branch,omitempty"`
}

// TracePath is the append-only trace for this bot.
func (b *Bot) TracePath() string {
	return filepath.Join(b.dir(), "trace.jsonl")
}

// LogTrace appends one trace event. The file is 0600 and created on the first write.
func (b *Bot) LogTrace(kind, reason string, at time.Time) error {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	return fsutil.AppendJSONL(b.TracePath(), Trace{Time: at.UTC(), Kind: kind, Reason: reason})
}

// LogUsage appends one usage trace. The counts are integers; the caller does
// not put prompt text or secrets in the record.
func (b *Bot) LogUsage(t Trace) error {
	if t.Time.IsZero() {
		t.Time = time.Now().UTC()
	} else {
		t.Time = t.Time.UTC()
	}
	t.Kind = TraceUsage
	if t.Reason == "" {
		if t.Estimated {
			t.Reason = "estimated"
		} else {
			t.Reason = "reported"
		}
	}
	return fsutil.AppendJSONL(b.TracePath(), t)
}

// Traces reads the trace log in order. A missing log is empty.
func (b *Bot) Traces() ([]Trace, error) {
	var out []Trace
	err := fsutil.ReadJSONL(b.TracePath(), func(line []byte) error {
		var t Trace
		if err := json.Unmarshal(line, &t); err != nil {
			return err
		}
		out = append(out, t)
		return nil
	})
	return out, err
}

// Decisions reads the review log in order. A missing log is empty.
func (b *Bot) Decisions() ([]Decision, error) {
	var out []Decision
	err := fsutil.ReadJSONL(b.DecisionPath(), func(line []byte) error {
		var d Decision
		if err := json.Unmarshal(line, &d); err != nil {
			return err
		}
		out = append(out, d)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
