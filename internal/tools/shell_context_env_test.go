package tools

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

func TestShellContextEnvironmentUsesPreparedCall(t *testing.T) {
	path, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("POSIX sh unavailable")
	}
	type markerKey struct{}
	j := NewJobs(t.TempDir())
	defer j.Shutdown()
	sh := newInterpreterShell(interpreter{name: "sh", path: path})
	sh.BindJobs(j)
	contextCalls := 0
	var seenID string
	sh.BindContextEnvironment(func(ctx context.Context, env []string) []string {
		contextCalls++
		seenID = CallID(ctx)
		marker, _ := ctx.Value(markerKey{}).(string)
		if seenID == "" {
			if marker != "" {
				t.Error("context-free environment inherited a prepared call marker")
			}
		} else {
			retained, ok := j.CallContext(seenID)
			if !ok || retained.Value(markerKey{}) != marker || marker != "prepared" {
				t.Errorf("environment did not receive retained prepared context: id=%q marker=%q", seenID, marker)
			}
			if status, err := j.Status(seenID); err != nil || status.Status != "running" {
				t.Errorf("Status from environment callback: %+v err=%v", status, err)
			}
		}
		return append(env, "XBOT_TEST_CALL_CONTEXT="+seenID+"|"+marker)
	})
	// Probe/precheck callers retain the existing context-free entry point.
	sh.commandEnv(false)
	if contextCalls != 1 || seenID != "" {
		t.Fatalf("context-free environment: calls=%d id=%q", contextCalls, seenID)
	}
	var preparedID string
	res := j.RunCall(context.Background(), CallRequest{
		Name: sh.Name(),
		Prepare: func(ctx context.Context, id string) (context.Context, func()) {
			preparedID = id
			return context.WithValue(ctx, markerKey{}, "prepared"), func() {}
		},
		Run: func(ctx context.Context) CallResult {
			text, err := sh.Run(ctx, json.RawMessage(`{"description":"Inspect prepared call environment","command":"printf '%s' \"$XBOT_TEST_CALL_CONTEXT\""}`))
			return CallResult{Text: text, Err: err}
		},
	})
	want := "exit 0\n" + preparedID + "|prepared"
	if res.Err != nil || res.Job != nil || res.Text != want || preparedID == "" || seenID != preparedID || contextCalls != 2 {
		t.Fatalf("real shell environment: result=%+v id=%q seen=%q calls=%d want=%q", res, preparedID, seenID, contextCalls, want)
	}
	legacyCalls := 0
	sh.BindEnvironment(func(env []string) []string {
		legacyCalls++
		return append(env, "XBOT_TEST_CALL_CONTEXT=legacy")
	})
	out, err := sh.Run(context.Background(), json.RawMessage(`{"description":"Inspect legacy environment binding","command":"printf '%s' \"$XBOT_TEST_CALL_CONTEXT\""}`))
	if err != nil || strings.TrimSpace(out) != "exit 0\nlegacy" || legacyCalls != 1 || contextCalls != 2 {
		t.Fatalf("legacy binding: output=%q err=%v legacy=%d context=%d", out, err, legacyCalls, contextCalls)
	}
}
