package kernel

import (
	"context"
	"encoding/json"
	"errors"

	"xbot/internal/cliops"
	"xbot/internal/clip"
	"xbot/internal/tools"
)

// compactRuntime keeps cancellation and usage on the original prepared Shell
// context. CompactRange owns its short current IO and environment publication.
func (k *Kernel) compactRuntime(ctx context.Context, in cliops.Compact) (cliops.CompactReply, error) {
	release := k.holdOperations()
	defer release()
	fail := func(err error) (cliops.CompactReply, error) {
		reply := cliops.CompactReply{Error: scrubSecrets(k.redactor()(err.Error()), k.secretValues())}
		for {
			raw, marshalErr := json.Marshal(reply)
			if marshalErr == nil && len(raw)+1 <= tools.DefaultShellOutputBytes && len(cliops.FormatCompact(reply)) <= tools.DefaultShellOutputBytes {
				return reply, errors.New(reply.Error)
			}
			reply.Error, _ = clip.Bytes(reply.Error, len(reply.Error)/2)
		}
	}
	prepared, cleanup, err := k.runtimeSourceCall(ctx)
	if err != nil {
		return fail(err)
	}
	defer cleanup()
	text, err := k.CompactRange(prepared, in.From, in.To, in.Focus)
	if err != nil {
		return fail(err)
	}
	// The original method returns only its short acknowledgement. It may have
	// published usage/environment before a late error; do not infer rollback.
	return cliops.CompactReply{Text: text}, nil
}
