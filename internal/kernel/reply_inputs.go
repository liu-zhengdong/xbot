package kernel

import (
	"errors"
	"strings"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/provider"
)

// The current wake's new inputs determine whether silence is meaningful. Older,
// already answered owner messages in the history do not make timer wakes chatty.
func (r *turnRun) validateReply(req provider.Request, msg provider.Message) error {
	if len(msg.Calls()) > 0 || strings.TrimSpace(msg.Text()) != "SILENT" || sentToChat(req.Messages) {
		return nil
	}
	required := false
	err := r.k.Bot.Walk(r.t.fromSeq, 0, func(_ int, e bot.Entry) error {
		if e.Branch != "" || e.IsInternal() || e.EventID == "" || e.Message.Role != provider.RoleUser {
			return nil
		}
		text := strings.TrimSpace(e.Message.Text())
		if !strings.HasPrefix(text, link.UntrustedEventPrefix) || strings.Contains(strings.SplitN(text, "\n", 2)[0], `source="subagent"`) || strings.Contains(strings.SplitN(text, "\n", 2)[0], `source="coding-agent"`) {
			required = true
		}
		return nil
	})
	if err != nil {
		return err
	}
	if required {
		return errors.New("reply: owner input or task report requires a normal answer, not SILENT")
	}
	return nil
}

// sentToChat reports a send_message_to_user that reached the owner's chat
// since the latest input. The owner has the answer, and the closing reply
// text is not shown, so SILENT is a fine ending.
func sentToChat(msgs []provider.Message) bool {
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m.Role == provider.RoleTool && m.Name == "send_message_to_user" && strings.HasPrefix(m.Text(), "sent to the owner's chat") {
			return true
		}
		if m.Role == provider.RoleUser && !strings.HasPrefix(m.Text(), "<environment") {
			return false
		}
	}
	return false
}
