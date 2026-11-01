package cliops

import "context"

const ExperienceMaxRequest = 1 << 20
const ExperienceMaxReply = 64 << 10
const experienceOutputBudget = 32 << 10

// ExperienceRecord leaves summary and transcript range validation to the source.
type ExperienceRecord struct {
	Summary string `json:"summary"`
	Focus   string `json:"focus,omitempty"`
	From    int    `json:"from,omitempty"`
	To      int    `json:"to,omitempty"`
}

type ExperienceSearch struct {
	Query string `json:"query,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

// Recorded is true only after the original primitive acknowledges success.
// Null means unconfirmed; it does not assert that no database effects occurred.
type ExperienceRecordReply struct {
	ID       int64  `json:"id,omitempty"`
	From     int    `json:"from"`
	To       int    `json:"to"`
	Recorded *bool  `json:"recorded"`
	Error    string `json:"error,omitempty"`
}

// Text is the original rendered search result, or File identifies that complete
// result's JSON artifact. The client never expands artifact metadata.
type ExperienceSearchReply struct {
	Text   string `json:"text,omitempty"`
	File   string `json:"file,omitempty"`
	Access string `json:"access,omitempty"`
	Bytes  int64  `json:"bytes,omitempty"`
	Error  string `json:"error,omitempty"`
}

type ExperienceHandlers struct {
	Record func(context.Context, ExperienceRecord) (ExperienceRecordReply, error)
	Search func(context.Context, ExperienceSearch) (ExperienceSearchReply, error)
}
