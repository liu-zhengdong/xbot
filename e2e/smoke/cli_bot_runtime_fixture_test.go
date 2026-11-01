//go:build smoke

package smoke

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// Independent views fixed by ccdc3f72 / ADR0052, never product DTOs.
type cliBotListLocal struct {
	Name           string
	PersonaPreview string `json:"persona_preview"`
}
type cliBotListRemote struct {
	Name string
	Seen time.Time
}
type cliBotListReply struct {
	Local        *[]cliBotListLocal
	Remote       *[]cliBotListRemote
	Count        int
	File, Access string
	Bytes        int64
	Error        string
}
type cliBotCreateReply struct {
	Name         string
	Applied      bool
	Stage, Error string
}
type cliBotDeleteReply struct {
	Name         string
	Removed      bool
	Stage, Error string
}
type cliBotTokenUse struct {
	Prompt, Completion, Cached, Window int
	TurnCompletion                     int `json:"turn_completion"`
	SessionPrompt                      int `json:"session_prompt"`
	SessionCompletion                  int `json:"session_completion"`
	SessionCached                      int `json:"session_cached"`
	Estimated                          bool
}
type cliBotStatusView struct {
	Bot, State, Tool, Since, Todo string
	ToolSummary                   string `json:"tool_summary"`
	LastMessage                   string `json:"last_message"`
	Backgrounds, Agents           int
	CompletedUnread               int `json:"completed_unread"`
	Tokens                        *cliBotTokenUse
}
type cliBotStepView struct {
	Time                 time.Time
	Tool, State, Summary string
}
type cliBotInfoReply struct {
	Bots         *[]cliBotStatusView
	Steps        *[]cliBotStepView
	Count        int
	File, Access string
	Bytes        int64
	Error        string
}

func cliBotDecode(t *testing.T, result string, dst any) {
	t.Helper()
	if !strings.HasPrefix(result, "exit 0\n") {
		t.Fatalf("source Bot operation failed: %.1024q", result)
	}
	raw := []byte(commandOutput(result))
	if len(raw) > 64<<10 {
		t.Fatalf("Bot JSON exceeded protocol64KiB: %d bytes", len(raw))
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		t.Fatalf("source Bot result is not one complete JSON: %v (bytes=%d)", err, len(raw))
	}
}
