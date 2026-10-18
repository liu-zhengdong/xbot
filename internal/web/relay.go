package web

import (
	"regexp"
	"strconv"
	"strings"

	"xbot/internal/fsutil"
)

var relaySourceRe = regexp.MustCompile(`^<untrusted_event source=("(?:[^"\\]|\\.)*")`)

// relaySender splits the source of a message another bot sent, "bot:writer"
// or "bot:writer@MacBook", into the bot and the paired device's name. Bot
// names never contain "@", so the first one ends the bot.
func relaySender(content string) (fromBot, fromDevice string) {
	m := relaySourceRe.FindStringSubmatch(content)
	if m == nil {
		return "", ""
	}
	source, err := strconv.Unquote(m[1])
	if err != nil || !strings.HasPrefix(source, "bot:") {
		return "", ""
	}
	name, device, _ := strings.Cut(strings.TrimPrefix(source, "bot:"), "@")
	if !fsutil.ValidName(name) {
		return "", ""
	}
	return name, strings.TrimSpace(device)
}
