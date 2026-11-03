package cliops

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

const TextOutputBudget = 32 << 10

// TextResult carries semantic text or its complete local JSON artifact.
// Clients print artifact metadata without opening the file.
type TextResult struct {
	Text   string `json:"text,omitempty"`
	File   string `json:"file,omitempty"`
	Access string `json:"access,omitempty"`
	Bytes  int64  `json:"bytes,omitempty"`
	Error  string `json:"error,omitempty"`
}

// FormatTextResult preserves text and artifact facts, including the final newline.
func FormatTextResult(reply TextResult) string {
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

// FitsOutput checks both supported Shell projections, including the JSON newline.
func FitsOutput(value any, human string) bool {
	raw, err := json.Marshal(value)
	return err == nil && len(raw)+1 <= TextOutputBudget && len(human) <= TextOutputBudget
}

// Only the display error may change. Text and artifact facts must survive.
func boundTextResult(reply TextResult, budgetDiagnostic string) (TextResult, error) {
	if FitsOutput(reply, FormatTextResult(reply)) {
		return reply, nil
	}
	problem := errors.New(budgetDiagnostic)
	reply.Error = problem.Error()
	return reply, problem
}
