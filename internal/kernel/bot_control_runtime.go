package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/config"
	"xbot/internal/inbox"
	"xbot/internal/peer"
	"xbot/internal/tools"
)

func (k *Kernel) pinBotRuntime(ctx context.Context, in cliops.BotPin) (cliops.BotPinReply, error) {
	release := k.holdOperations()
	defer release()
	source, resources := k.Bot, k.Resources
	redact := k.botControlRedactor()
	in.Name = strings.TrimSpace(in.Name)
	reply := cliops.BotPinReply{Name: redact(in.Name), Pinned: in.Pinned, Stage: "validation"}
	root, self := "", false
	write := func(current *bot.Bot, _ *inbox.Inbox) error {
		root = current.Root
		if err := qualifyBotControl(current, in.Name); err != nil {
			return err
		}
		self = in.Name == current.Name()
		if self {
			reply.Stage = "config"
			return current.SetPinned(in.Pinned)
		}
		return nil
	}
	err := ctx.Err()
	if err == nil {
		if resources != nil {
			err = resources.WithCurrentIO(write)
		} else if source != nil {
			err = write(source, nil)
		} else {
			err = errors.New("bot: no source bot")
		}
	}
	if err == nil && !self {
		var target *bot.Bot
		target, err = bot.Load(root, in.Name)
		if err == nil {
			reply.Stage = "config"
			err = target.SetPinned(in.Pinned)
		}
	}
	if err == nil {
		reply.Applied, reply.Stage = true, "complete"
	} else {
		err = errors.New(redact(err.Error()))
		reply.Error = err.Error()
	}
	raw, marshalErr := json.Marshal(reply)
	limit := min(cliops.BotMaxReply, tools.DefaultShellOutputBytes)
	if marshalErr != nil || len(raw)+1 > limit || len(cliops.FormatBotPin(reply)) > limit {
		err = errors.New("bot: pin receipt exceeds response budget")
		reply.Name, reply.Error = "", err.Error()
	}
	return reply, err
}

func (k *Kernel) primaryBotRuntime(ctx context.Context, name string) (cliops.BotPrimaryReply, error) {
	release := k.holdOperations()
	defer release()
	source, resources := k.Bot, k.Resources
	redact := k.botControlRedactor()
	name = strings.TrimSpace(name)
	reply := cliops.BotPrimaryReply{Name: redact(name), Stage: "validation"}
	root := ""
	validate := func(current *bot.Bot, _ *inbox.Inbox) error {
		root = current.Root
		return qualifyBotControl(current, name)
	}
	err := ctx.Err()
	if err == nil {
		if resources != nil {
			err = resources.WithCurrentIO(validate)
		} else if source != nil {
			err = validate(source, nil)
		} else {
			err = errors.New("bot: no source bot")
		}
	}
	// The original primary lock owns the pointer/pin sequence. Do not hold a
	// source route or host callback lock across this operation.
	if err == nil {
		reply.Stage = "primary"
		err = bot.SetPrimary(root, name)
		if err == nil {
			reply.Applied, reply.Stage = true, "complete"
		} else {
			var committed *bot.PrimaryCommitError
			if errors.As(err, &committed) {
				reply.Applied, reply.Stage = committed.Applied, committed.Stage
			}
		}
		if reply.Applied {
			reply.Primary = redact(name)
		}
	}
	if err != nil {
		err = errors.New(redact(err.Error()))
		reply.Error = err.Error()
	}
	raw, marshalErr := json.Marshal(reply)
	limit := min(cliops.BotMaxReply, tools.DefaultShellOutputBytes)
	if marshalErr != nil || len(raw)+1 > limit || len(cliops.FormatBotPrimary(reply)) > limit {
		err = errors.New("bot: primary receipt exceeds response budget")
		reply.Name, reply.Error = "", err.Error()
	}
	return reply, err
}

// qualifyBotControl uses the existing local owner intersection, including its
// explicit self exception. Qualification is read afresh for every operation.
func qualifyBotControl(current *bot.Bot, name string) error {
	primary, err := bot.Primary(current.Root)
	if err != nil || primary != current.Name() {
		return errNotPrimary
	}
	if name == current.Name() {
		return nil
	}
	visible, err := peer.VisibleBots(current.Root, current.Name())
	if err != nil {
		return err
	}
	for _, cfg := range visible {
		if cfg.Name == name {
			return nil
		}
	}
	return errNotVisible
}

func (k *Kernel) botControlRedactor() func(string) string {
	base, resources := k.Redact, k.Resources
	if base == nil {
		base = bot.RedactText
	}
	return func(text string) string {
		text = config.RedactHeld(base(text))
		if resources != nil && resources.Redact != nil {
			text = resources.Redact(text)
		}
		return text
	}
}
