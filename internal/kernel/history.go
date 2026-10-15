package kernel

import (
	"fmt"
	"strings"

	"xbot/internal/bm25"
	"xbot/internal/bot"
	"xbot/internal/clip"
	"xbot/internal/provider"
)

const (
	recallLimit  = 8
	recallCap    = 4 << 10
	expandCap    = 8 << 10
	snippetRunes = 240
)

// RecallHistory searches the raw append-only transcript. Sequence numbers stay valid after compaction.
func (k *Kernel) RecallHistory(query string) (string, error) {
	entries, err := k.Bot.Entries()
	if err != nil {
		return "", err
	}
	docs := make([]string, len(entries))
	for i, e := range entries {
		docs[i] = e.Message.Role + " " + e.Message.Content + " " + toolBlob(e.Message)
	}
	hits := bm25.Top(query, docs, recallLimit)
	if len(hits) == 0 {
		return "no matching transcript lines", nil
	}
	var b strings.Builder
	for _, h := range hits {
		e := entries[h.Index]
		snip := oneLine(e.Message.Content)
		if snip == "" {
			snip = oneLine(toolBlob(e.Message))
		}
		if len(snip) > snippetRunes {
			snip = snip[:snippetRunes]
		}
		fmt.Fprintf(&b, "%d %s: %s\n", h.Index+1, e.Message.Role, snip)
	}
	out, _ := clip.Bytes(strings.TrimRight(b.String(), "\n"), recallCap)
	return out, nil
}

// Expand returns the raw transcript text for an inclusive sequence range, capped.
func (k *Kernel) Expand(from, to int) (string, error) {
	entries, err := k.Bot.Entries()
	if err != nil {
		return "", err
	}
	if from < 1 || to < from || to > len(entries) {
		return "", fmt.Errorf("expand: range %d-%d outside 1-%d", from, to, len(entries))
	}
	var b strings.Builder
	for i := from - 1; i < to; i++ {
		e := entries[i]
		fmt.Fprintf(&b, "# seq %d %s\n%s\n", i+1, e.Message.Role, e.Message.Content)
		if blob := toolBlob(e.Message); blob != "" && e.Message.Content == "" {
			b.WriteString(blob)
			b.WriteByte('\n')
		}
	}
	out, _ := clip.Bytes(strings.TrimRight(b.String(), "\n"), expandCap)
	return out, nil
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
