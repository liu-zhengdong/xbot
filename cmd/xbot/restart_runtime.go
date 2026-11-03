package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"xbot/internal/cliops"
	"xbot/internal/tools"
)

const restartUsage = `Usage: xbot [--root DIR] restart --self [--hint TEXT] [--json]
  xbot restart --help
--self selects the active source through XBOT_ENDPOINT and canonical XBOT_ROOT;
XBOT_CALL_ID supplies the original active Shell context. XBOT_BOT is diagnostic
only. The runtime saves the optional continuation hint for working Bots, then
gracefully restarts the XBot process. Success prints the original acknowledgement.
Actual errors print before a failure exit; no automatic retry is added. Help
works offline; runtime calls require the active source endpoint and the original
Shell call ID.
`

func cmdRestartRuntime(rawRoot string, rootExplicit bool, args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runRestartRuntime(ctx, rawRoot, rootExplicit, args, os.Stdout)
}

func runRestartRuntime(ctx context.Context, rawRoot string, rootExplicit bool, args []string, out io.Writer) error {
	command, err := cliops.ParseRestartRuntime(args)
	if err != nil {
		return err
	}
	if command.Help {
		_, err := fmt.Fprint(out, restartUsage)
		return err
	}
	root, err := cardsRuntimeRoot(rawRoot, rootExplicit, "restart")
	if err != nil {
		return err
	}
	endpoint := os.Getenv("XBOT_ENDPOINT")
	if endpoint == "" {
		return errors.New("restart: --self requires an active local XBot endpoint")
	}
	callID := os.Getenv("XBOT_CALL_ID")
	if callID == "" {
		return errors.New("restart: requires XBOT_CALL_ID from an active XBot shell")
	}
	input := cliops.RestartCall{Hint: command.Hint}
	wire, err := json.Marshal(input)
	if err != nil {
		return err
	}
	if len(wire) > cliops.CardsMaxRequest {
		return errors.New("restart: request exceeds 1 MiB after JSON encoding")
	}
	reply, operationErr := cliops.CallRestart(ctx, endpoint, root, callID, input)
	if operationErr != nil && reply == (cliops.CardReply{}) {
		return operationErr
	}
	return writeCardReply(out, "restart", command.JSON, reply, operationErr)
}

// cardsRuntimeRoot resolves the canonical XBOT_ROOT --self requires and matches
// an explicit --root against it, shared by the cards-family commands.
func cardsRuntimeRoot(rawRoot string, rootExplicit bool, noun string) (string, error) {
	root := os.Getenv("XBOT_ROOT")
	if strings.TrimSpace(root) == "" {
		return "", fmt.Errorf("%s: --self requires XBOT_ROOT from an active runtime", noun)
	}
	root, err := cliops.CanonicalRoot(root)
	if err != nil {
		return "", fmt.Errorf("%s: runtime root: %w", noun, err)
	}
	if rootExplicit {
		if strings.TrimSpace(rawRoot) == "" {
			return "", fmt.Errorf("%s: explicit --root must name the runtime data root", noun)
		}
		given, err := cliops.CanonicalRoot(rawRoot)
		if err != nil {
			return "", fmt.Errorf("%s: explicit root: %w", noun, err)
		}
		if given != root {
			return "", fmt.Errorf("%s: runtime data root mismatch", noun)
		}
	}
	return root, nil
}

// writeCardReply prints received facts before the failure exit, bounded by the
// 32 KiB Shell output budget like the other runtime commands.
func writeCardReply(out io.Writer, noun string, asJSON bool, reply cliops.CardReply, operationErr error) error {
	raw, err := json.Marshal(reply)
	if err != nil {
		return err
	}
	text := cliops.FormatCards(reply)
	if len(raw)+1 > tools.DefaultShellOutputBytes || len(text) > tools.DefaultShellOutputBytes {
		return fmt.Errorf("%s: runtime reply facts exceed 32 KiB Shell output budget", noun)
	}
	if asJSON {
		raw = append(raw, '\n')
	} else {
		raw = []byte(text)
	}
	if _, err := out.Write(raw); err != nil {
		return err
	}
	return operationErr
}
