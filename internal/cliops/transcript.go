package cliops

import "context"

const TranscriptMaxRequest = 1 << 20
const TranscriptMaxReply = 64 << 10
const transcriptOutputBudget = 32 << 10

// Transcript inputs preserve the original methods' domain validation.
type TranscriptSearch struct {
	Query string `json:"query"`
	Bot   string `json:"bot,omitempty"`
}

type TranscriptExpand struct {
	Bot  string `json:"bot,omitempty"`
	From int    `json:"from"`
	To   int    `json:"to"`
}

// Detail is source-only. A positive Experience makes Seq a zero-based offset.
type TranscriptDetail struct {
	Seq        int   `json:"seq"`
	Experience int64 `json:"experience,omitempty"`
}

// Text is the original semantic result; File refers to its complete JSON artifact.
type TranscriptReadReply struct {
	Text   string `json:"text,omitempty"`
	File   string `json:"file,omitempty"`
	Access string `json:"access,omitempty"`
	Bytes  int64  `json:"bytes,omitempty"`
	Error  string `json:"error,omitempty"`
}

type TranscriptHandlers struct {
	Search func(context.Context, TranscriptSearch) (TranscriptReadReply, error)
	Expand func(context.Context, TranscriptExpand) (TranscriptReadReply, error)
	Detail func(context.Context, TranscriptDetail) (TranscriptReadReply, error)
}
