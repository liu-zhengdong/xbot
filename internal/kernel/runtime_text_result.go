package kernel

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/inbox"
	"xbot/internal/work"
)

// runtimeTextResult projects already scrubbed semantic text without changing it.
// Only the actual artifact write holds the current source route.
func (k *Kernel) runtimeTextResult(text, domain, action string) (cliops.TextResult, error) {
	reply := cliops.TextResult{Text: text}
	raw, err := json.Marshal(reply)
	if err != nil {
		return cliops.TextResult{}, err
	}
	raw = append(raw, '\n')
	if cliops.FitsOutput(reply, cliops.FormatTextResult(reply)) {
		return reply, nil
	}
	if len(raw) > work.ResultMaxBytes {
		return cliops.TextResult{}, fmt.Errorf("%s: result is %d bytes, over %d", domain, len(raw), work.ResultMaxBytes)
	}
	// WriteResult generates exactly 32 hex digits. Check both metadata formats
	// before creating a file, then resolve the workspace at the actual write.
	reply = cliops.TextResult{File: work.ResultDir + "/" + strings.Repeat("0", 32) + ".json", Access: "local", Bytes: int64(len(raw))}
	if !cliops.FitsOutput(reply, cliops.FormatTextResult(reply)) {
		return cliops.TextResult{}, errors.New(domain + ": metadata exceeds response budget")
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
		return cliops.TextResult{}, fmt.Errorf("%s: write %s result: %w", domain, action, err)
	}
	return reply, nil
}
