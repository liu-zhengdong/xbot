package cliops

import (
	"context"
	"time"
)

const ChatGroupMaxRequest = 1 << 20
const ChatGroupMaxReply = 64 << 10

// ChatGroupHandlers bind the existing source runtime, without loading a target Bot.
type ChatGroupHandlers struct {
	List    func(context.Context) (ChatGroupListReply, error)
	History func(context.Context, ChatGroupHistory) (ChatGroupHistoryReply, error)
	Create  func(context.Context, ChatGroupCreate) (ChatGroupCreateReply, error)
	Post    func(context.Context, ChatGroupPost) (ChatGroupPostReply, error)
}

type ChatGroup struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Members []string  `json:"members"`
	Created time.Time `json:"created"`
}

type ChatGroupLine struct {
	ID     string    `json:"id"`
	Time   time.Time `json:"time"`
	Sender string    `json:"sender"`
	Kind   string    `json:"kind"`
	Text   string    `json:"text"`
}

type ChatGroupHistory struct {
	ID       string `json:"id"`
	Limit    int    `json:"limit"`
	BeforeID string `json:"before_id"`
	Sender   string `json:"sender"`
	Keyword  string `json:"keyword"`
}

type ChatGroupCreate struct {
	Name    string   `json:"name"`
	Members []string `json:"members"`
}

type ChatGroupPost struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

type ChatGroupListReply struct {
	Groups *[]ChatGroup `json:"groups,omitempty"`
	Count  int          `json:"count"`
	File   string       `json:"file,omitempty"`
	Access string       `json:"access,omitempty"`
	Bytes  int64        `json:"bytes,omitempty"`
	Error  string       `json:"error,omitempty"`
}

type ChatGroupHistoryReply struct {
	Group     *ChatGroup       `json:"group,omitempty"`
	Messages  *[]ChatGroupLine `json:"messages,omitempty"`
	Untrusted bool             `json:"untrusted"`
	Count     int              `json:"count"`
	File      string           `json:"file,omitempty"`
	Access    string           `json:"access,omitempty"`
	Bytes     int64            `json:"bytes,omitempty"`
	Error     string           `json:"error,omitempty"`
}

type ChatGroupCreateReply struct {
	Group   *ChatGroup `json:"group,omitempty"`
	Created bool       `json:"created"`
	Error   string     `json:"error,omitempty"`
}

type ChatGroupPostReply struct {
	ID            string   `json:"id"`
	MessageID     string   `json:"message_id,omitempty"`
	Appended      bool     `json:"appended"`
	PostedMembers []string `json:"posted_members"`
	FailedMember  string   `json:"failed_member,omitempty"`
	Stage         string   `json:"stage"`
	Error         string   `json:"error,omitempty"`
}
