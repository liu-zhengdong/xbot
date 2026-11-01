package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/clip"
	"xbot/internal/config"
	"xbot/internal/inbox"
	"xbot/internal/tools"
	"xbot/internal/work"
)

// ExperienceHandlers exposes the original source primitives. Their transcript
// bounds and IO remain operation-time decisions, independent of any Shell turn.
func (k *Kernel) ExperienceHandlers() *cliops.ExperienceHandlers {
	redact := k.redactor()
	fits := func(value any, human string) bool {
		raw, err := json.Marshal(value)
		return err == nil && len(raw)+1 <= tools.DefaultShellOutputBytes && len(human) <= tools.DefaultShellOutputBytes
	}
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
			for !fits(reply, cliops.FormatExperienceRecord(reply)) {
				reply.Error, _ = clip.Bytes(reply.Error, len(reply.Error)/2)
			}
			return reply, errors.New(reply.Error)
		},
		Search: func(_ context.Context, in cliops.ExperienceSearch) (cliops.ExperienceSearchReply, error) {
			release := k.holdOperations()
			defer release()
			fail := func(err error) (cliops.ExperienceSearchReply, error) {
				reply := cliops.ExperienceSearchReply{Error: redact(err.Error())}
				for !fits(reply, cliops.FormatExperienceSearch(reply)) {
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
			reply := cliops.ExperienceSearchReply{Text: text}
			raw, err := json.Marshal(reply)
			if err != nil {
				return fail(err)
			}
			raw = append(raw, '\n')
			if len(raw) <= tools.DefaultShellOutputBytes && len(cliops.FormatExperienceSearch(reply)) <= tools.DefaultShellOutputBytes {
				return reply, nil
			}
			if len(raw) > work.ResultMaxBytes {
				return fail(fmt.Errorf("experience: result is %d bytes, over %d", len(raw), work.ResultMaxBytes))
			}
			// WriteResult uses exactly 32 random hex digits. Check both actual
			// metadata projections before creating an artifact.
			reply = cliops.ExperienceSearchReply{File: work.ResultDir + "/" + strings.Repeat("0", 32) + ".json", Access: "local", Bytes: int64(len(raw))}
			if !fits(reply, cliops.FormatExperienceSearch(reply)) {
				return fail(errors.New("experience: metadata exceeds response budget"))
			}
			err = k.withCurrentIO(func(current *bot.Bot, _ *inbox.Inbox) error {
				workspace := k.currentWorkspaceFor(current)
				if workspace == "" {
					workspace = work.BotDir(current.Root, current.Name())
				}
				var err error
				reply.File, err = work.WriteResult(workspace, raw, "json")
				return err
			})
			if err != nil {
				return fail(fmt.Errorf("experience: write search result: %w", err))
			}
			return reply, nil
		},
	}
}
