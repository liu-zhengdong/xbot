package cliops

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"xbot/internal/clip"
)

// FormatCompact includes the exact final newlines used by CLI output budgets.
func FormatCompact(reply CompactReply) string {
	var out bytes.Buffer
	if reply.Text != "" {
		out.WriteString(reply.Text)
		out.WriteByte('\n')
	}
	if reply.Error != "" {
		fmt.Fprintf(&out, "error: %s\n", reply.Error)
	}
	return out.String()
}

// Only the display error may shrink. The original acknowledgement stays intact.
func boundCompactReply(reply CompactReply) (CompactReply, error) {
	for {
		raw, err := json.Marshal(reply)
		if err == nil && len(raw)+1 <= compactOutputBudget && len(FormatCompact(reply)) <= compactOutputBudget {
			return reply, nil
		}
		if len(reply.Error) <= len("\n[truncated]") {
			return reply, errors.New("compact: runtime reply facts exceed 32 KiB Shell output budget")
		}
		limit := len(reply.Error) / 2
		if limit < len("\n[truncated]") {
			limit = len("\n[truncated]")
		}
		reply.Error, _ = clip.Bytes(reply.Error, limit)
	}
}
