package cliops

import "context"

const ExperienceMaxRequest = jsonRequestLimit
const ExperienceMaxReply = jsonReplyLimit

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
type ExperienceSearchReply = TextResult

type ExperienceHandlers struct {
	Record func(context.Context, ExperienceRecord) (ExperienceRecordReply, error)
	Search func(context.Context, ExperienceSearch) (ExperienceSearchReply, error)
}
