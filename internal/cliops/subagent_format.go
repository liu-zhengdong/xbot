package cliops

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"xbot/internal/clip"
)

// FormatSubagent includes the exact final newlines used by CLI output budgets.
func FormatSubagent(reply SubagentReply) string {
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

// Only the display error may shrink. The original text stays intact.
func boundSubagentReply(reply SubagentReply) (SubagentReply, error) {
	for {
		raw, err := json.Marshal(reply)
		if err == nil && len(raw)+1 <= subagentOutputBudget && len(FormatSubagent(reply)) <= subagentOutputBudget {
			return reply, nil
		}
		if len(reply.Error) <= len("\n[truncated]") {
			return reply, errors.New("subagent: runtime reply facts exceed 32 KiB Shell output budget")
		}
		limit := len(reply.Error) / 2
		if limit < len("\n[truncated]") {
			limit = len("\n[truncated]")
		}
		reply.Error, _ = clip.Bytes(reply.Error, limit)
	}
}
