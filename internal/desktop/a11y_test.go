package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"xbot/internal/machine"
	"xbot/internal/permissions"
)

// A real subprocess with the upstream wire shape. No model or desktop access.
func init() {
	if os.Getenv("XBOT_DESKTOP_TEST_HELPER") != "1" {
		return
	}
	args := os.Args[1:]
	if f, err := os.OpenFile(os.Getenv("XBOT_DESKTOP_TEST_LOG"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600); err == nil {
		_ = json.NewEncoder(f).Encode(args)
		_ = f.Close()
	}
	switch os.Getenv("XBOT_DESKTOP_TEST_RESULT") {
	case "denied":
		fmt.Print(`{"ok":false,"error":{"code":"PERM_DENIED","disposition":{"delivery":"not_delivered","retry":"safe"}}}`)
		os.Exit(1)
	case "uncertain":
		fmt.Print(`{"ok":false,"error":{"code":"ACTION_FAILED","disposition":{"delivery":"delivered_unverified","retry":"unsafe"},"details":{"post_state":"changed"}}}`)
		os.Exit(1)
	case "invalid":
		fmt.Print("not JSON")
		os.Exit(0)
	case "large":
		fmt.Print(strings.Repeat("x", maxCLIOutput+1))
		os.Exit(0)
	}
	if len(args) > 0 && args[0] == "snapshot" {
		value, _ := os.ReadFile(os.Getenv("XBOT_DESKTOP_TEST_STATE"))
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"ok": true, "data": map[string]any{"ref": "@snapshot-test:e1", "value": string(value)}})
	} else {
		if len(args) > 0 && (args[0] == "type" || args[0] == "set-value" || args[0] == "--headed") {
			_ = os.WriteFile(os.Getenv("XBOT_DESKTOP_TEST_STATE"), []byte(args[len(args)-1]), 0600)
		}
		fmt.Print(`{"ok":true,"data":{"disposition":{"delivery":"delivered_unverified","retry":"unsafe"}}}`)
	}
	os.Exit(0)
}

func cliFixture(t *testing.T) string {
	t.Helper()
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(t.TempDir(), "calls")
	t.Setenv("XBOT_AGENT_DESKTOP", bin)
	t.Setenv("XBOT_DESKTOP_TEST_HELPER", "1")
	t.Setenv("XBOT_DESKTOP_TEST_LOG", log)
	t.Setenv("XBOT_DESKTOP_TEST_STATE", filepath.Join(t.TempDir(), "state"))
	return log
}

func TestAccessibilityToolsObserveActVerify(t *testing.T) {
	log := cliFixture(t)
	l := Open(t.TempDir())
	l.Driver = nativeDriver{}
	call := func(name, args string) string {
		t.Helper()
		for _, tool := range l.Tools() {
			if tool.Name() == name {
				out, err := tool.Run(context.Background(), json.RawMessage(args))
				if err != nil {
					t.Fatal(err)
				}
				return out
			}
		}
		t.Fatal("missing tool", name)
		return ""
	}
	before := call("desktop_snapshot", `{"app":"Some Visible App"}`)
	if !strings.Contains(before, "@snapshot-test:e1") {
		t.Fatal(before)
	}
	call("desktop_click", `{"ref":"@snapshot-test:e1"}`)
	text := "--literal 'text' 中文"
	args, _ := json.Marshal(map[string]string{"ref": "@snapshot-test:e1", "text": text})
	call("desktop_type", string(args))
	after := call("desktop_snapshot", `{"app":"Some Visible App","ref":"@snapshot-test:e1","surface":"window"}`)
	if before == after || !strings.Contains(after, text) {
		t.Fatal(after)
	}
	call("desktop_snapshot", `{"app":"Some Visible App","skeleton":true}`)
	call("desktop_set_value", `{"ref":"@snapshot-test:e1","text":""}`)
	call("desktop_scroll", `{"ref":"@snapshot-test:e1","dy":-2}`)
	call("desktop_key", `{"app":"Some Visible App","key":"ctrl+l"}`)
	raw, _ := os.ReadFile(log)
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	var shallow []string
	_ = json.Unmarshal([]byte(lines[4]), &shallow)
	if !reflect.DeepEqual(shallow, []string{"snapshot", "--app", "Some Visible App", "--compact", "--skeleton"}) {
		t.Fatal(shallow)
	}
	var first, click, typed []string
	_ = json.Unmarshal([]byte(lines[0]), &first)
	_ = json.Unmarshal([]byte(lines[1]), &click)
	_ = json.Unmarshal([]byte(lines[2]), &typed)
	if !reflect.DeepEqual(first, []string{"snapshot", "--app", "Some Visible App", "--compact"}) || !reflect.DeepEqual(click, []string{"click", "@snapshot-test:e1"}) || typed[len(typed)-1] != text {
		t.Fatalf("argv: %s", raw)
	}
}

func TestAccessibilityRefusalAndPolicy(t *testing.T) {
	log := cliFixture(t)
	ctx := context.Background()
	root := t.TempDir()
	d := nativeDriver{}
	for _, result := range []string{"denied", "uncertain", "invalid", "large"} {
		t.Run(result, func(t *testing.T) {
			t.Setenv("XBOT_DESKTOP_TEST_RESULT", result)
			out, err := Perform(ctx, root, d, Action{Name: "click", Ref: "@snapshot-test:e1"}, true)
			if err == nil || out != "" {
				t.Fatalf("false success %s %v", out, err)
			}
			if result == "denied" || result == "uncertain" {
				var wire *CLIError
				if !errors.As(err, &wire) || !strings.Contains(wire.Error(), "disposition") {
					t.Fatal(err)
				}
			}
		})
	}
	raw, _ := os.ReadFile(log)
	if len(strings.Split(strings.TrimSpace(string(raw)), "\n")) != 4 {
		t.Fatal("action was retried", string(raw))
	}
	if _, err := Perform(ctx, root, d, Action{Name: "click", X: 1, Y: 2}, true); err == nil {
		t.Fatal("coordinates accepted")
	}
	if err := permissions.Set(root, true); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{machine.Off, machine.Ask} {
		if err := machine.Set(root, machine.Local, mode); err != nil {
			t.Fatal(err)
		}
		if _, err := Perform(ctx, root, d, Action{Name: "snapshot", App: "anything"}, false); err == nil || !strings.Contains(err.Error(), mode) {
			t.Fatal(mode, err)
		}
	}
	after, _ := os.ReadFile(log)
	if string(raw) != string(after) {
		t.Fatal("refused action reached CLI")
	}
	if err := permissions.Set(root, false); err != nil {
		t.Fatal(err)
	}
	if _, err := Perform(ctx, root, d, Action{Name: "snapshot", App: "anything"}, false); err != nil {
		t.Fatal("default permissions-off must allow", err)
	}
}

func TestAccessibilityMissingCLI(t *testing.T) {
	t.Setenv("XBOT_AGENT_DESKTOP", filepath.Join(t.TempDir(), "missing"))
	if _, err := Status(context.Background()); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatal(err)
	}
}
