package kernel

import (
	"context"
	"errors"

	"xbot/internal/cliops"
	"xbot/internal/clip"
	"xbot/internal/config"
)

// ExperienceHandlers exposes the original source primitives. Their transcript
// bounds and IO remain operation-time decisions, independent of any Shell turn.
func (k *Kernel) ExperienceHandlers() *cliops.ExperienceHandlers {
	redact := k.redactor()
	return &cliops.ExperienceHandlers{
		Record: func(_ context.Context, in cliops.ExperienceRecord) (cliops.ExperienceRecordReply, error) {
			release := k.holdOperations()
			defer release()
			e, err := k.RecordExperience(in.Summary, in.Focus, in.From, in.To)
			reply := cliops.ExperienceRecordReply{ID: e.ID, From: e.From, To: e.To}
			if err == nil {
				recorded := true
				reply.Recorded = &recorded
				return reply, nil
			}
			// Only display diagnostics may shrink. Even a late Store.Add error
			// retains the original returned ID/range and unconfirmed null state.
			reply.Error = redact(err.Error())
			for !cliops.FitsOutput(reply, cliops.FormatExperienceRecord(reply)) {
				reply.Error, _ = clip.Bytes(reply.Error, len(reply.Error)/2)
			}
			return reply, errors.New(reply.Error)
		},
		Search: func(_ context.Context, in cliops.ExperienceSearch) (cliops.ExperienceSearchReply, error) {
			release := k.holdOperations()
			defer release()
			fail := func(err error) (cliops.ExperienceSearchReply, error) {
				reply := cliops.ExperienceSearchReply{Error: redact(err.Error())}
				for !cliops.FitsOutput(reply, cliops.FormatTextResult(reply)) {
					reply.Error, _ = clip.Bytes(reply.Error, len(reply.Error)/2)
				}
				return reply, errors.New(reply.Error)
			}
			text, err := k.SearchExperiences(in.Query, in.Limit)
			if err != nil {
				return fail(err)
			}
			// Search already applied presentHistory and its semantic 6 KiB cap.
			// Scrub newly held secrets without rebuilding or widening that text.
			text = config.RedactHeld(text)
			if k.Resources != nil && k.Resources.Redact != nil {
				text = k.Resources.Redact(text)
			}
			reply, err := k.runtimeTextResult(text, "experience", "search")
			if err != nil {
				return fail(err)
			}
			return reply, nil
		},
	}
}
