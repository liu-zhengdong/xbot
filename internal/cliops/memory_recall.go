package cliops

const MemoryRecallMaxRequest = jsonRequestLimit
const MemoryRecallMaxReply = jsonReplyLimit

// MemoryRecall preserves the original scanner's query, scope and local date input.
type MemoryRecall struct {
	Query string `json:"query"`
	Scope string `json:"scope,omitempty"`
	Since string `json:"since,omitempty"`
}

// Text is the full original rendered result, or File refers to its complete JSON.
type MemoryRecallReply = TextResult
