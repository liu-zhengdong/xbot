package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"xbot/internal/cliops"
	"xbot/internal/clip"
	"xbot/internal/review"
	"xbot/internal/tools"
)

// cardsFail scrubs and bounds the display error the way every cards-family
// runtime handler does: only the error text may shrink.
func (k *Kernel) cardsFail(err error) (cliops.CardReply, error) {
	reply := cliops.CardReply{Error: scrubSecrets(k.redactor()(err.Error()), k.secretValues())}
	for {
		raw, marshalErr := json.Marshal(reply)
		if marshalErr == nil && len(raw)+1 <= tools.DefaultShellOutputBytes && len(cliops.FormatCards(reply)) <= tools.DefaultShellOutputBytes {
			return reply, errors.New(reply.Error)
		}
		reply.Error, _ = clip.Bytes(reply.Error, len(reply.Error)/2)
	}
}

// restartRuntime runs the retired restart_xbot tool's original body on the
// prepared source context. The original tool never asked: its class returned
// before the permission check, so this path is qualification only — the same
// runtimeSourceCall seam compact and links search/load use.
func (k *Kernel) restartRuntime(ctx context.Context, in cliops.RestartCall) (cliops.CardReply, error) {
	release := k.holdOperations()
	defer release()
	_, cleanup, err := k.runtimeSourceCall(ctx)
	if err != nil {
		return k.cardsFail(err)
	}
	defer cleanup()
	if k.Restart == nil {
		return k.cardsFail(errors.New("restart is unavailable in this session"))
	}
	hint := strings.TrimSpace(in.Hint)
	if len(hint) > 4000 {
		return k.cardsFail(errors.New("restart hint exceeds 4000 bytes"))
	}
	if err := k.Restart(hint); err != nil {
		return k.cardsFail(err)
	}
	return cliops.CardReply{Text: "已保存续跑提示，正在优雅重启 XBot。"}, nil
}

// httpRuntime runs the retired http_request tool's original body behind the
// original conditional owner card. runtimeApprovalSource rebinds the card to
// the original owner's current fork branch; the canonical typed request body
// keys deniedBefore/parkable/grant like the links intent blob. The permissive
// check matches the original reviewCall order, where NeedsOwner ran after it:
// an always-allow kernel never sent this card, so approveRuntimeIntent is
// skipped here rather than always fired the way links does.
func (k *Kernel) httpRuntime(ctx context.Context, in cliops.HTTPCall) (cliops.CardReply, error) {
	release := k.holdOperations()
	defer release()
	callCtx, cleanup, err := k.runtimeApprovalSource(ctx)
	if err != nil {
		return k.cardsFail(err)
	}
	defer cleanup()
	args, err := json.Marshal(struct {
		Method  string            `json:"method,omitempty"`
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers,omitempty"`
		Body    string            `json:"body,omitempty"`
	}{in.Method, in.URL, in.Headers, in.Body})
	if err != nil {
		return k.cardsFail(err)
	}
	method := strings.ToUpper(strings.TrimSpace(in.Method))
	if method == "" {
		method = http.MethodGet
	}
	if !k.permissive() {
		if err := k.approveRuntimeIntent(callCtx, args, method+" "+in.URL, review.HTTPNeedsOwner(args)); err != nil {
			return k.cardsFail(err)
		}
	}
	text, err := (&tools.HTTPRequest{Store: k.Vault}).Run(callCtx, args)
	if err != nil {
		return k.cardsFail(err)
	}
	return cliops.CardReply{Text: text}, nil
}
