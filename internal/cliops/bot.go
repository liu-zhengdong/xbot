package cliops

import (
	"context"
	"time"
)

const BotMaxRequest = 1 << 20
const BotMaxReply = 64 << 10

type BotHandlers struct {
	List    func(context.Context) (BotListReply, error)
	Info    func(context.Context, BotInfoQuery) (BotInfoReply, error)
	Create  func(context.Context, BotCreate) (BotCreateReply, error)
	Delete  func(context.Context, string) (BotDeleteReply, error)
	Pin     func(context.Context, BotPin) (BotPinReply, error)
	Primary func(context.Context, string) (BotPrimaryReply, error)
	Persona func(context.Context, BotPersona) (BotPersonaReply, error)
	Fork    func(context.Context, BotFork) (BotForkReply, error)
}

type BotInfoQuery struct {
	Bot   string `json:"bot,omitempty"`
	Steps int    `json:"steps,omitempty"`
}

type BotCreate struct {
	Name    string `json:"name"`
	Persona string `json:"persona"`
}

type BotPersona struct {
	Text string `json:"text"`
}

type BotPersonaReply struct {
	Name      string `json:"name"`
	Written   bool   `json:"written"`
	Effective string `json:"effective,omitempty"`
	Error     string `json:"error,omitempty"`
}

type BotPin struct {
	Name   string
	Pinned bool
}

type BotPinReply struct {
	Name    string `json:"name"`
	Pinned  bool   `json:"pinned"`
	Applied bool   `json:"applied"`
	Stage   string `json:"stage"`
	Error   string `json:"error,omitempty"`
}

type BotPrimaryReply struct {
	Name    string `json:"name"`
	Primary string `json:"primary,omitempty"`
	Applied bool   `json:"applied"`
	Stage   string `json:"stage"`
	Error   string `json:"error,omitempty"`
}

type BotListLocal struct {
	Name           string `json:"name"`
	PersonaPreview string `json:"persona_preview,omitempty"`
}

type BotListRemote struct {
	Name string    `json:"name"`
	Seen time.Time `json:"seen"`
}

type BotListReply struct {
	Local  *[]BotListLocal  `json:"local,omitempty"`
	Remote *[]BotListRemote `json:"remote,omitempty"`
	Count  int              `json:"count"`
	File   string           `json:"file,omitempty"`
	Access string           `json:"access,omitempty"`
	Bytes  int64            `json:"bytes,omitempty"`
	Error  string           `json:"error,omitempty"`
}

type BotInfoReply struct {
	Bots   *[]BotStatusView `json:"bots,omitempty"`
	Steps  *[]BotStepView   `json:"steps,omitempty"`
	Count  int              `json:"count"`
	File   string           `json:"file,omitempty"`
	Access string           `json:"access,omitempty"`
	Bytes  int64            `json:"bytes,omitempty"`
	Error  string           `json:"error,omitempty"`
}

type BotCreateReply struct {
	Name    string `json:"name"`
	Applied bool   `json:"applied"`
	Stage   string `json:"stage"`
	Error   string `json:"error,omitempty"`
}

type BotDeleteReply struct {
	Name    string `json:"name"`
	Removed bool   `json:"removed"`
	Stage   string `json:"stage"`
	Error   string `json:"error,omitempty"`
}

// BotStatusView projects the public runtime status without a kernel dependency.
type BotStatusView struct {
	Bot             string       `json:"bot"`
	State           string       `json:"state"`
	Tool            string       `json:"tool,omitempty"`
	ToolSummary     string       `json:"tool_summary,omitempty"`
	Since           string       `json:"since,omitempty"`
	Backgrounds     int          `json:"backgrounds,omitempty"`
	Agents          int          `json:"agents,omitempty"`
	CompletedUnread int          `json:"completed_unread,omitempty"`
	LastMessage     string       `json:"last_message,omitempty"`
	Todo            string       `json:"todo,omitempty"`
	Tokens          *BotTokenUse `json:"tokens,omitempty"`
}

type BotTokenUse struct {
	Prompt            int  `json:"prompt,omitempty"`
	Completion        int  `json:"completion,omitempty"`
	Cached            int  `json:"cached,omitempty"`
	TurnCompletion    int  `json:"turn_completion,omitempty"`
	SessionPrompt     int  `json:"session_prompt,omitempty"`
	SessionCompletion int  `json:"session_completion,omitempty"`
	SessionCached     int  `json:"session_cached,omitempty"`
	Window            int  `json:"window,omitempty"`
	Estimated         bool `json:"estimated,omitempty"`
}

type BotStepView struct {
	Time    time.Time `json:"time"`
	Tool    string    `json:"tool"`
	Summary string    `json:"summary,omitempty"`
	State   string    `json:"state"`
}
