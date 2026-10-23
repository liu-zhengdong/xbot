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
// There are two lines (ADR 0015). Ceiling is the hard one: past it the model
// would reject the request, so the kernel compacts inside the turn. Target is
// the soft one: past it the kernel compacts in the background between turns
// and the turn goes on. Zero for either means "derive it from the model's
// window" (see Lines). KeepTail is a floor on the raw tail, which the
// automatic paths grow by size as well (see tailBudget).
type Budget struct {
	Target   int
	Ceiling  int
	KeepTail int
}

// State is stored on the bot. It is not a new session.
type State struct {
	Summary string
	Covered int
}

// DefaultWindow stands in for a model whose window providers.json does not
// give. It is a common size, so the derived lines stay on the safe side.
const DefaultWindow = 128_000

// TargetCap and TargetPercent set the soft target from the hard ceiling:
// min(TargetCap, TargetPercent% of the ceiling). A small window keeps room to
// tidy up before the ceiling; a large one stops at TargetCap, past which every
// turn is slower and costlier and the model's attention thins out. The owner
// set both numbers (ADR 0015).
const (
	TargetCap     = 200_000
	TargetPercent = 60
)

// Lines derives the soft target and the hard ceiling from a model window.
// The ceiling leaves room for the reply, min(32K, window/16), and a tenth of
// the window for the estimate's error. Tool definitions are not taken off
// here: they vary by turn, and the kernel subtracts what it last sent.
func Lines(window int) (target, ceiling int) {
	if window <= 0 {
		window = DefaultWindow
	}
	reserve := min(32_000, window/16)
	ceiling = window - reserve - window/10
	if ceiling < 1 {
		ceiling = 1
	}
	return TargetFor(ceiling), ceiling
}

// TargetFor is the soft target that goes with a ceiling.
func TargetFor(ceiling int) int {
	t := min(TargetCap, ceiling*TargetPercent/100)
	if t < 1 {
		t = 1
	}
	return t
}

// DefaultBudget derives both lines from the model and keeps a short verbatim tail.
func DefaultBudget() Budget {
	return Budget{KeepTail: 8}
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
	n := 4 + Estimate(m.PortableText()) + Estimate(m.Name)
	for _, tc := range m.Calls() {
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
	if b.Ceiling <= 0 {
		_, b.Ceiling = Lines(DefaultWindow)
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
		if est <= b.Ceiling || len(recent) <= 1 {
			return st, nil
		}
		keep := b.KeepTail
		if keep >= len(recent) {
			keep = len(recent) - 1
		}
		if keep < 1 {
			keep = 1
		}
		for keep > 1 && Estimate(st.Summary)+EstimateMessages(recent[len(recent)-keep:]) > b.Ceiling {
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
