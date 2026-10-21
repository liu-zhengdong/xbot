package bot

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"xbot/internal/review"
)

// States written onto a persisted approval card.
const (
	DefaultCardWait      = 30 * time.Minute
	ApprovalAutoApproved = "auto_approved"
	ApprovalPending      = "pending"
	ApprovalApproved     = "approved"
	ApprovalDeclined     = "declined"
	ApprovalExpired      = "expired"
	// ApprovalNoDecision is a card nobody answered: the wait timed out, the
	// serving process restarted, or the wake was abandoned. It is deliberately
	// not ApprovalDeclined: the owner never said no.
	ApprovalNoDecision = "no_decision"
	// ApprovalReasonExpired and ApprovalReasonRestart name why a card ended
	// with no decision.
	ApprovalReasonExpired = "expired"
	ApprovalReasonRestart = "restart"
)

// NoDecisionText is the transcript state recorded for a card nobody answered.
func NoDecisionText(reason string) string {
	return ApprovalNoDecision + ":" + reason
}

// NoDecisionReason returns the reason of a no-decision state. ok is false for
// any other state.
func NoDecisionReason(state string) (string, bool) {
	reason, ok := strings.CutPrefix(state, ApprovalNoDecision+":")
	return reason, ok
}

// ApprovalCard is one owner-confirmation card as persisted in the transcript.
// The same ID is rewritten by appending a newer card; readers keep the latest.
type ApprovalCard struct {
	ID       string
	Tool     string
	Title    string
	Subtitle string
	Summary  string
	State    string
	// Deadline is the RFC3339 instant the card stops accepting a decision.
	// Empty on cards written before the field existed.
	Deadline string
	Notice   string
}

// CardText labels one approval card in Chinese. It mirrors review.CardText for
// callers that already depend on this package.
func CardText(tool string, args json.RawMessage, summary, machine string) (title, subtitle string) {
	return review.CardText(tool, args, summary, machine)
}

// ApprovalCardText wraps one confirmation card as a transcript line. title and
// subtitle are the human labels; pass empty strings for a card without them.
func ApprovalCardText(id, tool, title, subtitle, summary, state string) string {
	return approvalCardText(id, tool, title, subtitle, summary, state, "", "")
}

// ApprovalCardTextAt is ApprovalCardText with the server's decision deadline.
// deadline is RFC3339. An empty deadline omits the attribute, so cards written
// before it existed stay valid.
func ApprovalCardTextAt(id, tool, title, subtitle, summary, state, deadline string) string {
	return approvalCardText(id, tool, title, subtitle, summary, state, deadline, "")
}

func CardWaitText(wait time.Duration) string {
	if wait%time.Minute == 0 {
		return fmt.Sprintf("%d 分钟", wait/time.Minute)
	}
	if wait%time.Second == 0 {
		return fmt.Sprintf("%d 秒", wait/time.Second)
	}
	return fmt.Sprintf("%d 毫秒", wait/time.Millisecond)
}

func ApprovalCardTextWithNotice(id, tool, title, subtitle, summary, state, notice string) string {
	return approvalCardText(id, tool, title, subtitle, summary, state, "", notice)
}

func approvalCardText(id, tool, title, subtitle, summary, state, deadline, notice string) string {
	summary = strings.ReplaceAll(summary, "</approval_card>", "< /approval_card>")
	open := "<approval_card id=\"" + escAttr(id) + "\" tool=\"" + escAttr(tool) +
		"\" title=\"" + escAttr(title) + "\" subtitle=\"" + escAttr(subtitle) +
		"\" state=\"" + escAttr(state) + "\""
	if deadline != "" {
		open += " deadline=\"" + escAttr(deadline) + "\""
	}
	if notice != "" {
		open += " notice=\"" + escAttr(notice) + "\""
	}
	return open + ">\n" + summary + "\n</approval_card>"
}

// IsApprovalCard reports an approval-card transcript line. Those lines stay on
// screen and out of the model context.
func IsApprovalCard(content string) bool {
	return strings.HasPrefix(strings.TrimSpace(content), "<approval_card ")
}

// ParseApprovalCard decodes a persisted card line. ok is false for any other
// text. Cards written before the title and subtitle attributes existed still
// parse, with those fields empty.
func ParseApprovalCard(content string) (ApprovalCard, bool) {
	s := strings.TrimSpace(content)
	const open = "<approval_card "
	const close = "\n</approval_card>"
	if !strings.HasPrefix(s, open) || !strings.HasSuffix(s, close) {
		return ApprovalCard{}, false
	}
	attrs, body, ok := parseCardTag(s[len(open):])
	if !ok {
		return ApprovalCard{}, false
	}
	for _, key := range []string{"id", "tool", "state"} {
		if _, present := attrs[key]; !present {
			return ApprovalCard{}, false
		}
	}
	body = strings.TrimPrefix(body, "\n")
	body = strings.TrimSuffix(body, close)
	return ApprovalCard{
		ID:       attrs["id"],
		Tool:     attrs["tool"],
		Title:    attrs["title"],
		Subtitle: attrs["subtitle"],
		Summary:  body,
		State:    attrs["state"],
		Deadline: attrs["deadline"],
		Notice:   attrs["notice"],
	}, true
}

// parseCardTag reads the attribute list and body of an opening
// <approval_card ...> tag. Attributes are key="value" pairs and may appear in
// any order; a > inside a quoted value does not end the tag. The returned body
// starts right after the closing > and still carries the </approval_card> tail.
func parseCardTag(s string) (map[string]string, string, bool) {
	attrs := map[string]string{}
	i := 0
	for {
		for i < len(s) && isCardSpace(s[i]) {
			i++
		}
		if i >= len(s) {
			return nil, "", false
		}
		if s[i] == '>' {
			return attrs, s[i+1:], true
		}
		j := i
		for j < len(s) && s[j] != '=' {
			j++
		}
		if j >= len(s) {
			return nil, "", false
		}
		key := strings.TrimSpace(s[i:j])
		if key == "" || j+1 >= len(s) || s[j+1] != '"' {
			return nil, "", false
		}
		k := j + 2
		for k < len(s) && s[k] != '"' {
			k++
		}
		if k >= len(s) {
			return nil, "", false
		}
		attrs[key] = unescAttr(s[j+2 : k])
		i = k + 1
	}
}

func isCardSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
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
