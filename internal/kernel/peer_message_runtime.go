package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/inbox"
	"xbot/internal/peer"
	"xbot/internal/tools"
)

// peerMessageRuntime uses the original listener and prepared Shell source. Only
// the source name/root snapshot holds the storage route; delivery runs outside it.
func (k *Kernel) peerMessageRuntime(ctx context.Context, input cliops.PeerMessage) (reply cliops.PeerMessageReply, err error) {
	release := k.holdOperations()
	defer release()
	// Freeze the decoded request before resolving the original execution context.
	target, text := strings.TrimSpace(input.Bot), input.Text
	redact := func(value string) string {
		return scrubSecrets(k.redactor()(value), k.secretValues())
	}
	reply = cliops.PeerMessageReply{Bot: redact(target), Route: "local", Stage: "validation"}
	if strings.Contains(target, "@") {
		reply.Route, reply.Gateway = "remote", "not_attempted"
	}
	defer func() {
		if err != nil {
			err = errors.New(redact(err.Error()))
			reply.Error = err.Error()
		}
		raw, marshalErr := json.Marshal(reply)
		if marshalErr != nil || len(raw)+1 > tools.DefaultShellOutputBytes || len(cliops.FormatPeerMessage(reply)) > tools.DefaultShellOutputBytes {
			// Display overflow cannot erase an actual delivery or queue fact.
			err = errors.New("peer-message: receipt exceeds response budget")
			reply.Bot, reply.Error = "", err.Error()
		}
	}()
	prepared, cleanup, err := k.runtimeSourceCall(ctx)
	if err != nil {
		return reply, err
	}
	defer cleanup()
	frozen, ok := prepared.Value(turnKey{}).(*turn)
	if !ok || frozen == nil {
		return reply, errors.New("peer-message: source frozen turn unavailable")
	}
	frozen.mu.Lock()
	chain, hops := frozen.turnChain, frozen.turnHops
	frozen.mu.Unlock()
	var name, root string
	if err = prepared.Err(); err != nil {
		return reply, err
	}
	err = k.withCurrentIO(func(current *bot.Bot, _ *inbox.Inbox) error {
		if current == nil {
			return errors.New("peer-message: no source bot")
		}
		name, root = current.Name(), current.Root
		return nil
	})
	if err != nil {
		return reply, err
	}
	if chain == "" {
		chain = "ch_" + name
	}
	if reply.Route == "remote" {
		return reply, errors.New("peer-message: remote runtime unavailable while implementation is pending")
	}
	result, err := (&peer.Bus{Root: root, Now: k.now}).MessageWithResult(name, target, text, chain, hops+1, k.now())
	reply.EventID, reply.Posted, reply.Stage = result.EventID, result.Posted, result.Stage
	return reply, err
}
