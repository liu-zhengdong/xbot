package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"xbot/internal/cliops"
	"xbot/internal/tools"
)

func TestBotControlCLIEarlyDispatchAndHelp(t *testing.T) {
	t.Setenv("XBOT_ROOT", "")
	t.Setenv("XBOT_ENDPOINT", "")
	absent := filepath.Join(t.TempDir(), "must-stay-absent")
	for _, action := range []string{"pin", "unpin", "primary"} {
		output := captureStdout(t, func() error { return run([]string{"--root", absent, action, "--help"}) })
		if !strings.Contains(output, "primary set NAME --self") || !strings.Contains(output, "offline") {
			t.Fatalf("help %s: %s", action, output)
		}
	}
	for _, args := range [][]string{
		{"pin"}, {"unpin", "a", "b"}, {"primary", "set"},
		{"pin", "a", "--self"}, {"unpin", "a", "--json"}, {"primary", "--self"},
		{"pin", "a", "--unknown"}, {"primary", "set", "a", "--self", "--self"},
	} {
		if err := run(append([]string{"--root", absent}, args...)); err == nil {
			t.Errorf("accepted invalid/unavailable command %q", args)
		}
	}
	if _, err := os.Stat(absent); !os.IsNotExist(err) {
		t.Fatalf("help/error initialized root: %v", err)
	}
	root := t.TempDir()
	t.Setenv("XBOT_ROOT", root)
	for _, args := range [][]string{{"pin", "a", "--self"}, {"unpin", "a", "--self"}, {"primary", "set", "a", "--self"}} {
		var out bytes.Buffer
		if err := runBotRuntime(context.Background(), "", false, args, nil, &out); err == nil || !strings.Contains(err.Error(), "endpoint") || out.Len() != 0 {
			t.Fatalf("missing endpoint %q: out=%q error=%v", args, out.String(), err)
		}
		if err := runBotRuntime(context.Background(), "", true, args, nil, &out); err == nil || !strings.Contains(err.Error(), "explicit --root") {
			t.Fatalf("empty explicit root %q: error=%v", args, err)
		}
	}
}

func TestBotControlCLIRequestsPartialAndRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XBOT_ROOT", root)
	t.Setenv("XBOT_BOT", "stale-diagnostic")
	t.Setenv("XBOT_CALL_ID", "must-not-send")
	calls := 0
	source, err := cliops.Start(root, cliops.Handlers{Bots: &cliops.BotHandlers{
		Pin: func(ctx context.Context, input cliops.BotPin) (cliops.BotPinReply, error) {
			calls++
			if (input.Name != "ada" && input.Name != "partial") || cliops.SourceCallID(ctx) != "" {
				t.Errorf("pin=%+v call=%q", input, cliops.SourceCallID(ctx))
			}
			if input.Name == "partial" {
				return cliops.BotPinReply{Name: input.Name, Pinned: input.Pinned, Stage: "config"}, errors.New("config save failed")
			}
			return cliops.BotPinReply{Name: input.Name, Pinned: input.Pinned, Applied: true, Stage: "complete"}, nil
		},
		Primary: func(ctx context.Context, name string) (cliops.BotPrimaryReply, error) {
			calls++
			if name != "ada" || cliops.SourceCallID(ctx) != "" {
				t.Errorf("primary=%q call=%q", name, cliops.SourceCallID(ctx))
			}
			return cliops.BotPrimaryReply{Name: name, Primary: name, Applied: true, Stage: "pin"}, errors.New("pin save failed")
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	t.Setenv("XBOT_ENDPOINT", source.Endpoint)
	for _, tc := range []struct {
		args    []string
		want    string
		partial bool
	}{
		{[]string{"pin", "ada", "--self"}, "name: ada\npinned: true\napplied: true\nstage: complete\n", false},
		{[]string{"unpin", "ada", "--self", "--json"}, "{\"name\":\"ada\",\"pinned\":false,\"applied\":true,\"stage\":\"complete\"}\n", false},
		{[]string{"unpin", "partial", "--self"}, "name: partial\npinned: false\napplied: false\nstage: config\nerror: config save failed\n", true},
		{[]string{"unpin", "partial", "--self", "--json"}, "{\"name\":\"partial\",\"pinned\":false,\"applied\":false,\"stage\":\"config\",\"error\":\"config save failed\"}\n", true},
		{[]string{"primary", "set", "ada", "--self"}, "name: ada\nprimary: ada\napplied: true\nstage: pin\nerror: pin save failed\n", true},
		{[]string{"primary", "set", "ada", "--self", "--json"}, "{\"name\":\"ada\",\"primary\":\"ada\",\"applied\":true,\"stage\":\"pin\",\"error\":\"pin save failed\"}\n", true},
	} {
		var out bytes.Buffer
		err := runBotRuntime(context.Background(), "", false, tc.args, nil, &out)
		if (err != nil) != tc.partial || out.String() != tc.want {
			t.Fatalf("args=%q out=%q error=%v", tc.args, out.String(), err)
		}
	}
	var out bytes.Buffer
	if err := runBotRuntime(context.Background(), t.TempDir(), true, []string{"pin", "ada", "--self"}, nil, &out); err == nil || !strings.Contains(err.Error(), "mismatch") || calls != 6 || out.Len() != 0 {
		t.Fatalf("root mismatch calls=%d output=%q error=%v", calls, out.String(), err)
	}
	output := captureStdout(t, func() error { return run([]string{"--root", root, "unpin", "ada", "--self", "--json"}) })
	if !strings.Contains(output, `"pinned":false`) || calls != 7 {
		t.Fatalf("main dispatch calls=%d out=%q", calls, output)
	}
}

func TestBotControlCLIBothOutputBudgets(t *testing.T) {
	t.Setenv("XBOT_ROOT", t.TempDir())
	var body []byte
	requests := 0
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodPut || r.Header.Get("X-XBot-Call-ID") != "" {
			t.Errorf("method=%s call=%q", r.Method, r.Header.Get("X-XBot-Call-ID"))
		}
		_, _ = w.Write(body)
	}))
	defer source.Close()
	t.Setenv("XBOT_ENDPOINT", source.URL)
	for _, action := range []string{"pin", "primary"} {
		args := []string{action, "ada", "--self"}
		if action == "primary" {
			args = []string{"primary", "set", "ada", "--self"}
		}
		for _, extra := range []int{0, 1} {
			var reply any
			var text string
			if action == "pin" {
				value := cliops.BotPinReply{Applied: true, Stage: "complete"}
				raw, _ := json.Marshal(value)
				value.Name = strings.Repeat("x", tools.DefaultShellOutputBytes-len(raw)-1+extra)
				reply, text = value, cliops.FormatBotPin(value)
			} else {
				value := cliops.BotPrimaryReply{Applied: true, Stage: "complete"}
				raw, _ := json.Marshal(value)
				value.Name = strings.Repeat("x", tools.DefaultShellOutputBytes-len(raw)-1+extra)
				reply, text = value, cliops.FormatBotPrimary(value)
			}
			body, _ = json.Marshal(reply)
			if len(body)+1 != tools.DefaultShellOutputBytes+extra || len(text) > tools.DefaultShellOutputBytes {
				t.Fatalf("bad newline boundary fixture JSON=%d human=%d", len(body)+1, len(text))
			}
			for _, asJSON := range []bool{false, true} {
				command := append([]string{}, args...)
				if asJSON {
					command = append(command, "--json")
				}
				var out bytes.Buffer
				err := runBotRuntime(context.Background(), "", false, command, nil, &out)
				if extra == 1 {
					if err == nil || out.Len() != 0 {
						t.Fatalf("overflow %q output=%d error=%v", command, out.Len(), err)
					}
				} else {
					want := text
					if asJSON {
						want = string(body) + "\n"
					}
					if err != nil || out.String() != want {
						t.Fatalf("exact boundary %q output=%d error=%v", command, out.Len(), err)
					}
				}
			}
		}
		// Escaping can exceed the JSON budget while human output remains short.
		if action == "pin" {
			body, _ = json.Marshal(cliops.BotPinReply{Name: strings.Repeat(`"`, 17000), Stage: "complete"})
		} else {
			body, _ = json.Marshal(cliops.BotPrimaryReply{Name: strings.Repeat(`"`, 17000), Stage: "complete"})
		}
		for _, asJSON := range []bool{false, true} {
			command := append([]string{}, args...)
			if asJSON {
				command = append(command, "--json")
			}
			var out bytes.Buffer
			if err := runBotRuntime(context.Background(), "", false, command, nil, &out); err == nil || out.Len() != 0 {
				t.Fatalf("escaped JSON overflow %q output=%d error=%v", command, out.Len(), err)
			}
		}
	}
	if requests != 12 {
		t.Fatalf("requests=%d want 12 without retry", requests)
	}
}
