package person

import (
	"time"
	"unicode/utf8"
)

func capStamp(t, now time.Time) time.Time {
	if t.IsZero() {
		return now.UTC()
	}
	t = t.UTC()
	limit := now.UTC().Add(maxSkew)
	if t.After(limit) {
		return limit
	}
	return t
}

func hasControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

func fieldOK(s string, max int) bool {
	if s == "" || len(s) > max || hasControl(s) || !utf8.ValidString(s) {
		return false
	}
	return true
}

// acceptRemote caps the clock and strips ownership. A line that claims this
// machine's device is stored as relay so it cannot impersonate a local write.
func acceptRemote(e LogEntry, now time.Time, home string) (LogEntry, bool) {
	e.Time = capStamp(e.Time, now)
	e.Grant = false
	if e.Device == "" || e.Device == home || e.Device == "local" {
		e.Device = "relay"
	}
	if e.Bot != "" && !fieldOK(e.Bot, 80) {
		return LogEntry{}, false
	}
	if !fieldOK(e.Device, 128) {
		return LogEntry{}, false
	}
	if e.Person.DisplayName != "" && !fieldOK(e.Person.DisplayName, 200) {
		return LogEntry{}, false
	}
	if e.Person.Timezone != "" && !fieldOK(e.Person.Timezone, 64) {
		return LogEntry{}, false
	}
	for _, a := range e.Person.Accounts {
		if !fieldOK(a.Link, 64) || !fieldOK(a.ExternalID, 128) {
			return LogEntry{}, false
		}
	}
	return e, true
}
