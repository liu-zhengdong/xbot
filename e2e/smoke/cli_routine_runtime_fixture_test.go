//go:build smoke

package smoke

import (
	"encoding/json"
	"strings"
	"testing"
)

// Independent wire views fixed by contract3e3067d4, not implementation types.
type cliRoutineView struct {
	ID, Name, Prompt, Cron, At, Zone, Precheck string
	Enabled                                    bool
	Skills                                     []string
	LastSlot                                   string `json:"last_slot"`
}
type cliRoutineWriteReply struct {
	ID           string
	Applied      bool
	Stage, Error string
}
type cliRoutineListReply struct {
	Routines     *[]cliRoutineView
	Count        int
	File, Access string
	Bytes        int64
	Error        string
}

func cliRoutineDecode(t *testing.T, result string, dst any) {
	t.Helper()
	if !strings.HasPrefix(result, "exit 0\n") {
		t.Fatalf("source routine operation failed: %.1024q", result)
	}
	raw := []byte(commandOutput(result))
	if len(raw) > 64<<10 {
		t.Fatalf("routine JSON exceeded protocol64KiB: %d bytes", len(raw))
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		t.Fatalf("source routine result is not one complete JSON reply: %v (bytes=%d)", err, len(raw))
	}
}
