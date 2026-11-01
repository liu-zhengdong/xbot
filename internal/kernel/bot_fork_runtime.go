package kernel

import (
	"context"
	"encoding/json"
	"errors"

	"xbot/internal/cliops"
	"xbot/internal/clip"
	"xbot/internal/tools"
)

// botForkRuntime retains the original source Kernel and prepared Shell context.
// ForkWithResult owns its short current-source snapshots; no route spans the LLM.
func (k *Kernel) botForkRuntime(ctx context.Context, input cliops.BotFork) (reply cliops.BotForkReply, err error) {
	release := k.holdOperations()
	defer release()
	// The decoded value is frozen once, before looking up the original call.
	request := ForkRequest{Name: input.Name, Role: input.Role, Group: input.Group}
	redact := func(value string) string {
		return scrubSecrets(k.redactor()(value), k.secretValues())
	}
	reply = cliops.BotForkReply{Name: redact(request.Name), Stage: "validation"}
	defer func() {
		if err != nil {
			err = errors.New(redact(err.Error()))
			reply.Error = err.Error()
		}
		raw, marshalErr := json.Marshal(reply)
		if marshalErr != nil || len(raw)+1 > tools.DefaultShellOutputBytes || len(cliops.FormatBotFork(reply)) > tools.DefaultShellOutputBytes {
			// A display failure must retain acknowledged facts and the cause.
			reply.Name = ""
			if err == nil {
				err = errors.New("bot-fork: receipt exceeds response budget")
				reply.Error = err.Error()
			} else {
				reply.Error, _ = clip.Bytes(reply.Error, 4096)
				err = errors.New(reply.Error)
			}
		}
	}()
	prepared, cleanup, err := k.runtimeSourceCall(ctx)
	if err != nil {
		return reply, err
	}
	defer cleanup()
	if frozen, ok := prepared.Value(turnKey{}).(*turn); !ok || frozen == nil {
		return reply, errors.New("bot-fork: source frozen turn unavailable")
	}
	result, err := k.ForkWithResult(prepared, request)
	reply.Name, reply.GroupID = redact(result.Name), redact(result.GroupID)
	reply.Created, reply.CreateStage = result.Created, result.CreateStage
	reply.Confirmed = append([]string(nil), result.Confirmed...)
	reply.HandoffParts, reply.HandoffTotal = result.HandoffParts, result.HandoffTotal
	reply.Stage = result.Stage
	if result.UncertainMemory != nil {
		reply.UncertainMemory = &cliops.ForkMemoryAttempt{Bot: redact(result.UncertainMemory.Bot), Key: redact(result.UncertainMemory.Key)}
	}
	return reply, err
}
