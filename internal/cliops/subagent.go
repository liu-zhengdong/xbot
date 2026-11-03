package cliops

import "context"

const SubagentMaxRequest = 1 << 20
const SubagentMaxReply = 64 << 10
const subagentOutputBudget = 32 << 10

// SubagentList keeps the original optional limit; the domain owns its bounds.
type SubagentList struct {
	Limit int `json:"limit,omitempty"`
}

// SubagentRead carries one original subagent id.
type SubagentRead struct {
	ID string `json:"id"`
}

// SubagentExpand carries one id and an optional 1-based inclusive range.
type SubagentExpand struct {
	ID   string `json:"id"`
	From int    `json:"from,omitempty"`
	To   int    `json:"to,omitempty"`
}

// SubagentReply contains only the original text and the actual error.
type SubagentReply struct {
	Text  string `json:"text,omitempty"`
	Error string `json:"error,omitempty"`
}

type SubagentHandlers struct {
	List   func(context.Context, SubagentList) (SubagentReply, error)
	Read   func(context.Context, SubagentRead) (SubagentReply, error)
	Expand func(context.Context, SubagentExpand) (SubagentReply, error)
}
