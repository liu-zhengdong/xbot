package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/config"
	"xbot/internal/inbox"
	"xbot/internal/peer"
	"xbot/internal/remote"
	"xbot/internal/tools"
	"xbot/internal/work"
)

// BotHandlers keeps the listener's source resources while consulting the original
// Kernel's status source at request time (the web host attaches it after publish).
func (k *Kernel) BotHandlers() *cliops.BotHandlers {
	source, resources, now := k.Bot, k.Resources, k.Now
	root := ""
	if source != nil {
		root = source.Root
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
	currentIO := func(ctx context.Context, fn func(*bot.Bot, *inbox.Inbox) error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if resources != nil {
			return resources.WithCurrentIO(fn)
		}
		if source == nil {
			return errors.New("bot: no source bot")
		}
		return fn(source, nil)
	}
	limit := min(cliops.BotMaxReply, tools.DefaultShellOutputBytes)
	fits := func(value any, human string) bool {
		raw, err := json.Marshal(value)
		return err == nil && len(raw)+1 <= limit && len(human) <= limit
	}
	// Both read replies use the complete JSON as the artifact. Reserve its exact
	// filename length and check both metadata formats before creating the file.
	project := func(ctx context.Context, value any, human string, reference func(string, int64) (any, string)) (string, int64, error) {
		raw, err := json.Marshal(value)
		if err != nil {
			return "", 0, err
		}
		raw = append(raw, '\n')
		if len(raw) <= limit && len(human) <= limit {
			return "", 0, nil
		}
		if len(raw) > work.ResultMaxBytes {
			return "", 0, fmt.Errorf("bot: result is %d bytes, over %d", len(raw), work.ResultMaxBytes)
		}
		placeholder := work.ResultDir + "/" + strings.Repeat("0", 32) + ".json"
		metadata, text := reference(placeholder, int64(len(raw)))
		if !fits(metadata, text) {
			return "", 0, errors.New("bot: metadata exceeds response budget")
		}
		var file string
		err = currentIO(ctx, func(current *bot.Bot, _ *inbox.Inbox) error {
			var err error
			file, err = work.WriteResult(work.BotDir(root, current.Name()), raw, "json")
			return err
		})
		return file, int64(len(raw)), err
	}
	validateDelete := func(ctx context.Context, name string) error {
		return currentIO(ctx, func(current *bot.Bot, _ *inbox.Inbox) error {
			primary, err := bot.Primary(root)
			if err != nil || primary != current.Name() {
				return errNotPrimary
			}
			if name != current.Name() {
				visible, err := peer.VisibleBots(root, current.Name())
				if err != nil {
					return err
				}
				found := false
				for _, cfg := range visible {
					if cfg.Name == name {
						found = true
						break
					}
				}
				if !found {
					return errNotVisible
				}
			}
			return bot.CheckDelete(root, name)
		})
	}
	return &cliops.BotHandlers{
		Pin:     k.pinBotRuntime,
		Primary: k.primaryBotRuntime,
		List: func(ctx context.Context) (cliops.BotListReply, error) {
			release := k.holdOperations()
			defer release()
			fail := func(err error) (cliops.BotListReply, error) {
				err = failure(err)
				return cliops.BotListReply{Error: err.Error()}, err
			}
			local, distant := []cliops.BotListLocal{}, []cliops.BotListRemote{}
			err := currentIO(ctx, func(current *bot.Bot, _ *inbox.Inbox) error {
				visible, err := peer.VisibleBots(root, current.Name())
				if err != nil {
					return err
				}
				for _, cfg := range visible {
					preview := strings.TrimSpace(cfg.Persona)
					if len(preview) > 80 {
						preview = preview[:80]
					}
					local = append(local, cliops.BotListLocal{Name: redact(cfg.Name), PersonaPreview: redact(preview)})
				}
				remotes, err := remote.VisibleBots(root, current.Config().Owners, now())
				if err != nil {
					return err
				}
				for _, entry := range remotes {
					distant = append(distant, cliops.BotListRemote{Name: redact(entry.Name), Seen: entry.Seen})
				}
				return nil
			})
			if err != nil {
				return fail(err)
			}
			reply := cliops.BotListReply{Local: &local, Remote: &distant, Count: len(local) + len(distant)}
			file, size, err := project(ctx, reply, cliops.FormatBotList(reply), func(file string, size int64) (any, string) {
				ref := cliops.BotListReply{Count: reply.Count, File: file, Bytes: size, Access: "local"}
				return ref, cliops.FormatBotList(ref)
			})
			if err != nil {
				return fail(err)
			}
			if file != "" {
				reply.Local, reply.Remote, reply.File, reply.Bytes, reply.Access = nil, nil, file, size, "local"
			}
			return reply, nil
		},
		Info: func(ctx context.Context, query cliops.BotInfoQuery) (cliops.BotInfoReply, error) {
			release := k.holdOperations()
			defer release()
			fail := func(err error) (cliops.BotInfoReply, error) {
				err = failure(err)
				return cliops.BotInfoReply{Error: err.Error()}, err
			}
			if err := currentIO(ctx, func(*bot.Bot, *inbox.Inbox) error { return nil }); err != nil {
				return fail(err)
			}
			query.Bot = strings.TrimSpace(query.Bot)
			list := k.BotStatuses()
			if query.Bot != "" {
				filtered := make([]BotStatus, 0, 1)
				for _, status := range list {
					if status.Bot == query.Bot {
						filtered = append(filtered, status)
					}
				}
				if len(filtered) == 0 {
					return fail(fmt.Errorf("bot_status: %s is not a same-owner bot", query.Bot))
				}
				list = filtered
			}
			views := make([]cliops.BotStatusView, 0, len(list))
			for _, status := range list {
				view := cliops.BotStatusView{Bot: redact(status.Bot), State: redact(status.State), Tool: redact(status.Tool), ToolSummary: redact(status.ToolSummary), Since: redact(status.Since), Backgrounds: status.Backgrounds, Agents: status.Agents, CompletedUnread: status.CompletedUnread, LastMessage: redact(status.LastMessage), Todo: redact(status.Todo)}
				if u := status.Tokens; u != nil {
					view.Tokens = &cliops.BotTokenUse{Prompt: u.Prompt, Completion: u.Completion, Cached: u.Cached, TurnCompletion: u.TurnCompletion, SessionPrompt: u.SessionPrompt, SessionCompletion: u.SessionCompletion, SessionCached: u.SessionCached, Window: u.Window, Estimated: u.Estimated}
				}
				views = append(views, view)
			}
			steps := []cliops.BotStepView{}
			name := query.Bot
			if name == "" && len(list) == 1 {
				name = list[0].Bot
			}
			if name != "" && query.Steps > 0 {
				for _, step := range k.BotSteps(name, min(query.Steps, 10)) {
					steps = append(steps, cliops.BotStepView{Time: step.Time, Tool: redact(step.Tool), Summary: redact(step.Summary), State: redact(step.State)})
				}
			}
			reply := cliops.BotInfoReply{Bots: &views, Steps: &steps, Count: len(views)}
			file, size, err := project(ctx, reply, cliops.FormatBotInfo(reply), func(file string, size int64) (any, string) {
				ref := cliops.BotInfoReply{Count: reply.Count, File: file, Bytes: size, Access: "local"}
				return ref, cliops.FormatBotInfo(ref)
			})
			if err != nil {
				return fail(err)
			}
			if file != "" {
				reply.Bots, reply.Steps, reply.File, reply.Bytes, reply.Access = nil, nil, file, size, "local"
			}
			return reply, nil
		},
		Create: func(ctx context.Context, in cliops.BotCreate) (cliops.BotCreateReply, error) {
			release := k.holdOperations()
			defer release()
			in.Name, in.Persona = strings.TrimSpace(in.Name), strings.TrimSpace(in.Persona)
			reply := cliops.BotCreateReply{Name: redact(in.Name), Stage: "validation"}
			err := currentIO(ctx, func(current *bot.Bot, _ *inbox.Inbox) error {
				primary, err := bot.Primary(root)
				if err != nil || primary != current.Name() {
					return errNotPrimary
				}
				if in.Persona == "" {
					return errors.New("bot: purpose/persona is required; ask the owner what this bot should do before creating it")
				}
				cfg := current.Config()
				created, err := bot.Create(root, in.Name, in.Persona, append([]string(nil), cfg.Links...))
				if err != nil {
					var committed *bot.CreateCommitError
					if errors.As(err, &committed) {
						reply.Applied, reply.Stage = committed.Applied, committed.Stage
					}
					return err
				}
				reply.Name, reply.Applied, reply.Stage = redact(created.Name()), true, "owners"
				if len(cfg.Owners) > 0 {
					if err := created.SetOwners(cfg.Owners); err != nil {
						return err
					}
				}
				reply.Stage = "complete"
				return nil
			})
			if err != nil {
				err = failure(err)
				reply.Error = err.Error()
			}
			if !fits(reply, cliops.FormatBotCreate(reply)) {
				err = errors.New("bot: create receipt exceeds response budget")
				reply.Name, reply.Error = "", err.Error()
			}
			return reply, err
		},
		Delete: func(ctx context.Context, name string) (cliops.BotDeleteReply, error) {
			release := k.holdOperations()
			defer release()
			name = strings.TrimSpace(name)
			reply := cliops.BotDeleteReply{Name: redact(name), Stage: "validate"}
			err := validateDelete(ctx, name)
			if err == nil {
				reply.Stage = "approval"
				err = k.checkRuntimeBotDelete(ctx, name)
			}
			if err == nil {
				reply.Stage = "validate"
				err = validateDelete(ctx, name)
			}
			if err == nil {
				reply.Stage = "delete"
				if resources != nil && resources.Delete != nil {
					reply.Removed, err = resources.Delete(ctx, name)
				} else {
					reply.Removed, err = bot.DeleteWithResult(root, name)
				}
				if reply.Removed && err != nil {
					reply.Stage = "vault_key"
				}
			}
			if err == nil {
				reply.Stage = "complete"
			} else {
				err = failure(err)
				reply.Error = err.Error()
			}
			if !fits(reply, cliops.FormatBotDelete(reply)) {
				err = errors.New("bot: delete receipt exceeds response budget")
				reply.Name, reply.Error = "", err.Error()
			}
			return reply, err
		},
	}
}
