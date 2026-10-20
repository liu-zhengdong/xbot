// Package clock is the process clock.
//
// Normally Now is time.Now. When XBOT_FAKE_CLOCK_FILE names a readable file,
// Now adds the offset in seconds it holds to the real time. Tests write that
// file to move serve's timers, traces and quiet-hours checks forward without
// touching the machine clock. The file is read on every call, so a test can
// change the offset while the process runs.
package clock

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// FakeEnv is the variable that names the offset file.
const FakeEnv = "XBOT_FAKE_CLOCK_FILE"

// Now is the current time, shifted by the fake clock when one is configured.
// A configured but unreadable or malformed file is a test setup error, so it
// panics instead of quietly falling back to real time, which would make a
// broken test look like a passing one.
func Now() time.Time {
	path := strings.TrimSpace(os.Getenv(FakeEnv))
	if path == "" {
		return time.Now()
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		panic("clock: read " + path + ": " + err.Error())
	}
	secs, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
	if err != nil {
		panic("clock: bad offset in " + path + ": " + err.Error())
	}
	return time.Now().Add(time.Duration(secs) * time.Second)
}
