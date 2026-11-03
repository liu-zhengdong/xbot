package cliops

import "context"

const JobsMaxRequest = 1 << 20
const JobsMaxReply = 64 << 10
const jobsOutputBudget = 32 << 10

// JobsOutput carries one job id with the original optional byte offset or
// line tail; the domain method owns their bounds and the tail-over-offset rule.
type JobsOutput struct {
	ID     string `json:"id"`
	Offset int    `json:"offset,omitempty"`
	Tail   int    `json:"tail,omitempty"`
}

// JobsStatus carries an optional id; empty lists the original job table.
type JobsStatus struct {
	ID string `json:"id,omitempty"`
}

// JobsCancel carries one job id.
type JobsCancel struct {
	ID string `json:"id"`
}

// JobsReply contains only the original text and the actual error.
type JobsReply struct {
	Text  string `json:"text,omitempty"`
	Error string `json:"error,omitempty"`
}

type JobsHandlers struct {
	Output func(context.Context, JobsOutput) (JobsReply, error)
	Status func(context.Context, JobsStatus) (JobsReply, error)
	Cancel func(context.Context, JobsCancel) (JobsReply, error)
}
