package kernel

import (
	"fmt"
	"strings"
	"time"

	"xbot/internal/bot"
	"xbot/internal/clip"
	"xbot/internal/link"
	"xbot/internal/peer"
	"xbot/internal/provider"
)

const (
	recallLimit  = 8
	recallCap    = 4 << 10
	expandCap    = 8 << 10
	snippetRunes = 240
)

// RecallHistory searches this bot's raw append-only transcript.
// Sequence numbers stay valid after compaction.
func (k *Kernel) RecallHistory(query string) (string, error) {
	return k.RecallHistoryBot(query, "")
}

// RecallHistoryBot searches a transcript. An empty name, or this bot's name, searches this bot.
// Another bot is allowed only when it shares an owner. The text is then untrusted data from that bot.
func (k *Kernel) RecallHistoryBot(query, name string) (string, error) {
	target, foreign, err := k.historyBot(name)
	if err != nil {
		return "", err
	}
	seqs, err := target.Search(query, recallLimit)
	if err != nil {
		return "", err
	}
	if len(seqs) == 0 {
		return k.presentHistory("no matching transcript lines", target.Name(), foreign, recallCap), nil
	}
	var b strings.Builder
	for _, seq := range seqs {
		part, err := target.Slice(seq, seq)
		if err != nil {
			return "", err
		}
		if len(part) == 0 {
			continue
		}
		e := part[0]
		snip := oneLine(e.Message.Content)
		if snip == "" {
			snip = oneLine(toolBlob(e.Message))
		}
		if len(snip) > snippetRunes {
			snip = snip[:snippetRunes]
		}
		fmt.Fprintf(&b, "%d %s: %s\n", seq, e.Message.Role, snip)
	}
	return k.presentHistory(b.String(), target.Name(), foreign, recallCap), nil
}

// Expand returns this bot's raw transcript text for an inclusive sequence range, capped.
func (k *Kernel) Expand(from, to int) (string, error) {
	return k.ExpandBot("", from, to)
}

// ExpandBot returns raw transcript text. The bot rules match RecallHistoryBot.
func (k *Kernel) ExpandBot(name string, from, to int) (string, error) {
	target, foreign, err := k.historyBot(name)
	if err != nil {
		return "", err
	}
	n, err := target.Count()
	if err != nil {
		return "", err
	}
	if from < 1 || to < from || to > n {
		return "", fmt.Errorf("expand: range %d-%d outside 1-%d", from, to, n)
	}
	entries, err := target.Slice(from, to)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for i, e := range entries {
		e, err = target.HydrateLimit(e, expandCap)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "# seq %d %s\n%s\n", from+i, e.Message.Role, e.Message.Content)
		if blob := toolBlob(e.Message); blob != "" && e.Message.Content == "" {
			b.WriteString(blob)
			b.WriteByte('\n')
		}
	}
	return k.presentHistory(b.String(), target.Name(), foreign, expandCap), nil
}

// historyBot resolves an optional bot name. foreign is true when the transcript is not this bot's.
func (k *Kernel) historyBot(name string) (*bot.Bot, bool, error) {
	name = strings.TrimSpace(name)
	if name == "" || name == k.Bot.Name() {
		return k.Bot, false, nil
	}
	other, err := bot.Load(k.Bot.Root, name)
	if err != nil || !peer.SameOwner(k.Bot, other) {
		// Missing bots, other owners, and group ids are the same answer.
		return nil, false, fmt.Errorf("history: %w", bot.ErrNotFound)
	}
	return other, true, nil
}

func (k *Kernel) presentHistory(body, botName string, foreign bool, cap int) string {
	redactFn := bot.RedactText
	if k != nil && k.Redact != nil {
		redactFn = k.Redact
	}
	body = redactFn(strings.TrimRight(body, "\n"))
	if !foreign {
		out, _ := clip.Bytes(body, cap)
		return out
	}
	at := time.Now().UTC()
	ev := link.Event{Source: "bot:" + botName, Type: "transcript", Sender: botName, Time: at}
	overhead := len(link.Wrap(ev))
	budget := cap - overhead
	if budget < 1 {
		budget = 1
	}
	clipped, _ := clip.Bytes(body, budget)
	ev.Content = clipped
	out := link.Wrap(ev)
	if len(out) > cap {
		out, _ = clip.Bytes(out, cap)
	}
	return out
}

func toolBlob(m provider.Message) string {
	var b strings.Builder
	for _, tc := range m.ToolCalls {
		fmt.Fprintf(&b, "%s %s ", tc.Function.Name, tc.Function.Arguments)
	}
	return strings.TrimSpace(b.String())
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.Join(strings.Fields(s), " ")
}

// Seq is the 1-based position of an entry. The transcript is append-only, so the number does not move.
func Seq(entries []bot.Entry, i int) int { return i + 1 }
