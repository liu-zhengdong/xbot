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
	// Remember's size check runs after this call, on the text plus suffix.
	// Clip the text so text+suffix fits in maxFactBytes.
	return clipTo(text, maxFactBytes-len(suffix)) + suffix
}

// OwnerProvenance marks a fact the owner wrote on another linked device.
// The suffix is applied again only when it is not already the end of the text.
func OwnerProvenance(text, device string) string {
	device = label(device)
	suffix := " (owner-recorded)"
	if device != "" && device != "local" && device != "relay" {
		suffix = " (owner-recorded on " + device + ")"
	}
	if strings.HasSuffix(text, suffix) {
		return text
	}
	return clipTo(text, maxFactBytes-len(suffix)) + suffix
}

func clipTo(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for !utf8.ValidString(s) && len(s) > 0 {
		s = s[:len(s)-1]
	}
	return s
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
// peer is the authenticated device that delivered this bundle.
// Owner-recorded requires that peer to be the claimed device and locally linked.
// A relayed line keeps its device name and stays untrusted.
func acceptRemote(ln SyncLine, now time.Time, home, peer string, linked map[string]bool) (SyncLine, bool) {
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
	ln.OwnerRecorded = false
	ln.Fact.OwnerRecorded = false
	ln.Untrusted = true
	if peer != "" && ln.Device == peer && linkedDevice(peer, linked) {
		ln.Untrusted = false
		ln.OwnerRecorded = true
	}
	return ln, true
}

func linkedDevice(device string, linked map[string]bool) bool {
	if device == "" || device == "local" || device == "relay" || linked == nil {
		return false
	}
	return linked[device]
}

func fieldOK(s string, max int) bool {
	if s == "" || len(s) > max || hasControl(s) || !utf8.ValidString(s) {
		return false
	}
	return true
}
