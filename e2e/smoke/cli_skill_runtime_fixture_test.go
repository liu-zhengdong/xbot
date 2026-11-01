//go:build smoke

package smoke

import (
	"encoding/json"
	"strings"
	"testing"
)

// Independent wire views fixed by ca6a8530; never use implementation DTOs to
// manufacture a passing expected response.
type cliSkillReadReply struct {
	Slug, Name, Title, Description string
	Body                           *string
	Preview, File, Access          string
	Bytes                          int64
	Error                          string
}
type cliSkillWriteReply struct {
	Slug          string
	Written       bool
	Scope, Target string
	UpdatedBots   []string `json:"updated_bots"`
	Error         string
}

func cliSkillIsolation(t *testing.T) {
	t.Helper()
	for _, key := range []string{"XBOT_VAULT_SOCKET", "XBOT_SMOKE_OAUTH", "XBOT_DEV_STATIC", "XBOT_ROOT", "XBOT_ENDPOINT", "XBOT_BIN", "XBOT_BOT", "XBOT_RUNNER_URL", "XBOT_RUNNER_TOKEN", "XBOT_CODE_AGENT"} {
		t.Setenv(key, "")
	}
}
func cliSkillDecode(t *testing.T, result string, dst any) {
	t.Helper()
	if !strings.HasPrefix(result, "exit 0\n") {
		t.Fatalf("source skill operation did not succeed: %.1024q", result)
	}
	raw := []byte(commandOutput(result))
	if len(raw) > 64<<10 {
		t.Fatalf("skill JSON exceeded 64KiB: %d bytes", len(raw))
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		t.Fatalf("skill output is not one JSON object: %v; prefix=%.1024q", err, raw)
	}
}
