package cliops

const BotForkMaxRequest = 1 << 20
const BotForkMaxReply = 64 << 10
const botForkOutputBudget = 32 << 10

// BotFork carries the complete duties input. The original source writer owns
// name, role and group validation.
type BotFork struct {
	Name  string `json:"name"`
	Role  string `json:"role"`
	Group string `json:"group,omitempty"`
}

// ForkMemoryAttempt identifies an unconfirmed Remember call. Its Markdown may
// already have been written even though the call returned an error.
type ForkMemoryAttempt struct {
	Bot string `json:"bot"`
	Key string `json:"key"`
}

type BotForkReply struct {
	Name            string             `json:"name,omitempty"`
	Created         bool               `json:"created"`
	CreateStage     string             `json:"create_stage,omitempty"`
	Confirmed       []string           `json:"confirmed,omitempty"`
	HandoffParts    int                `json:"handoff_parts"`
	HandoffTotal    int                `json:"handoff_total"`
	UncertainMemory *ForkMemoryAttempt `json:"uncertain_memory,omitempty"`
	GroupID         string             `json:"group_id,omitempty"`
	Stage           string             `json:"stage"`
	Error           string             `json:"error,omitempty"`
}
