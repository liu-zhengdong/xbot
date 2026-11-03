package cliops

import "context"

const TranscriptMaxRequest = jsonRequestLimit
const TranscriptMaxReply = jsonReplyLimit

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
type TranscriptReadReply = TextResult

type TranscriptHandlers struct {
	Search func(context.Context, TranscriptSearch) (TranscriptReadReply, error)
	Expand func(context.Context, TranscriptExpand) (TranscriptReadReply, error)
	Detail func(context.Context, TranscriptDetail) (TranscriptReadReply, error)
}
