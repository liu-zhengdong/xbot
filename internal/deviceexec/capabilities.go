package deviceexec

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"runtime"
	"time"

	"xbot/internal/browser"
	"xbot/internal/desktop"
	"xbot/internal/tools"
)

// Capabilities probes installed interpreters and browser dependencies. Native
// desktop capability additionally requires a successful permissions status read.
func (m *Manager) Capabilities() []string {
	caps := []string{"read", "write"}
	shells := tools.NewShells()
	if len(shells) > 0 {
		caps = append(caps, "exec")
	}
	for _, sh := range shells {
		caps = append(caps, "exec:"+sh.Name())
	}
	if m.Browser != nil && browser.Available() {
		caps = append(caps, "browser")
	}
	if m.Desktop == nil {
		return caps
	}
	if m.Desktop.Driver != nil {
		return append(caps, "desktop")
	}
	if runtime.GOOS == "linux" {
		_, pointerErr := exec.LookPath("xdotool")
		_, shotErr := exec.LookPath("import")
		if pointerErr == nil && shotErr == nil && os.Getenv("DISPLAY") != "" {
			caps = append(caps, "desktop")
		}
		return caps
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	text, err := desktop.Status(ctx)
	if err != nil {
		return caps
	}
	var status struct {
		OK   bool `json:"ok"`
		Data struct {
			Permissions struct {
				Accessibility struct {
					State string `json:"state"`
				} `json:"accessibility"`
				Automation struct {
					State string `json:"state"`
				} `json:"automation"`
			} `json:"permissions"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(text), &status) != nil || !status.OK {
		return caps
	}
	p := status.Data.Permissions
	if runtime.GOOS == "darwin" && p.Accessibility.State == "granted" || runtime.GOOS == "windows" && (p.Automation.State == "granted" || p.Automation.State == "not_required") {
		caps = append(caps, "desktop")
	}
	return caps
}
