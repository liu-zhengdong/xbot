//go:build smoke

package smoke

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"testing"
	"time"

	"xbot/internal/browsertest"
)

// Continue the already-running binary smoke case through its shipped Web UI.
// No browser routes, product HTTP responses or rendering functions are mocked.
func continueSubagentViaBrowser(t *testing.T, srv *Server, id, feedback, firstBody, firstConclusion, secondConclusion string) (int, []byte) {
	t.Helper()
	chrome := browsertest.Chrome(t)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	cookies := srv.client.Jar.Cookies(u)
	if len(cookies) == 0 {
		t.Fatal("real serve did not establish the browser login cookie")
	}
	input, err := json.Marshal(map[string]any{
		"chrome": chrome, "url": srv.URL, "cookies": cookies, "bot": "collaborator",
		"id": id, "feedback": feedback, "firstBody": firstBody,
		"firstConclusion": firstConclusion, "secondConclusion": secondConclusion,
		"artifacts": os.Getenv("XBOT_SMOKE_ARTIFACT_DIR"),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "testdata/continuable-subagent.cjs")
	cmd.Stdin = bytes.NewReader(input)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("same-server child browser continuation: %v\n%s", err, out)
	}
	var result struct {
		Status    int             `json:"status"`
		Receipt   json.RawMessage `json:"receipt"`
		Artifacts []string        `json:"artifacts"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out), &result); err != nil {
		t.Fatalf("browser did not return the actual continuation response: %v\n%s", err, out)
	}
	t.Logf("real Chrome opened child %s, read its persisted tool call/result and sent feedback to the same serve; artifacts=%v", id, result.Artifacts)
	return result.Status, result.Receipt
}
