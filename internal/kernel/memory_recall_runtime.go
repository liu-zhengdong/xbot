package kernel

import (
	"context"
	"errors"

	"xbot/internal/cliops"
	"xbot/internal/clip"
)

func (k *Kernel) memoryRecallRuntime(ctx context.Context, in cliops.MemoryRecall) (cliops.MemoryRecallReply, error) {
	release := k.holdOperations()
	defer release()
	redact := func(text string) string {
		return scrubSecrets(k.redactor()(text), k.secretValues())
	}
	fail := func(err error) (cliops.MemoryRecallReply, error) {
		reply := cliops.MemoryRecallReply{Error: redact(err.Error())}
		for !cliops.FitsOutput(reply, cliops.FormatTextResult(reply)) {
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
	reply, err := k.runtimeTextResult(redact(text), "memory", "recall")
	if err != nil {
		return fail(err)
	}
	return reply, nil
}
