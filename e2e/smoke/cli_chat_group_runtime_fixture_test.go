//go:build smoke

package smoke

import "time"

// Independent full wire views fixed by concrete7bd3/ADR0055, not product aliases.
type cliChatGroup struct {
	ID, Name string
	Members  []string
	Created  time.Time
}
type cliChatGroupLine struct {
	ID                 string
	Time               time.Time
	Sender, Kind, Text string
}
type cliChatGroupListReply struct {
	Groups              *[]cliChatGroup
	Count               int
	File, Access, Error string
	Bytes               int64
}
type cliChatGroupHistoryReply struct {
	Group               *cliChatGroup
	Messages            *[]cliChatGroupLine
	Untrusted           bool
	Count               int
	File, Access, Error string
	Bytes               int64
}
type cliChatGroupCreateReply struct {
	Group   *cliChatGroup
	Created bool
	Error   string
}
type cliChatGroupPostReply struct {
	ID            string
	MessageID     string `json:"message_id"`
	Appended      bool
	PostedMembers []string `json:"posted_members"`
	FailedMember  string   `json:"failed_member"`
	Stage, Error  string
}
