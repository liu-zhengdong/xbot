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
	"xbot/internal/inbox"
	"xbot/internal/tools"
	"xbot/internal/work"
)

func (k *Kernel) memoryRecallRuntime(ctx context.Context, in cliops.MemoryRecall) (cliops.MemoryRecallReply, error) {
	release := k.holdOperations()
	defer release()
	redact := func(text string) string {
		return scrubSecrets(k.redactor()(text), k.secretValues())
	}
	fits := func(reply cliops.MemoryRecallReply) bool {
		raw, err := json.Marshal(reply)
		return err == nil && len(raw)+1 <= tools.DefaultShellOutputBytes && len(cliops.FormatMemoryRecall(reply)) <= tools.DefaultShellOutputBytes
	}
	fail := func(err error) (cliops.MemoryRecallReply, error) {
		reply := cliops.MemoryRecallReply{Error: redact(err.Error())}
		for !fits(reply) {
			reply.Error, _ = clip.Bytes(reply.Error, len(reply.Error)/2)
		}
		return reply, errors.New(reply.Error)
	}
	prepared, cleanup, err := k.runtimeSourceCall(ctx)
	if err != nil {
		return fail(err)
	}
	defer cleanup()
	if frozen, ok := prepared.Value(turnKey{}).(*turn); !ok || frozen == nil {
		return fail(errors.New("memory: source turn unavailable"))
	}
	text, err := k.recall(prepared, recallInput{Query: in.Query, Scope: in.Scope, Since: in.Since})
	if err != nil {
		return fail(err)
	}
	// Preserve the scanner's complete twenty-hit projection before applying the
	// original result redaction and the actual JSON/human display budgets.
	reply := cliops.MemoryRecallReply{Text: redact(text)}
	raw, err := json.Marshal(reply)
	if err != nil {
		return fail(err)
	}
	raw = append(raw, '\n')
	if len(raw) <= tools.DefaultShellOutputBytes && len(cliops.FormatMemoryRecall(reply)) <= tools.DefaultShellOutputBytes {
		return reply, nil
	}
	if len(raw) > work.ResultMaxBytes {
		return fail(fmt.Errorf("memory: result is %d bytes, over %d", len(raw), work.ResultMaxBytes))
	}
	// WriteResult generates exactly 32 random hex digits. Validate both metadata
	// projections before writing, then resolve the current workspace under route.
	reply = cliops.MemoryRecallReply{File: work.ResultDir + "/" + strings.Repeat("0", 32) + ".json", Access: "local", Bytes: int64(len(raw))}
	if !fits(reply) {
		return fail(errors.New("memory: metadata exceeds response budget"))
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
		return fail(fmt.Errorf("memory: write recall result: %w", err))
	}
	return reply, nil
}
