package cliops

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// FormatRoutineList is the complete human-readable CLI representation, including
// its final newline. Projection uses the same output to enforce caller budgets.
func FormatRoutineList(reply RoutineListReply) string {
	var text bytes.Buffer
	fmt.Fprintf(&text, "count: %d\n", reply.Count)
	if reply.Routines != nil {
		for _, routine := range *reply.Routines {
			skills, _ := json.Marshal(routine.Skills)
			fmt.Fprintf(&text, "id: %s\nname: %s\nprompt: %s\nprecheck: %s\nskills: %s\ncron: %s\nat: %s\nenabled: %t\nlast_slot: %s\nzone: %s\n", routine.ID, routine.Name, routine.Prompt, routine.Precheck, skills, routine.Cron, routine.At, routine.Enabled, routine.LastSlot, routine.Zone)
		}
	}
	if reply.File != "" {
		fmt.Fprintf(&text, "%s (access: %s, bytes: %d)\n", reply.File, reply.Access, reply.Bytes)
	}
	if reply.Error != "" {
		fmt.Fprintf(&text, "error: %s\n", reply.Error)
	}
	return text.String()
}
