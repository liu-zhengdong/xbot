package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"xbot/internal/cliops"
)

func TestDeviceRuntimeCLIStableRefAndPreparedCorrelation(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XBOT_ROOT", root)
	t.Setenv("XBOT_CALL_ID", "original-shell")
	t.Setenv("XBOT_BOT", "spoofed-diagnostic")
	calls := 0
	var got cliops.DeviceRequest
	server, err := cliops.Start(root, cliops.Handlers{Device: func(ctx context.Context, in cliops.DeviceRequest) (json.RawMessage, error) {
		calls++
		got = in
		if cliops.SourceCallID(ctx) != "original-shell" {
			t.Errorf("call association %q", cliops.SourceCallID(ctx))
		}
		return json.RawMessage(`{"task":{"ref":"accepted","status":"running"}}`), errors.New("receipt uncertain")
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	t.Setenv("XBOT_ENDPOINT", server.Endpoint)
	var out bytes.Buffer
	err = runDeviceRuntime(context.Background(), "", false, []string{"exec", "--self", "--device", "target", "--args-file", "-", "--json"}, strings.NewReader(`{"command":"printf marker","n":9007199254740993}`), &out)
	if err == nil || calls != 1 || got.Ref == "" || got.Ref != got.Call.Ref || got.Call.Tool != "" || got.DeviceID != "target" || !strings.Contains(string(got.Call.Args), "9007199254740993") || !strings.Contains(out.String(), `"task"`) {
		t.Fatalf("request=%+v out=%s err=%v calls=%d", got, out.String(), err, calls)
	}
	t.Setenv("XBOT_CALL_ID", "")
	reader := strings.NewReader(`{}`)
	out.Reset()
	if err := runDeviceRuntime(context.Background(), "", false, []string{"exec", "--self", "--args-file", "-"}, reader, &out); err == nil || reader.Len() != 2 || calls != 1 {
		t.Fatalf("missing source read stdin or sent request: %v", err)
	}
}

func TestDeviceRuntimeHelpStaysOffline(t *testing.T) {
	t.Setenv("XBOT_ENDPOINT", "")
	t.Setenv("XBOT_ROOT", "")
	absent := filepath.Join(t.TempDir(), "absent")
	for _, args := range [][]string{{"--help"}, {"exec", "--help"}, {"write", "--args-file", "missing", "--help"}} {
		var out bytes.Buffer
		if err := runDeviceRuntime(context.Background(), absent, true, args, nil, &out); err != nil || out.String() != deviceUsage {
			t.Fatalf("offline help %v: %v %s", args, err, out.String())
		}
	}
	if _, err := os.Stat(absent); !os.IsNotExist(err) {
		t.Fatalf("offline initialized root: %v", err)
	}
}
