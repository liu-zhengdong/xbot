package kernel

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"strings"
	"testing"
	"xbot/internal/work"

	"xbot/internal/cliops"
	"xbot/internal/deviceexec"
	"xbot/internal/exec"
	"xbot/internal/inbox"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

func TestDeviceRuntimePreparedSelectionAndSource(t *testing.T) {
	k, _, _ := freshBot(t)
	if err := k.StartOperations(); err != nil {
		t.Fatal(err)
	}
	defer k.CloseOperations()
	endpoint := k.operations.server.Endpoint
	owner := k.ensureTurn()
	owner.turnDevice = "device-original"
	k.openInterject(owner, &Sink{})
	defer k.closeInterject(owner, true)
	count := 0
	k.DeviceCall = func(ctx context.Context, s deviceexec.Source, in cliops.DeviceRequest) (json.RawMessage, error) {
		count++
		if s.Bot != k.Bot.Name() || s.ContextID != "main" || s.Readonly || s.Account != "" || s.DeviceID != "" {
			t.Errorf("untrusted source %+v", s)
		}
		want := "device-original"
		if count == 2 {
			want = "explicit"
		}
		if in.DeviceID != want {
			t.Errorf("prepared target %q want %q", in.DeviceID, want)
		}
		return json.RawMessage(`{"tasks":[],"exact":9007199254740993}`), nil
	}
	var finishedID string
	runRoutineSource(t, k, nil, `{"command":"device tasks --self"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
		finishedID = tools.CallID(ctx)
		if !Interject(k.Bot, inbox.Event{ID: "ev_device_later", Source: inbox.NameWeb, Sender: "u1", SenderKind: inbox.KindPerson, DeviceID: "device-later", Content: "use the other device", Time: k.now()}, &Sink{}) {
			t.Fatal("interjection not accepted")
		}
		if owner.turnDevice != "device-later" {
			t.Error("new calls did not get the later message selection")
		}
		k.turnMu.Lock()
		k.turn = &turn{turnDevice: "latest-unrelated"}
		k.turnMu.Unlock()
		for _, target := range []string{"", "explicit"} {
			raw, err := cliops.CallDevice(ctx, endpoint, k.Bot.Root, finishedID, cliops.DeviceRequest{Action: "tasks", DeviceID: target})
			if err != nil || !strings.Contains(string(raw), "9007199254740993") {
				t.Errorf("reply %s %v", raw, err)
			}
		}
		return "completed", nil
	})
	for _, id := range []string{"", finishedID, "foreign"} {
		if _, err := cliops.CallDevice(context.Background(), endpoint, k.Bot.Root, id, cliops.DeviceRequest{Action: "tasks"}); err == nil {
			t.Errorf("inactive call %q accepted", id)
		}
	}
	if count != 2 {
		t.Fatalf("unexpected source callback count %d", count)
	}
}

func TestDeviceRuntimeLargeReplyStoresCurrentSourceArtifact(t *testing.T) {
	k, _, _ := freshBot(t)
	if err := k.StartOperations(); err != nil {
		t.Fatal(err)
	}
	defer k.CloseOperations()
	want := strings.Repeat("设备清单", 5000)
	k.DeviceCall = func(context.Context, deviceexec.Source, cliops.DeviceRequest) (json.RawMessage, error) {
		return json.Marshal(map[string]string{"value": want})
	}
	runRoutineSource(t, k, nil, `{"command":"device list --self"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
		raw, err := cliops.CallDevice(ctx, k.operations.server.Endpoint, k.Bot.Root, tools.CallID(ctx), cliops.DeviceRequest{Action: "list"})
		if err != nil {
			t.Fatal(err)
		}
		var out struct {
			File  string `json:"file"`
			Bytes int    `json:"bytes"`
		}
		if json.Unmarshal(raw, &out) != nil || out.File == "" || len(raw) > 1024 {
			t.Fatalf("large result receipt %s", raw)
		}
		workspace := k.currentWorkspaceFor(k.Bot)
		if workspace == "" {
			workspace = work.BotDir(k.Bot.Root, k.Bot.Name())
		}
		data, err := os.ReadFile(filepath.Join(workspace, out.File))
		if err != nil {
			t.Fatal(err)
		}
		var full map[string]string
		if json.Unmarshal(data, &full) != nil || full["value"] != want || len(data) != out.Bytes {
			t.Fatal("stored device reply lost data")
		}
		return "completed", nil
	})
}

