package memory

import (
	"strings"
	"time"
	"unicode/utf8"
)

// maxSkew is how far a merged timestamp may sit ahead of this machine's clock.
const maxSkew = 2 * time.Minute

// Provenance marks a fact as data recorded by someone else.
// The suffix is applied again only when it is not already the end of the text.
// The word "untrusted" earlier in the fact does not count.
func Provenance(text, by, device string) string {
	by = label(by)
	if by == "" {
		by = "unknown"
	}
	device = label(device)
	suffix := " (untrusted, recorded by " + by + ")"
	if device != "" && device != "local" {
		suffix = " (untrusted, recorded by " + by + " on " + device + ")"
	}
	if strings.HasSuffix(text, suffix) {
		return text
	}
	return text + suffix
}

func label(s string) string {
	s = oneLine(s)
	if s == "" || hasControl(s) {
		return ""
	}
	if len(s) > 80 {
		s = s[:80]
		for !utf8.ValidString(s) && len(s) > 0 {
			s = s[:len(s)-1]
		}
	}
	return s
}

func hasControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

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

// acceptRemote reports whether a merged line is safe to store.
// A future timestamp is capped. An illegal field is refused.
// A device that claims this machine is stored as relay.
func acceptRemote(ln SyncLine, now time.Time, home string) (SyncLine, bool) {
	ln.Time = capStamp(ln.Time, now)
	ln.Fact.Time = ln.Time
	ln.Fact.PersonID = strings.TrimSpace(ln.Fact.PersonID)
	ln.Fact.Scope = ScopePerson
	if ln.Op == "" {
		ln.Op = opRemember
	}
	if ln.Op != opRemember && ln.Op != opForget {
		return SyncLine{}, false
	}
	if ln.Fact.Tier != TierProfile && ln.Fact.Tier != TierLog {
		return SyncLine{}, false
	}
	if !fieldOK(ln.Fact.Key, 80) || !fieldOK(ln.Fact.Text, maxFactBytes) {
		return SyncLine{}, false
	}
	if ln.Bot != "" && !fieldOK(ln.Bot, 80) {
		return SyncLine{}, false
	}
	if ln.Device != "" && !fieldOK(ln.Device, 128) {
		return SyncLine{}, false
	}
	if ln.Fact.WrittenBy != "" && !fieldOK(ln.Fact.WrittenBy, 80) {
		return SyncLine{}, false
	}
	if ln.Device == "" || ln.Device == home || ln.Device == "local" {
		ln.Device = "relay"
	}
	ln.Untrusted = true
	return ln, true
}

func fieldOK(s string, max int) bool {
	if s == "" || len(s) > max || hasControl(s) || !utf8.ValidString(s) {
		return false
	}
	return true
}
