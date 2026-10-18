package remote

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"xbot/internal/bot"
	"xbot/internal/fsutil"
)

// G7e batch B: what the 设备 list and the chat list need about each
// transcript, plus the read cursor behind the unread count.

// ChatSummary is one bot's chat with a paired device, for the device list.
type ChatSummary struct {
	Last     string `json:"last,omitempty"`
	LastRole string `json:"last_role,omitempty"`
	Unread   int    `json:"unread"`
	Queued   string `json:"queued,omitempty"`
	Failed   bool   `json:"failed,omitempty"`
}

// chatClip is the longest summary text, in runes.
const chatClip = 72

// readCursor records the id of the newest transcript line the owner has seen.
type readCursor struct {
	ID string    `json:"id"`
	At time.Time `json:"at"`
}

func readPath(root, device, name string) string {
	return filepath.Join(chatDir(root), device, name+".read")
}

// ChatSummaries returns one summary per transcript on this computer for
// deviceID. Names come from the files, so a device this computer no longer
// knows still counts here; ChatHistory and MarkChatRead are the ones that
// require a paired device.
func ChatSummaries(root, deviceID string) (map[string]ChatSummary, error) {
	out := map[string]ChatSummary{}
	entries, err := os.ReadDir(filepath.Join(chatDir(root), deviceID))
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		name, ok := strings.CutSuffix(entry.Name(), ".jsonl")
		if !ok || entry.IsDir() || !bot.ValidName(name) {
			continue
		}
		lines, err := readChat(root, deviceID, name)
		if err != nil {
			return nil, err
		}
		if len(lines) > 0 {
			out[name] = summarize(lines, readCursorOf(root, deviceID, name))
		}
	}
	return out, nil
}

// summarize reports the newest line, the bot lines after the cursor (all of
// them before the first read; when the cursor's line was trimmed away, those
// newer than its time), the oldest line not delivered yet, and whether any
// line failed.
func summarize(lines []ChatMessage, cur readCursor) ChatSummary {
	var s ChatSummary
	last := lines[len(lines)-1]
	s.Last, s.LastRole = chatText(last.Text), last.Role
	start := -1
	if cur.ID != "" {
		start = len(lines) - 1
		found := false
		for i, m := range lines {
			if m.ID == cur.ID {
				start, found = i, true
			}
		}
		if !found {
			start = -1
			for i, m := range lines {
				if !m.At.After(cur.At) {
					start = i
				}
			}
		}
	}
	for i, m := range lines {
		if m.Role == "bot" && i > start {
			s.Unread++
		}
		if m.Role != "me" {
			continue
		}
		if s.Queued == "" && (m.State == "queued_local" || m.State == "sent") {
			s.Queued = chatText(m.Text)
		}
		s.Failed = s.Failed || m.State == "failed"
	}
	return s
}

// readCursorOf returns the cursor, zero when it is missing or damaged.
func readCursorOf(root, device, name string) readCursor {
	var c readCursor
	if readJSON(readPath(root, device, name), &c) != nil {
		return readCursor{}
	}
	return c
}

// MarkChatRead moves the cursor to the newest line, so the unread count stays
// zero until the bot answers again. Without a transcript it does nothing.

// MarkChatRead moves the read cursor to the line upTo, or to the newest line
// when upTo is empty, so a reply that arrived after the owner's view stays
// unread. No transcript or an unknown upTo leaves the cursor alone.
func MarkChatRead(root, deviceID, botName, upTo string) error {
	if !bot.ValidName(botName) {
		return errors.New("remote: invalid bot name")
	}
	cfg, err := Load(root)
	if err != nil {
		return err
	}
	d, ok := cfg.ByID(deviceID)
	if !ok {
		return fmt.Errorf("remote: device %s not found", deviceID)
	}
	lines, err := readChat(root, d.ID, botName)
	if err != nil || len(lines) == 0 {
		return err
	}
	at := len(lines) - 1
	if upTo != "" {
		at = slices.IndexFunc(lines, func(m ChatMessage) bool { return m.ID == upTo })
		if at < 0 {
			return nil
		}
	}
	raw, err := fsutil.Marshal(readCursor{ID: lines[at].ID, At: lines[at].At})
	if err != nil {
		return err
	}
	return fsutil.WriteAtomic(readPath(root, d.ID, botName), raw, 0600)
}

var (
	mdLink  = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)
	mdLead  = regexp.MustCompile(`(?m)^[ \t]*(#{1,6}[ \t]+|>[ \t]?|[-*+][ \t]+|\d+\.[ \t]+)`)
	mdEm    = regexp.MustCompile(`\*([^*\s][^*\n]*)\*`)
	mdMarks = strings.NewReplacer("**", "", "__", "", "~~", "", "`", "")
)

// chatText is a sidebar preview: Markdown marks removed (headings, quotes, list
// markers, bold, code, [text](url) → text), whitespace collapsed, at most
// chatClip runes.
func chatText(s string) string {
	s = mdLead.ReplaceAllString(mdLink.ReplaceAllString(s, "$1"), "")
	s = mdEm.ReplaceAllString(mdMarks.Replace(s), "$1")
	return clipRunes(strings.Join(strings.Fields(s), " "), chatClip)
}

// clipRunes cuts s to n runes, adding … when anything was cut.
func clipRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

// ChatAckTimeout is how long a line the gateway took may wait for the other
// computer's ack while that computer is online. Past it the line shows
// 「没收到 X 的回执」; while the computer is offline the line is just queued.
const ChatAckTimeout = 3 * time.Minute

// NoAck reports whether an owner line has waited past ChatAckTimeout for its
// ack while the device is online.
func NoAck(m ChatMessage, online bool, now time.Time) bool {
	return m.Role == "me" && m.State == "sent" && online && now.Sub(m.At) > ChatAckTimeout
}
