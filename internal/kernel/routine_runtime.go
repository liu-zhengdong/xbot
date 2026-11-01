package kernel

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/config"
	"xbot/internal/inbox"
	"xbot/internal/link"
	"xbot/internal/tools"
	"xbot/internal/work"
)

// routineReviewRequest retains the normalized request used by the actual write.
// An automatically generated ID is deliberately absent until review succeeds.
type routineReviewRequest struct {
	Action    string
	RequestID string
	Create    *cliops.RoutineCreate
	Patch     *cliops.RoutinePatch
}

// RoutineHandlers binds this listener to its original mounted Timer and source.
func (k *Kernel) RoutineHandlers() *cliops.RoutineHandlers {
	timer, timerErr := k.timer()
	sourceBot, resources, now := k.Bot, k.Resources, k.Now
	root := ""
	if sourceBot != nil {
		root = sourceBot.Root
	}
	if now == nil {
		now = time.Now
	}
	baseRedact := k.Redact
	if baseRedact == nil {
		baseRedact = bot.RedactText
	}
	redact := func(text string) string {
		text = config.RedactHeld(baseRedact(text))
		if resources != nil && resources.Redact != nil {
			text = resources.Redact(text)
		}
		return text
	}
	failure := func(err error) error {
		if err == nil {
			return nil
		}
		return errors.New(redact(err.Error()))
	}
	writeReply := func(id string, err error) (cliops.RoutineWriteReply, error) {
		reply := cliops.RoutineWriteReply{ID: redact(id)}
		if err == nil {
			reply.Applied, reply.Stage = true, "complete"
			return reply, nil
		}
		var committed *link.RoutineCommitError
		if errors.As(err, &committed) {
			reply.Applied, reply.Stage = committed.Applied, committed.Stage
		}
		err = failure(err)
		reply.Error = err.Error()
		return reply, err
	}
	ready := func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return timerErr
	}
	return &cliops.RoutineHandlers{
		Create: func(ctx context.Context, in cliops.RoutineCreate) (cliops.RoutineWriteReply, error) {
			release := k.holdOperations()
			defer release()
			if err := ready(ctx); err != nil {
				return writeReply(in.ID, err)
			}
			if strings.TrimSpace(in.ID) == "" {
				in.ID = ""
			}
			in.Prompt, in.Cron, in.At = strings.TrimSpace(in.Prompt), strings.TrimSpace(in.Cron), strings.TrimSpace(in.At)
			in.Skills = append([]string(nil), in.Skills...)
			for i := range in.Skills {
				in.Skills[i] = strings.TrimSpace(in.Skills[i])
			}
			if in.Precheck != nil {
				value := *in.Precheck
				in.Precheck = &value
			}
			if in.At != "" {
				at, err := time.Parse(time.RFC3339, in.At)
				if err != nil {
					return writeReply(in.ID, fmt.Errorf("timer: at: %w", err))
				}
				current := now().UTC()
				if at.Before(current.Add(-link.OneShotWindow)) {
					return writeReply(in.ID, fmt.Errorf("at is in the past: %s (now %s); use a future time", at.UTC().Format(time.RFC3339), current.Format(time.RFC3339)))
				}
				in.At = at.UTC().Format(time.RFC3339)
			}
			command := ""
			if in.Precheck != nil {
				command = *in.Precheck
			}
			if err := k.checkRuntimeRoutinePrecheck(ctx, command, routineReviewRequest{Action: "create", RequestID: in.ID, Create: &in}); err != nil {
				return writeReply(in.ID, err)
			}
			if err := ctx.Err(); err != nil {
				return writeReply(in.ID, err)
			}
			if in.ID == "" {
				var id [4]byte
				if _, err := rand.Read(id[:]); err != nil {
					return writeReply("", err)
				}
				in.ID = "r" + hex.EncodeToString(id[:])
			}
			r := link.Routine{ID: in.ID, Name: in.Name, Prompt: in.Prompt, Cron: in.Cron, At: in.At, Skills: in.Skills, Precheck: command, Enabled: true}
			if r.Cron != "" {
				r.Zone = "local"
			}
			return writeReply(in.ID, timer.Save(r))
		},
		List: func(ctx context.Context) (cliops.RoutineListReply, error) {
			release := k.holdOperations()
			defer release()
			fail := func(err error) (cliops.RoutineListReply, error) {
				err = failure(err)
				return cliops.RoutineListReply{Error: err.Error()}, err
			}
			if err := ready(ctx); err != nil {
				return fail(err)
			}
			list, err := timer.List()
			if err != nil {
				return fail(err)
			}
			views := make([]cliops.RoutineView, 0, len(list))
			for _, r := range list {
				skills := make([]string, len(r.Skills))
				for i, id := range r.Skills {
					skills[i] = redact(id)
				}
				views = append(views, cliops.RoutineView{ID: redact(r.ID), Name: redact(r.Name), Prompt: redact(r.Prompt), Cron: redact(r.Cron), At: redact(r.At), Zone: redact(r.Zone), Enabled: r.Enabled, Precheck: redact(r.Precheck), Skills: skills, LastSlot: redact(r.LastSlot)})
			}
			reply := cliops.RoutineListReply{Routines: &views, Count: len(views)}
			raw, err := json.Marshal(reply)
			if err != nil {
				return fail(err)
			}
			raw = append(raw, '\n')
			if len(raw) <= min(cliops.RoutineMaxReply, tools.DefaultShellOutputBytes) {
				return reply, nil
			}
			if len(raw) > work.ResultMaxBytes {
				return fail(fmt.Errorf("routine: result is %d bytes, over %d", len(raw), work.ResultMaxBytes))
			}
			reply.Routines, reply.Access, reply.Bytes = nil, "local", int64(len(raw))
			reply.File = work.ResultDir + "/" + strings.Repeat("0", 32) + ".json"
			metadata, err := json.Marshal(reply)
			if err != nil {
				return fail(err)
			}
			if len(metadata)+1 > min(cliops.RoutineMaxReply, tools.DefaultShellOutputBytes) {
				return fail(errors.New("routine: metadata exceeds response budget"))
			}
			// Timer.List has released its route before artifact workspace selection.
			write := func(current *bot.Bot, _ *inbox.Inbox) error {
				var err error
				reply.File, err = work.WriteResult(work.BotDir(root, current.Name()), raw, "json")
				return err
			}
			if resources != nil {
				err = resources.WithCurrentIO(write)
			} else if sourceBot != nil {
				err = write(sourceBot, nil)
			} else {
				err = errors.New("routine: no bot")
			}
			if err != nil {
				return fail(err)
			}
			return reply, nil
		},
		Update: func(ctx context.Context, id string, in cliops.RoutinePatch) (cliops.RoutineWriteReply, error) {
			release := k.holdOperations()
			defer release()
			if err := ready(ctx); err != nil {
				return writeReply(id, err)
			}
			in.Prompt, in.Cron = strings.TrimSpace(in.Prompt), strings.TrimSpace(in.Cron)
			if in.Precheck != nil {
				value := *in.Precheck
				in.Precheck = &value
			}
			if in.Skills != nil {
				value := append([]string{}, (*in.Skills)...)
				for i := range value {
					value[i] = strings.TrimSpace(value[i])
				}
				in.Skills = &value
			}
			old, err := timer.Load(id)
			if err != nil {
				return writeReply(id, err)
			}
			command := old.Precheck
			if in.Precheck != nil {
				command = *in.Precheck
			}
			if err := k.checkRuntimeRoutinePrecheck(ctx, command, routineReviewRequest{Action: "update", RequestID: id, Patch: &in}); err != nil {
				return writeReply(id, err)
			}
			if err := ctx.Err(); err != nil {
				return writeReply(id, err)
			}
			err = timer.Update(id, func(r *link.Routine) error {
				if strings.TrimSpace(in.Name) != "" {
					r.Name = in.Name
				}
				if in.Prompt != "" {
					r.Prompt = in.Prompt
				}
				if in.Cron != "" {
					r.Cron, r.Zone = in.Cron, "local"
				}
				if in.Enabled != nil {
					r.Enabled = *in.Enabled
				}
				if in.Precheck != nil {
					r.Precheck = *in.Precheck
				}
				if in.Skills != nil {
					r.Skills = *in.Skills
				}
				return nil
			})
			return writeReply(id, err)
		},
		Delete: func(ctx context.Context, id string) (cliops.RoutineWriteReply, error) {
			release := k.holdOperations()
			defer release()
			if err := ready(ctx); err != nil {
				return writeReply(id, err)
			}
			return writeReply(id, timer.Delete(id))
		},
	}
}
