//go:build smoke

package smoke

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Scripted external desktop, only for this acceptance test. The entry points,
// binary, original organ driver selection and status HTTP route remain real.
func init() {
	if os.Getenv("XBOT_DESKTOP_SMOKE_CHILD") != "1" {
		return
	}
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "--headed" {
		args = args[1:]
	}
	if len(args) == 0 {
		os.Exit(2)
	}
	state := os.Getenv("XBOT_DESKTOP_SMOKE_STATE")
	value, _ := os.ReadFile(state)
	if string(value) == "denied" && args[0] != "status" {
		fmt.Print(`{"ok":false,"error":{"code":"PERM_DENIED","disposition":{"delivery":"not_delivered","retry":"safe"}}}`)
		os.Exit(1)
	}
	switch args[0] {
	case "snapshot":
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"ok": true, "data": map[string]any{"ref": "@binary:e1", "value": string(value)}})
	case "type":
		_ = os.WriteFile(state, []byte(args[len(args)-1]), 0600)
		fmt.Print(`{"ok":true,"data":{"disposition":{"delivery":"delivered_unverified","retry":"unsafe"}}}`)
	case "status":
		fmt.Print(`{"ok":true,"data":{"platform":"macos","permissions":{"accessibility":{"state":"denied"}}}}`)
	default:
		fmt.Print(`{"ok":true,"data":{"disposition":{"delivery":"delivered_unverified","retry":"unsafe"}}}`)
	}
	os.Exit(0)
}

func TestDesktopAccessibilityBinary(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		t.Skip("Linux retains pixel driver")
	}
	helper, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(t.TempDir(), "desktop-state")
	t.Setenv("XBOT_AGENT_DESKTOP", helper)
	t.Setenv("XBOT_DESKTOP_SMOKE_CHILD", "1")
	t.Setenv("XBOT_DESKTOP_SMOKE_STATE", state)
	srv := Start(t, StartStub(t))
	cli := func(wantOK bool, args ...string) string {
		t.Helper()
		all := append([]string{"--root", srv.Root, "desktop"}, args...)
		cmd := exec.Command(xbotBinary, all...)
		out, err := cmd.CombinedOutput()
		if (err == nil) != wantOK {
			t.Fatalf("%v: %s %v", args, out, err)
		}
		return string(out)
	}
	before := cli(true, "snapshot", "Any Visible App")
	if !strings.Contains(before, "@binary:e1") {
		t.Fatal(before)
	}
	cli(true, "click", "@binary:e1")
	cli(true, "type", "@binary:e1", "changed through real CLI")
	after := cli(true, "snapshot", "Any Visible App")
	if before == after || !strings.Contains(after, "changed through real CLI") {
		t.Fatal(after)
	}
	if err := os.WriteFile(state, []byte("denied"), 0600); err != nil {
		t.Fatal(err)
	}
	denied := cli(false, "click", "@binary:e1")
	if !strings.Contains(denied, "PERM_DENIED") || !strings.Contains(denied, "not_delivered") {
		t.Fatal(denied)
	}
	res, err := srv.client.Get(srv.URL + "/api/devices/desktop")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var status struct {
		Status struct {
			Data struct {
				Permissions struct{ Accessibility struct{ State string } }
			}
		}
	}
	if err := json.NewDecoder(res.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 || status.Status.Data.Permissions.Accessibility.State != "denied" {
		t.Fatalf("status %+v", status)
	}
}
