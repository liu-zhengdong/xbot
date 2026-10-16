package bot

import (
	"regexp"
	"strings"
)

// States written onto a persisted approval card.
const (
	ApprovalPending  = "pending"
	ApprovalApproved = "approved"
	ApprovalDeclined = "declined"
	ApprovalExpired  = "expired"
)

// ApprovalCard is one owner-confirmation card as persisted in the transcript.
// The same ID is rewritten by appending a newer card; readers keep the latest.
type ApprovalCard struct {
	ID      string
	Tool    string
	Summary string
	State   string
}

var approvalCardRe = regexp.MustCompile(`(?s)^<approval_card id="([^"]*)" tool="([^"]*)" state="([^"]*)">\n(.*)\n</approval_card>$`)

// ApprovalCardText wraps one confirmation card as a transcript line.
func ApprovalCardText(id, tool, summary, state string) string {
	summary = strings.ReplaceAll(summary, "</approval_card>", "< /approval_card>")
	return "<approval_card id=\"" + escAttr(id) + "\" tool=\"" + escAttr(tool) + "\" state=\"" + escAttr(state) + "\">\n" +
		summary + "\n</approval_card>"
}

// IsApprovalCard reports an approval-card transcript line. Those lines stay on
// screen and out of the model context.
func IsApprovalCard(content string) bool {
	return strings.HasPrefix(strings.TrimSpace(content), "<approval_card ")
}

// ParseApprovalCard decodes a persisted card line. ok is false for any other text.
func ParseApprovalCard(content string) (ApprovalCard, bool) {
	m := approvalCardRe.FindStringSubmatch(strings.TrimSpace(content))
	if m == nil {
		return ApprovalCard{}, false
	}
	return ApprovalCard{ID: unescAttr(m[1]), Tool: unescAttr(m[2]), Summary: m[4], State: unescAttr(m[3])}, true
}

func escAttr(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	return s
}

func unescAttr(s string) string {
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&quot;", `"`)
	s = strings.ReplaceAll(s, "&amp;", "&")
	return s
}
