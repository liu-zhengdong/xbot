package cliops

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// FormatExperienceRecord is the exact human receipt, including its final newline.
func FormatExperienceRecord(reply ExperienceRecordReply) string {
	var out bytes.Buffer
	if reply.Recorded != nil && *reply.Recorded {
		fmt.Fprintf(&out, "recorded experience #%d", reply.ID)
		if reply.To != 0 {
			fmt.Fprintf(&out, " (seq %d-%d)", reply.From, reply.To)
		}
		out.WriteByte('\n')
	} else {
		out.WriteString("experience recording unconfirmed")
		if reply.ID != 0 {
			fmt.Fprintf(&out, " #%d", reply.ID)
		}
		if reply.From != 0 || reply.To != 0 {
			fmt.Fprintf(&out, " (seq %d-%d)", reply.From, reply.To)
		}
		out.WriteString("; database effects may have occurred\n")
	}
	if reply.Error != "" {
		fmt.Fprintf(&out, "error: %s\n", reply.Error)
	}
	return out.String()
}

// FormatExperienceSearch preserves the original semantic text and adds one final
// newline. Artifact metadata is printed as supplied, without reading the file.
func FormatExperienceSearch(reply ExperienceSearchReply) string {
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

// Only the display error may shrink. Actual record acknowledgement and artifact
// facts must survive a later display failure.
func boundExperienceRecordReply(reply ExperienceRecordReply) (ExperienceRecordReply, error) {
	raw, err := json.Marshal(reply)
	if err == nil && len(raw)+1 <= experienceOutputBudget && len(FormatExperienceRecord(reply)) <= experienceOutputBudget {
		return reply, nil
	}
	problem := errors.New("experience: record receipt exceeds 32 KiB Shell output budget; preserve the acknowledgement before retrying")
	reply.Error = problem.Error()
	return reply, problem
}

func boundExperienceSearchReply(reply ExperienceSearchReply) (ExperienceSearchReply, error) {
	raw, err := json.Marshal(reply)
	if err == nil && len(raw)+1 <= experienceOutputBudget && len(FormatExperienceSearch(reply)) <= experienceOutputBudget {
		return reply, nil
	}
	problem := errors.New("experience: search reply exceeds 32 KiB Shell output budget; source must provide a complete result artifact")
	reply.Error = problem.Error()
	return reply, problem
}
