package kernel

import (
	"strings"
	"testing"
)

// Neutralizing another bot's transcript can make it longer. The cap is met by
// shrinking the body, not by cutting the closer off the wrapped text.
func TestForeignHistoryKeepsCloserAtCap(t *testing.T) {
	body := strings.Repeat("<untrusted_event x ", 200)
	for _, limit := range []int{300, 1000, 2000} {
		out := (&Kernel{}).presentHistory(body, "bob", true, limit)
		if len(out) > limit {
			t.Fatalf("cap %d: %d bytes", limit, len(out))
		}
		if !strings.HasSuffix(out, "\n</untrusted_event>") || strings.Count(out, "<untrusted_event") != 1 {
			t.Fatalf("cap %d: fence broken: %q", limit, out[max(0, len(out)-80):])
		}
	}
}
