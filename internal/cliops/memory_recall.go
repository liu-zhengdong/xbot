package cliops

const MemoryRecallMaxRequest = 1 << 20
const MemoryRecallMaxReply = 64 << 10
const memoryRecallOutputBudget = 32 << 10

// MemoryRecall preserves the original scanner's query, scope and local date input.
type MemoryRecall struct {
	Query string `json:"query"`
	Scope string `json:"scope,omitempty"`
	Since string `json:"since,omitempty"`
}

// Text is the full original rendered result, or File refers to its complete JSON.
type MemoryRecallReply struct {
	Text   string `json:"text,omitempty"`
	File   string `json:"file,omitempty"`
	Access string `json:"access,omitempty"`
	Bytes  int64  `json:"bytes,omitempty"`
	Error  string `json:"error,omitempty"`
}