func TestDeviceResearchParserRejectsMutations(t *testing.T) {
	k, _, _ := freshBot(t)
	if err := k.StartOperations(); err != nil {
		t.Fatal(err)
	}
	defer k.CloseOperations()
	for _, command := range []string{
		`"$XBOT_BIN" device list --self`, `"$XBOT_BIN" device read --self --args '{"path":"x"}'`,
		`"$XBOT_BIN" device tasks --self`, `"$XBOT_BIN" device task dc_a --self`,
		`"$XBOT_BIN" device artifact dc_a --self --artifact-id f`, `"$XBOT_BIN" device browser --self --tool browser_snapshot --args '{}'`,
		`"$XBOT_BIN" device desktop --help`,
	} {
		raw, _ := json.Marshal(map[string]string{"command": command})
		if !k.allowReadOnlyPlatform("sh", raw) {
			t.Errorf("read/help rejected %s", command)
		}
	}
	for _, command := range []string{
		`"$XBOT_BIN" device exec --self --args '{"command":"ls"}'`, `"$XBOT_BIN" device write --self --args '{}'`,
		`"$XBOT_BIN" device desktop --self --tool desktop_snapshot --args '{}'`, `"$XBOT_BIN" device browser --self --tool browser_click --args '{}'`,
		`"$XBOT_BIN" device read --self --tool write_file --args '{}'`, `"$XBOT_BIN" device stop dc_a --self`,
		`"$XBOT_BIN" device exec --self --args --help`, `"$XBOT_BIN" device list --self; echo injected`,
	} {
		raw, _ := json.Marshal(map[string]string{"command": command})
		if k.allowReadOnlyPlatform("sh", raw) {
			t.Errorf("mutation permitted %s", command)
		}
	}
}

func TestDeviceRuntimeActualResearchChildIdentityAndGate(t *testing.T) {
	p := &scripted{resps: []provider.Message{toolCall("device-child", "sh", `{"command":"device tasks --self","description":"inspect device"}`), {Role: provider.RoleAssistant, Content: provider.TextContent("done")}}}
	k, b, box := dispatchKernel(t, p)
	if err := k.StartOperations(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := k.Close(); err != nil {
			t.Error(err)
		}
	})
	endpoint := k.operations.server.Endpoint
	var childID string
	calls := 0
	k.DeviceCall = func(_ context.Context, s deviceexec.Source, in cliops.DeviceRequest) (json.RawMessage, error) {
		calls++
		childID = s.ContextID
		if !s.Readonly || s.Bot != b.Name() || !strings.HasPrefix(s.ContextID, "sa_") || in.DeviceID != "child-target" {
			t.Errorf("actual child %+v request %+v", s, in)
		}
		return json.RawMessage(`{"tasks":[]}`), nil
	}
	_ = k.ensureExec()
	local := k.execSnapshot().(*exec.Local)
	original := local.ToolsFor
	local.ToolsFor = func(req exec.StartRequest) []tools.Tool {
		list := original(req)
		for i, tool := range list {
			if tool.Name() == "sh" {
				list[i] = routineSourceShell{run: func(ctx context.Context, _ json.RawMessage) (string, error) {
					id := tools.CallID(ctx)
					if _, err := cliops.CallDevice(ctx, endpoint, b.Root, id, cliops.DeviceRequest{Action: "tasks"}); err != nil {
						return "", err
					}
					for _, action := range []string{"exec", "write", "desktop", "browser", "stop"} {
						in := cliops.DeviceRequest{Action: action, Ref: "dc_deny", Call: deviceexec.Call{Ref: "dc_deny", Kind: action, Tool: "sh", Args: json.RawMessage(`{}`)}}
						if _, err := cliops.CallDevice(ctx, endpoint, b.Root, id, in); err == nil {
							t.Errorf("runtime child mutation %s accepted", action)
						}
					}
					return "completed", nil
				}}
			}
		}
		return list
	}
	parent := &turn{turnDevice: "child-target"}
	task, err := k.StartSubagent(context.WithValue(context.Background(), turnKey{}, parent), SubagentRequest{Type: "research", Task: "inspect device tasks"})
	if err != nil {
		t.Fatal(err)
	}
	done := waitEvent(t, box, exec.EvDone)
	if done.Sender != task.ID || childID != task.ID || calls != 1 {
		t.Fatalf("child task=%+v source=%s callbacks=%d event=%+v", task, childID, calls, done)
	}
}
