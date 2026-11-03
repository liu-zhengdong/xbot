package cliops

const CompactMaxRequest = 1 << 20
const CompactMaxReply = 64 << 10
const compactOutputBudget = 32 << 10

// Compact preserves the original range and focus for CompactRange validation.
type Compact struct {
	From  int    `json:"from"`
	To    int    `json:"to"`
	Focus string `json:"focus,omitempty"`
}

// CompactReply contains only the original acknowledgement and actual error.
type CompactReply struct {
	Text  string `json:"text,omitempty"`
	Error string `json:"error,omitempty"`
}
