package cliops

import (
	"bytes"
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

// Only the display error may shrink. Actual record acknowledgement and artifact
// facts must survive a later display failure.
func boundExperienceRecordReply(reply ExperienceRecordReply) (ExperienceRecordReply, error) {
	if FitsOutput(reply, FormatExperienceRecord(reply)) {
		return reply, nil
	}
	problem := errors.New("experience: record receipt exceeds 32 KiB Shell output budget; preserve the acknowledgement before retrying")
	reply.Error = problem.Error()
	return reply, problem
}
