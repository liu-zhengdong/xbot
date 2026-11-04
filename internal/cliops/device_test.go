package cliops

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestDeviceParserRequiresSelfAndLiteralValues(t *testing.T) {
	for _, args := range [][]string{
		{"list", "--self"}, {"exec", "--self", "--args", `{"command":"pwd"}`}, {"read", "--self", "--args-file", "-"},
		{"task", "dc_a", "--self", "--offset", "5", "--limit", "10"}, {"artifact", "dc_a", "--self", "--artifact-id", "file"}, {"desktop", "--help"},
	} {
		if _, err := ParseDeviceRuntime(args); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	for _, args := range [][]string{
		{"list"}, {"exec", "--self"}, {"task", "--self"}, {"read", "--self", "--args", "{}", "--args-file", "-"},
		{"tasks", "--self", "--limit", "-1"}, {"tasks", "--self", "--readonly"}, {"list", "--self", "--self"}, {"browser", "--self", "--args", "{}"},
	} {
		c, err := ParseDeviceRuntime(args)
		if err == nil {
			t.Fatalf("accepted %v %+v", args, c)
		}
	}
	c, err := ParseDeviceRuntime([]string{"exec", "--self", "--args", "--help"})
	if err != nil || c.Help || c.Args != "--help" {
		t.Fatalf("literal help changed %+v %v", c, err)
	}
	c, err = ParseDeviceRuntime([]string{"exec", "--self", "--args", "{}"})
	if err != nil || c.Tool != "" {
		t.Fatalf("default interpreter must be selected at target %+v %v", c, err)
	}
}

func TestDeviceTransportSourceAndPartialReceipt(t *testing.T) {
	root := t.TempDir()
	calls := 0
	server, err := Start(root, Handlers{Device: func(ctx context.Context, in DeviceRequest) (json.RawMessage, error) {
		calls++
		if SourceCallID(ctx) != "job-original" || in.Action != "list" {
			t.Errorf("identity/request %q %+v", SourceCallID(ctx), in)
		}
		return json.RawMessage(`{"task":{"ref":"dc_a","status":"running"}}`), errors.New("late failure")
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	raw, err := CallDevice(context.Background(), server.Endpoint, root, "job-original", DeviceRequest{Action: "list"})
	if err == nil || !strings.Contains(string(raw), `"task"`) || calls != 1 {
		t.Fatalf("lost actual receipt %s %v calls=%d", raw, err, calls)
	}
	canonical, _ := CanonicalRoot(root)
	for _, body := range []string{`{"action":"list","source":{"readonly":false}}`, `{"action":"list","bot":"fake"}`, `{"action":"list"} {}`, `null`, `{"action":"unknown"}`} {
		req, _ := http.NewRequest(http.MethodPost, server.Endpoint+"/v1/device", strings.NewReader(body))
		req.Header.Set("X-XBot-Root", canonical)
		client := LocalHTTPClient(0)
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		client.CloseIdleConnections()
		if res.StatusCode != http.StatusBadRequest || calls != 1 {
			t.Fatalf("spoofed request reached callback: %s status=%d calls=%d", body, res.StatusCode, calls)
		}
	}
}
