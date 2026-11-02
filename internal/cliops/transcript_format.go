package cliops

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// FormatTranscriptRead preserves semantic text and artifact facts exactly,
// including the final newline used by both budget checks and CLI printing.
func FormatTranscriptRead(reply TranscriptReadReply) string {
	var out bytes.Buffer
	if reply.Text != "" {
		out.WriteString(reply.Text)
		out.WriteByte('\n')
	}
	if reply.File != "" {
		fmt.Fprintf(&out, "%s (access: %s, bytes: %d)\n", reply.File, reply.Access, reply.Bytes)
	}
	if reply.Error != "" {
		fmt.Fprintf(&out, "error: %s\n", reply.Error)
	}
	return out.String()
}

// Only the display error may shrink; text and actual artifact facts survive.
func boundTranscriptReadReply(reply TranscriptReadReply) (TranscriptReadReply, error) {
	raw, err := json.Marshal(reply)
	if err == nil && len(raw)+1 <= transcriptOutputBudget && len(FormatTranscriptRead(reply)) <= transcriptOutputBudget {
		return reply, nil
	}
	problem := errors.New("transcript: reply exceeds 32 KiB Shell output budget; source must provide a complete result artifact")
	reply.Error = problem.Error()
	return reply, problem
}
