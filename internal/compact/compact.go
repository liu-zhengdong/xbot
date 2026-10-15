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
type Budget struct {
	CompactAt int
	KeepTail  int
}

// State is stored on the bot. It is not a new session.
type State struct {
	Summary string
	Covered int
}

// DefaultBudget stays under a 64k-token window with a margin, and keeps a short verbatim tail.
func DefaultBudget() Budget {
	return Budget{CompactAt: 48000, KeepTail: 8}
}

// Estimate is a rough token count: about 4 bytes per token. It is not a model tokenizer.
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

// EstimateMessage estimates one message.
func EstimateMessage(m provider.Message) int {
	n := 4 + Estimate(m.Content) + Estimate(m.Name)
	for _, tc := range m.ToolCalls {
		n += 4 + Estimate(tc.Function.Name) + Estimate(tc.Function.Arguments)
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
