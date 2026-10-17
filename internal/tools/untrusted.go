package tools

import (
	"fmt"
	"strings"
	"time"
)

// wrapUntrusted marks external text as data, in the same form as link.Wrap.
// A page that contains the closer cannot end the block early.
func wrapUntrusted(source, typ, sender string, at time.Time, body string) string {
	body = strings.NewReplacer(
		"</untrusted_event>", "< /untrusted_event>",
		"<untrusted_event", "< untrusted_event",
	).Replace(body)
	sender = strings.Map(func(r rune) rune {
		if r < 32 || r == '"' || r == '<' || r == '>' {
			return -1
		}
		return r
	}, sender)
	if at.IsZero() {
		at = time.Now()
	}
	return fmt.Sprintf("<untrusted_event source=%q type=%q sender=%q time=%q>\n%s\n</untrusted_event>",
		source, typ, sender, at.UTC().Format(time.RFC3339), body)
}
