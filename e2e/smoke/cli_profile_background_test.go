//go:build smoke

package smoke

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// A real inbound webhook is handled by serve.Pass, without an owner messages
// request or cached web Kernel. A second inbound wake proves the host progressed
// beyond the dispatching turn before the held local dev agent calls its CLI.
func TestCLIProfileServeBackground(t *testing.T) {
	cliProfileIsolation(t)
	const parentCue = "CLI_PASS_DISPATCH"
	const probeCue = "CLI_PASS_LATER_WAKE"
	const parentDone = "CLI_PASS_MAIN_FINISHED"
	const childTask = "CLI_PASS_CHILD_PROFILE"
	childEntered, childRelease := make(chan struct{}), make(chan struct{})
	probeEntered := make(chan struct{})
	childResult := make(chan string, 1)
	var enterOnce, releaseOnce, probeOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(childRelease) }) }
	defer release()
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var req stubRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			http.Error(w, "invalid scripted model request", 400)
			return
		}
		caller, _ := classify(req)
		if caller == callerCompaction || caller == callerExtract {
			streamText(w, []string{"No durable facts needed."})
			return
		}
		if caller == callerSubagent && strings.Contains(joined(req), childTask) {
			if !sawTool(req, "sh") {
				enterOnce.Do(func() { close(childEntered) })
				select {
				case <-childRelease:
				case <-r.Context().Done():
					return
				}
				streamTool(w, "cli_pass_child_shell", "sh", argsJSON(map[string]string{
					"description": "Update own profile after the parent serve turn has ended",
					"command":     `"$XBOT_BIN" profile set --self --description 'background runtime saved' --timezone Asia/Tokyo --json && "$XBOT_BIN" profile show --self --json`,
				}))
				return
			}
			_, _, result := toolProgress(req)
			select {
			case childResult <- result:
			default:
			}
			streamText(w, []string{"Background profile acceptance finished."})
			return
		}
		text := lastUser(req)
		if strings.Contains(text, probeCue) {
			probeOnce.Do(func() { close(probeEntered) })
			streamOwner(w, req, "Later inbound wake accepted.")
			return
		}
		if strings.Contains(text, parentCue) {
			if !sawTool(req, "dispatch") {
				streamTool(w, "cli_pass_dispatch", "dispatch", argsJSON(map[string]string{"type": "dev", "task": childTask}))
				return
			}
			if sawTool(req, "send_message_to_user") {
				streamText(w, []string{parentDone})
				return
			}
			streamOwner(w, req, "Local dev agent has started.")
			return
		}
		streamOwner(w, req, "Background completion received.")
	}))
	t.Cleanup(model.Close)
	srv := Start(t, &Stub{srv: model})
	srv.CreateBot("Worker", "Independent acceptance of background CLI self operations.")
	srv.PatchBot("Worker", map[string]any{"description": "before background", "timezone": "UTC"})
	status, body := srv.do(http.MethodPost, "/api/bots/Worker/webhooks", map[string]string{"name": "cli-profile", "auth": "bearer"})
	if status != http.StatusCreated {
		t.Fatalf("create private webhook: HTTP %d", status)
	}
	var hook struct {
		URL    string `json:"local_url"`
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(body, &hook); err != nil || hook.URL == "" || hook.Secret == "" {
		t.Fatal("private webhook did not return the documented local address and credential")
	}
	post := func(text, delivery string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, hook.URL, strings.NewReader(text))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+hook.Secret)
		req.Header.Set("X-XBot-Delivery", delivery)
		resp, err := srv.client.Do(req)
		if err != nil {
			t.Fatal("private webhook delivery failed")
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("private webhook delivery: HTTP %d", resp.StatusCode)
		}
	}
	post(parentCue, "cli-profile-first")
	select {
	case <-childEntered:
	case <-time.After(15 * time.Second):
		t.Fatal("serve's inbound turn did not dispatch the real local dev agent")
	}
	WaitFor(t, 10*time.Second, "the dispatching serve turn's final transcript row", func() (bool, string) {
		for _, row := range srv.Transcript("Worker") {
			if row.Role == "assistant" && len(row.ToolCalls) == 0 && row.Content == parentDone {
				return true, ""
			}
		}
		return false, "original inbound turn has not finished"
	})
	// This goes through the real coalescer and inbox again, rather than sleeping
	// for an assumed lock/listener lifetime or calling a kernel cleanup helper.
	post(probeCue, "cli-profile-later")
	select {
	case <-probeEntered:
	case <-time.After(15 * time.Second):
		t.Fatal("serve did not progress to the later inbound wake while its local agent was running")
	}
	release()
	var result string
	select {
	case result = <-childResult:
	case <-time.After(15 * time.Second):
		t.Fatal("local agent's CLI did not return after its parent serve turn ended")
	}
	if !strings.HasPrefix(result, "exit 0\n") {
		t.Fatalf("background caller lost its runtime after parent turn completion:\n%s", result)
	}
	lines := strings.Split(strings.TrimSpace(commandOutput(result)), "\n")
	if len(lines) != 2 {
		t.Fatalf("background set/show did not each emit one JSON result: %s", result)
	}
	for _, line := range lines {
		var value map[string]any
		if err := json.Unmarshal([]byte(line), &value); err != nil {
			t.Fatalf("background CLI returned invalid JSON: %v", err)
		}
		cliProfileWant(t, value, "Worker", "background runtime saved", "Asia/Tokyo")
		if value["scope"] != "runtime" {
			t.Fatalf("background CLI did not use its existing runtime: %v", value)
		}
	}
	cliProfileWant(t, cliProfileDisk(t, srv.Root, "Worker"), "Worker", "background runtime saved", "Asia/Tokyo")
	t.Log("real inbound serve wake -> local dev agent -> parent completed -> later wake -> background Shell self set/show remained usable")
}
