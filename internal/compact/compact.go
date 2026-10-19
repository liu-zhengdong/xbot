// Package compact shrinks a bot's working context. The transcript stays intact.
// Covered is how many leading transcript messages are represented by Summary.
package compact

import (
	"context"
	"errors"
	"strings"

	"xbot/internal/provider"
)

// Compactor turns older turns into a summary. The LLM implementation is one of these.
type Compactor interface {
	Compact(ctx context.Context, prior string, turns []provider.Message) (string, error)
}

// Budget is when to compact, in the rough token estimate used by Estimate.
// CompactAt is the working set: a long session is held near this size instead
// of growing to the model's window. KeepTail is a floor on the raw tail, which
// the automatic paths grow by size as well (see tailBudget).
type Budget struct {
	CompactAt int
	KeepTail  int
}

// State is stored on the bot. It is not a new session.
type State struct {
	Summary string
	Covered int
}

// WorkingSet is the context size a long session is held near, in estimated
// tokens. A model window is usually far larger, but using it all makes every
// later turn slower, costlier and less accurate, and one large tool result can
// then fill what is left. The window is a ceiling, not a target.
const WorkingSet = 100_000

// DefaultBudget holds a session near WorkingSet and keeps a short verbatim tail.
func DefaultBudget() Budget {
	return Budget{CompactAt: WorkingSet, KeepTail: 8}
}

// Estimate is a rough token count: about 4 bytes per token. It is not a model
// tokenizer. A Chinese character is three bytes in UTF-8, so it counts as 0.75
// of a token here while a real tokenizer spends about one; text that is mostly
// Chinese is therefore undercounted by roughly a quarter. Compare this with a
// reported prompt count before trusting it to hold a fixed size.
func Estimate(s string) int {
	if s == "" {
		return 0
	}
	return (len(s) + 3) / 4
}

// EstimateMessages estimates a slice of chat messages, including a small per-message overhead.
func EstimateMessages(msgs []provider.Message) int {
	n := 0
	for _, m := range msgs {
		n += EstimateMessage(m)
	}
	return n
}

// EstimateMessage estimates one message. Image bytes count at the same rough
// rate as text, about 4 bytes per token, so a picture is not free.
func EstimateMessage(m provider.Message) int {
	n := 4 + Estimate(m.Content) + Estimate(m.Name)
	for _, tc := range m.ToolCalls {
		n += 4 + Estimate(tc.Function.Name) + Estimate(tc.Function.Arguments)
	}
	for _, img := range m.Images {
		n += 4 + (len(img.MIME)+len(img.Data)+3)/4
	}
	return n
}

// Maybe compacts the prefix of msgs until the estimate fits or only the latest message remains.
// On a compactor error the returned state includes any passes that already succeeded.
// The transcript is not modified.
func Maybe(ctx context.Context, c Compactor, b Budget, st State, msgs []provider.Message) (State, error) {
	if b.CompactAt <= 0 {
		b.CompactAt = DefaultBudget().CompactAt
	}
	if b.KeepTail <= 0 {
		b.KeepTail = DefaultBudget().KeepTail
	}
	if st.Covered < 0 || st.Covered > len(msgs) {
		return st, errors.New("compact: covered is out of range")
	}
	for pass := 0; pass < 8; pass++ {
		recent := msgs[st.Covered:]
		est := Estimate(st.Summary) + EstimateMessages(recent)
		if est <= b.CompactAt || len(recent) <= 1 {
			return st, nil
		}
		keep := b.KeepTail
		if keep >= len(recent) {
			keep = len(recent) - 1
		}
		if keep < 1 {
			keep = 1
		}
		for keep > 1 && Estimate(st.Summary)+EstimateMessages(recent[len(recent)-keep:]) > b.CompactAt {
			keep--
		}
		cut := len(msgs) - keep
		if cut <= st.Covered {
			return st, nil
		}
		if c == nil {
			return st, errors.New("compact: context exceeded and no compactor is configured")
		}
		summary, err := c.Compact(ctx, st.Summary, msgs[st.Covered:cut])
		if err != nil {
			return st, err
		}
		summary = strings.TrimSpace(summary)
		if summary == "" {
			return st, errors.New("compact: empty summary")
		}
		st.Summary = summary
		st.Covered = cut
	}
	return st, nil
}
