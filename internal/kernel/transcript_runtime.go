package kernel

import (
	"context"
	"errors"

	"xbot/internal/cliops"
	"xbot/internal/clip"
	"xbot/internal/config"
)

// TranscriptHandlers keeps the existing source methods and their own IO routes.
// All three original methods ignore calling context and have no turn requirement.
func (k *Kernel) TranscriptHandlers() *cliops.TranscriptHandlers {
	redact := k.redactor()
	fail := func(err error) (cliops.TranscriptReadReply, error) {
		reply := cliops.TranscriptReadReply{Error: redact(err.Error())}
		for !cliops.FitsOutput(reply, cliops.FormatTextResult(reply)) {
			reply.Error, _ = clip.Bytes(reply.Error, len(reply.Error)/2)
		}
		return reply, errors.New(reply.Error)
	}
	finish := func(text string, err error) (cliops.TranscriptReadReply, error) {
		if err != nil {
			return fail(err)
		}
		// presentHistory already applied the original redactor, byte cap and
		// foreign fence. Only scrub newly held values; do not rebuild that text.
		text = config.RedactHeld(text)
		if k.Resources != nil && k.Resources.Redact != nil {
			text = k.Resources.Redact(text)
		}
		reply, err := k.runtimeTextResult(text, "transcript", "read")
		if err != nil {
			return fail(err)
		}
		return reply, nil
	}
	return &cliops.TranscriptHandlers{
		Search: func(_ context.Context, in cliops.TranscriptSearch) (cliops.TranscriptReadReply, error) {
			release := k.holdOperations()
			defer release()
			return finish(k.RecallHistoryBot(in.Query, in.Bot))
		},
		Expand: func(_ context.Context, in cliops.TranscriptExpand) (cliops.TranscriptReadReply, error) {
			release := k.holdOperations()
			defer release()
			return finish(k.ExpandBot(in.Bot, in.From, in.To))
		},
		Detail: func(_ context.Context, in cliops.TranscriptDetail) (cliops.TranscriptReadReply, error) {
			release := k.holdOperations()
			defer release()
			return finish(k.Detail(in.Seq, in.Experience))
		},
	}
}
