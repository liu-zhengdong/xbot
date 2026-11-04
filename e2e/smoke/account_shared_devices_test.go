//go:build smoke

package smoke

import (
	"bytes"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// UC-DEVICE-01/03: owner selection reaches the original source model's real
// runtime CLI. Account membership suffices for both existing and later Bots.
// UC-DEVICE-04: a revoked member cannot receive another execution.
// The separate stop test covers actual target Jobs cancellation. Minute-scale
// heartbeat resurrection and browser controls belong to separate acceptance.
func TestAccountSharedDevicesBinary(t *testing.T) {
	f := newAccountSharedFixture(t)
	for _, scenario := range []struct{ bot, cue, receiptCue, marker, content string }{
		{"DeviceAlpha", "ACCT_COPPER_SIGNAL", "ACCT_AMBER_OAK", "acct-copper-marker.txt", "copper target content\n"},
		{"DeviceBeta", "ACCT_IVORY_BEACON", "ACCT_RUBY_MAPLE", "acct-ivory-marker.txt", "ivory target content\n"},
	} {
		f.run(t, scenario.bot, scenario.cue, "printf '%s' "+shellQuote(scenario.content)+" > "+shellQuote(scenario.marker))
		task := f.waitTask(t, scenario.bot, "completed")
		f.marker(t, scenario.marker, scenario.content)
		f.terminalReceipt(t, scenario.bot, scenario.receiptCue, task)
		var detail struct {
			Task       accountSharedTask `json:"task"`
			Output     string            `json:"output"`
			NextOffset int64             `json:"next_offset"`
		}
		path := "/api/account/devices/" + url.PathEscape(f.Device.ID) + "/tasks/" + url.PathEscape(task.Ref) + "?offset=0&limit=128"
		accountSharedJSON(t, f.Source, http.MethodGet, path, nil, &detail)
		if detail.Task.Ref != task.Ref || detail.Task.DeviceID != f.Device.ID || detail.Task.Status != "completed" || len(detail.Output) > 128 || detail.NextOffset < 0 {
			t.Fatal("task detail lost real terminal status/device identity or bounded output")
		}
	}
	// Creation happens after two successful operations. No Bot-by-device grant
	// is added for this new source, and the target still has no delegate model.
	f.Source.CreateBot("DeviceGamma", "Use the account device pool from a newly created source Bot.")
	f.run(t, "DeviceGamma", "ACCT_JADE_LANTERN", "printf '%s' 'jade target content' > acct-jade-marker.txt")
	gammaTask := f.waitTask(t, "DeviceGamma", "completed")
	f.terminalReceipt(t, "DeviceGamma", "ACCT_PEARL_WILLOW", gammaTask)
	f.marker(t, "acct-jade-marker.txt", "jade target content")
	f.noDelegate(t)

	var removed struct {
		Unbound bool `json:"unbound"`
	}
	devicePath := "/api/account/devices/" + url.PathEscape(f.Device.ID)
	accountSharedJSON(t, f.Source, http.MethodDelete, devicePath, nil, &removed)
	if !removed.Unbound {
		t.Fatal("member removal did not acknowledge actual unbinding")
	}
	var list struct {
		Devices []accountSharedDevice `json:"devices"`
	}
	accountSharedJSON(t, f.Source, http.MethodGet, "/api/account/devices", nil, &list)
	for _, d := range list.Devices {
		if d.ID == f.Device.ID {
			t.Fatal("unbound member remains in account directory")
		}
	}

	const revokedCue = "ACCT_VIOLET_COMPASS"
	const revokedMarker = "acct-revoked-marker.txt"
	f.Model.add(revokedCue, mcpRuntimeTool("sh", map[string]string{
		"description": "Attempt a selected device command after membership revocation",
		"command":     accountSharedCommand("printf forbidden > " + revokedMarker),
	}))
	status, _ := accountSharedAPI(t, f.Source, http.MethodPost, "/api/bots/DeviceAlpha/messages", map[string]string{"text": revokedCue, "device_id": f.Device.ID})
	if status == http.StatusOK {
		var result string
		WaitFor(t, 8*time.Second, "explicit runtime refusal for unbound device", func() (bool, string) {
			results := f.Model.results(revokedCue)
			if len(results) == 1 {
				result = results[0]
			}
			return result != "", "source has not observed the rejected command"
		})
		if strings.HasPrefix(result, "exit 0\n") || strings.TrimSpace(commandOutput(result)) == "" {
			t.Fatal("unbound-device command did not return an explicit failure")
		}
	} else if status != http.StatusBadRequest && status != http.StatusForbidden && status != http.StatusNotFound && status != http.StatusConflict && status != http.StatusGone {
		t.Fatalf("unbound-device message did not explicitly reject selection: HTTP %d", status)
	}
	if len(accountSharedFiles(t, f.Target.Root, revokedMarker)) != 0 || len(accountSharedFiles(t, f.Source.Root, revokedMarker)) != 0 {
		t.Fatal("unbound selection executed remotely or silently fell back to source")
	}
	// Successful target work survives account membership removal.
	f.marker(t, "acct-copper-marker.txt", "copper target content\n")
	f.noDelegate(t)
	t.Log("Same-account source Bots and a later Bot executed on target; target model calls=0; original receipts/task APIs retained device_id; unbound selection refused. Heartbeat resurrection: not covered here.")
}

// UC-DEVICE-03: stop operates on the live target Jobs instance. A finite shell
// loop writes progress every 100 ms. Cancellation must stop those disk writes.
func TestAccountSharedDevicesStopBinary(t *testing.T) {
	f := newAccountSharedFixture(t)
	const cue = "ACCT_SILVER_ANCHOR"
	const progress = "acct-stop-progress.txt"
	const late = "acct-stop-late.txt"
	command := "printf ready > " + progress + "; i=0; while [ \"$i\" -lt 450 ]; do printf . >> " + progress + "; i=$((i+1)); sleep 0.1; done; printf leaked > " + late
	f.run(t, "DeviceAlpha", cue, command)
	task := f.waitTask(t, "DeviceAlpha", "running")
	var progressPath string
	WaitFor(t, 8*time.Second, "live target process disk progress", func() (bool, string) {
		files := accountSharedFiles(t, f.Target.Root, progress)
		if len(files) != 1 {
			return false, "target process has not written progress"
		}
		progressPath = files[0]
		raw, err := os.ReadFile(progressPath)
		return err == nil && bytes.HasPrefix(raw, []byte("ready.")), "target process has not begun its bounded loop"
	})
	if len(accountSharedFiles(t, f.Source.Root, progress)) != 0 {
		t.Fatal("long task ran on source")
	}
	path := "/api/account/devices/" + url.PathEscape(f.Device.ID) + "/tasks/" + url.PathEscape(task.Ref) + "/stop"
	var stopped struct {
		Task accountSharedTask `json:"task"`
	}
	accountSharedJSON(t, f.Source, http.MethodPost, path, map[string]any{}, &stopped)
	if stopped.Task.Ref != task.Ref || stopped.Task.DeviceID != f.Device.ID {
		t.Fatal("stop receipt lost target task identity")
	}
	terminal := f.waitTask(t, "DeviceAlpha", "cancelled")
	if terminal.Ref != task.Ref {
		t.Fatal("stop cancelled a different task")
	}
	f.terminalReceipt(t, "DeviceAlpha", "ACCT_BRONZE_CEDAR", terminal)
	before, err := os.ReadFile(progressPath)
	if err != nil {
		t.Fatal(err)
	}
	// Four producer intervals make a forged cancelled status observable without
	// waiting for the 45-second command deadline. This wait is bounded.
	timer := time.NewTimer(450 * time.Millisecond)
	defer timer.Stop()
	<-timer.C
	after, err := os.ReadFile(progressPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("target process kept writing after terminal cancellation")
	}
	if len(accountSharedFiles(t, f.Target.Root, late)) != 0 || len(accountSharedFiles(t, f.Source.Root, late)) != 0 {
		t.Fatal("stopped command reached its later side effect")
	}
	var detail struct {
		Task   accountSharedTask `json:"task"`
		Output string            `json:"output"`
	}
	accountSharedJSON(t, f.Source, http.MethodGet, strings.TrimSuffix(path, "/stop")+"?offset=0&limit=128", nil, &detail)
	if detail.Task.Status != "cancelled" || detail.Task.Ref != task.Ref || detail.Task.DeviceID != f.Device.ID {
		t.Fatal("task detail lost actual cancellation")
	}
	f.noDelegate(t)
	t.Log("Real target process wrote progress, stop reached cancelled with finished_at, subsequent writes ceased; target model calls=0. Heartbeat resurrection and concurrent cwd isolation: not covered.")
}
