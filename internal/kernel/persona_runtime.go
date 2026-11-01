package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/inbox"
	"xbot/internal/tools"
)

func (k *Kernel) personaBotRuntime(ctx context.Context, in cliops.BotPersona) (cliops.BotPersonaReply, error) {
	release := k.holdOperations()
	defer release()
	source, resources := k.Bot, k.Resources
	redact := k.botControlRedactor()
	// Keep the decoded request frozen across approval. Only the approval summary
	// uses its effective representation; WritePersona owns all text validation.
	text := in.Text
	reply := cliops.BotPersonaReply{}
	err := ctx.Err()
	if err == nil {
		err = k.checkRuntimePersona(ctx, strings.TrimSpace(text))
	}
	if err == nil {
		write := func(current *bot.Bot, _ *inbox.Inbox) error {
			reply.Name = current.Name()
			if err := current.WritePersona(text); err != nil {
				return err
			}
			reply.Written, reply.Effective = true, "next_wake"
			return nil
		}
		if resources != nil {
			err = resources.WithCurrentIO(write)
		} else if source != nil {
			err = write(source, nil)
		} else {
			err = errors.New("persona: no source bot")
		}
	}
	if reply.Name == "" {
		if resources != nil {
			reply.Name = resources.Name()
		} else if source != nil {
			reply.Name = source.Name()
		}
	}
	reply.Name = redact(reply.Name)
	if err != nil {
		err = errors.New(redact(err.Error()))
		reply.Error = err.Error()
	}
	raw, marshalErr := json.Marshal(reply)
	limit := min(cliops.BotMaxReply, tools.DefaultShellOutputBytes)
	if marshalErr != nil || len(raw)+1 > limit || len(cliops.FormatBotPersona(reply)) > limit {
		err = errors.New("persona: receipt exceeds response budget")
		reply.Name, reply.Error = "", err.Error()
	}
	return reply, err
}
