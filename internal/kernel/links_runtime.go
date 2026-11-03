package kernel

import (
	"context"
	"encoding/json"
	"errors"

	"xbot/internal/agent"
	"xbot/internal/cliops"
	"xbot/internal/clip"
	"xbot/internal/link"
	"xbot/internal/tools"
)

// linksCardID normalizes a runtime id the same way the original Confirm did:
// a catalog hit keeps its canonical id so the card's first line matches the id
// StageSecrets is keyed to; a miss or store connector keeps the given text.
func linksCardID(id string) string {
	if e, ok := link.Lookup(id); ok {
		return e.ID
	}
	if c, ok := storeConnector(id); ok {
		return c.ID
	}
	return id
}

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

// linksInstallRuntime and linksRemoveRuntime keep the retired tool's owner
// card: runtimeApprovalSource rebinds the card to the original owner's current
// fork branch, the original Shell args carry a typed _xbot_links intent blob
// (deniedBefore/parkable/grant key on it, so one id's approval feeds no other
// id), and approveRuntimeIntent sends the Confirm-shaped card summary. Unlike
// the checkRuntime* precedents there is no permissive short-circuit: secrets
// arrive only through this card's stage, so the card always goes out. After
// approval the original install/remove helpers run byte for byte and the
// original Reconcile closes out.
func (k *Kernel) linksInstallRuntime(ctx context.Context, in cliops.LinksInstall) (cliops.LinksReply, error) {
	return k.linksApproveRuntime(ctx, "install", in.ID, func(t linksTool, id string) (string, error) {
		return t.install(id)
	})
}

func (k *Kernel) linksRemoveRuntime(ctx context.Context, in cliops.LinksRemove) (cliops.LinksReply, error) {
	return k.linksApproveRuntime(ctx, "remove", in.ID, func(t linksTool, id string) (string, error) {
		return t.remove(id)
	})
}

func (k *Kernel) linksApproveRuntime(ctx context.Context, action, id string, run func(linksTool, string) (string, error)) (cliops.LinksReply, error) {
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
	callCtx, cleanup, err := k.runtimeApprovalSource(ctx)
	if err != nil {
		return fail(err)
	}
	defer cleanup()
	var args map[string]json.RawMessage
	if err := json.Unmarshal(agent.ToolArgs(callCtx), &args); err != nil || args == nil {
		return fail(errors.New("links: source approval context unavailable"))
	}
	raw, err := json.Marshal(struct {
		Action string `json:"action"`
		ID     string `json:"id"`
	}{action, linksCardID(id)})
	if err != nil {
		return fail(err)
	}
	args["_xbot_links"] = raw
	approvalArgs, err := json.Marshal(args)
	if err != nil {
		return fail(err)
	}
	if err := k.approveRuntimeIntent(callCtx, approvalArgs, cardSummary(action, id), true); err != nil {
		return fail(err)
	}
	text, err := run(linksTool{k}, id)
	if err == nil && k.Resources != nil {
		_ = k.Resources.manager.Reconcile(callCtx)
	}
	if err != nil {
		return fail(err)
	}
	return cliops.LinksReply{Text: text}, nil
}
