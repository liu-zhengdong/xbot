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

// TranscriptHandlers keeps the existing source methods and their own IO routes.
// All three original methods ignore calling context and have no turn requirement.
func (k *Kernel) TranscriptHandlers() *cliops.TranscriptHandlers {
	redact := k.redactor()
	fits := func(reply cliops.TranscriptReadReply) bool {
		raw, err := json.Marshal(reply)
		return err == nil && len(raw)+1 <= tools.DefaultShellOutputBytes && len(cliops.FormatTranscriptRead(reply)) <= tools.DefaultShellOutputBytes
	}
	fail := func(err error) (cliops.TranscriptReadReply, error) {
		reply := cliops.TranscriptReadReply{Error: redact(err.Error())}
		for !fits(reply) {
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
		reply := cliops.TranscriptReadReply{Text: text}
		raw, err := json.Marshal(reply)
		if err != nil {
			return fail(err)
		}
		raw = append(raw, '\n')
		if len(raw) <= tools.DefaultShellOutputBytes && len(cliops.FormatTranscriptRead(reply)) <= tools.DefaultShellOutputBytes {
			return reply, nil
		}
		if len(raw) > work.ResultMaxBytes {
			return fail(fmt.Errorf("transcript: result is %d bytes, over %d", len(raw), work.ResultMaxBytes))
		}
		// WriteResult generates a 32-hex filename. Both metadata displays must
		// fit before creating a file; actual IO resolves the current source route.
		reply = cliops.TranscriptReadReply{File: work.ResultDir + "/" + strings.Repeat("0", 32) + ".json", Access: "local", Bytes: int64(len(raw))}
		if !fits(reply) {
			return fail(errors.New("transcript: metadata exceeds response budget"))
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
			return fail(fmt.Errorf("transcript: write read result: %w", err))
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
