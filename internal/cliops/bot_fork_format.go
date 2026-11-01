package cliops

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// FormatBotFork is pure and includes the final newline used by the source and
// CLI output budget checks. The receipt contains no duties or handoff body.
func FormatBotFork(reply BotForkReply) string {
	var out bytes.Buffer
	if reply.Name != "" {
		fmt.Fprintf(&out, "name: %s\n", strconv.Quote(reply.Name))
	}
	fmt.Fprintf(&out, "created: %t\n", reply.Created)
	if reply.Created {
		out.WriteString("creation receipt: original Bot config acknowledged; later setup follows the confirmations below\n")
	}
	if reply.CreateStage != "" {
		fmt.Fprintf(&out, "create_stage: %s\n", reply.CreateStage)
	}
	if len(reply.Confirmed) > 0 {
		fmt.Fprintf(&out, "confirmed: %s\n", strings.Join(reply.Confirmed, ", "))
	}
	fmt.Fprintf(&out, "handoff_parts: %d\nhandoff_total: %d\n", reply.HandoffParts, reply.HandoffTotal)
	out.WriteString("handoff_parts counts only successful Remember acknowledgements\n")
	if reply.UncertainMemory != nil {
		fmt.Fprintf(&out, "uncertain_memory: bot=%s key=%s\n", strconv.Quote(reply.UncertainMemory.Bot), strconv.Quote(reply.UncertainMemory.Key))
		out.WriteString("memory outcome unknown: this attempt may already have written Markdown\n")
	}
	if reply.GroupID != "" {
		fmt.Fprintf(&out, "group_id: %s\n", reply.GroupID)
	}
	fmt.Fprintf(&out, "stage: %s\n", reply.Stage)
	if reply.Error != "" {
		fmt.Fprintf(&out, "error: %s\n", strconv.Quote(reply.Error))
		out.WriteString("preserve this receipt; do not blindly retry a partial or uncertain fork\n")
	}
	return out.String()
}

// Only display fields may shrink. Actual creation, acknowledgements, memory
// uncertainty and group facts survive a display failure.
func boundBotForkReply(reply BotForkReply) (BotForkReply, error) {
	fits := func(value BotForkReply) bool {
		raw, err := json.Marshal(value)
		return err == nil && len(raw)+1 <= botForkOutputBudget && len(FormatBotFork(value)) <= botForkOutputBudget
	}
	if fits(reply) {
		return reply, nil
	}
	problem := errors.New("bot-fork: runtime receipt exceeds 32 KiB Shell output budget; preserve receipt before considering another fork")
	reply.Error = problem.Error()
	if !fits(reply) {
		reply.Name = ""
	}
	return reply, problem
}
