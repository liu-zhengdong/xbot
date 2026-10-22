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
func (r *turnRun) validateReply(_ provider.Request, msg provider.Message) error {
	if len(msg.ToolCalls) > 0 || strings.TrimSpace(msg.Content) != "SILENT" {
		return nil
	}
	required := false
	err := r.k.Bot.Walk(r.t.fromSeq, 0, func(_ int, e bot.Entry) error {
		if e.Branch != "" || e.IsInternal() || e.EventID == "" || e.Message.Role != provider.RoleUser {
			return nil
		}
		text := strings.TrimSpace(e.Message.Content)
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
