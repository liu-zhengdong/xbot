package kernel

import (
	"context"
	"encoding/json"
	"errors"

	"xbot/internal/cliops"
	"xbot/internal/clip"
	"xbot/internal/tools"
)

// linksRuntime shares one prepared source context and reply binder for the
// search and load actions; the original linksTool helpers own the domain.
func (k *Kernel) linksRuntime(ctx context.Context, run func(context.Context, linksTool) (string, error)) (cliops.LinksReply, error) {
	release := k.holdOperations()
	defer release()
	fail := func(err error) (cliops.LinksReply, error) {
		reply := cliops.LinksReply{Error: scrubSecrets(k.redactor()(err.Error()), k.secretValues())}
		for {
			raw, marshalErr := json.Marshal(reply)
			if marshalErr == nil && len(raw)+1 <= tools.DefaultShellOutputBytes && len(cliops.FormatLinks(reply)) <= tools.DefaultShellOutputBytes {
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
	text, err := run(prepared, linksTool{k})
	if err != nil {
		return fail(err)
	}
	return cliops.LinksReply{Text: text}, nil
}

// linksSearchRuntime keeps qualification on the original prepared Shell
// context; the original search already scrubs secrets and falls back.
func (k *Kernel) linksSearchRuntime(ctx context.Context, in cliops.LinksSearch) (cliops.LinksReply, error) {
	return k.linksRuntime(ctx, func(_ context.Context, t linksTool) (string, error) {
		return t.search(in.Query), nil
	})
}

// linksLoadRuntime passes the prepared context through to the original
// loadContext so markLoaded and tool listing stay on this running kernel.
func (k *Kernel) linksLoadRuntime(ctx context.Context, in cliops.LinksLoad) (cliops.LinksReply, error) {
	return k.linksRuntime(ctx, func(prepared context.Context, t linksTool) (string, error) {
		return t.loadContext(prepared, in.ID)
	})
}
