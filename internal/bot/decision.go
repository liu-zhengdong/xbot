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
