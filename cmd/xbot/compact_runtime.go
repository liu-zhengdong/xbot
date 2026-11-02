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

const compactUsage = `Usage: xbot [--root DIR] compact --self --from N --to N [--focus TEXT | --focus-file FILE|-] [--json]
  xbot compact --help
--self selects the active source through XBOT_ENDPOINT and canonical XBOT_ROOT;
XBOT_CALL_ID supplies the original active Shell context. XBOT_BOT is diagnostic only.
--focus-file - reads stdin. Raw focus and escaped JSON each allow 1 MiB.
The inclusive range must satisfy 1 <= from <= to <= transcript count, preserve
complete tool pairs and the original raw tail (default 8 entries), and avoid
partial overlap with existing summary blocks or recompacting the same range.
Success prints the original compacted f-t acknowledgement. Actual errors print
before a failure exit; a late error can follow model usage or an environment update.
No automatic retry is added. Help works offline; runtime calls require the active
source endpoint and original Shell call ID.
`

func cmdCompactRuntime(rawRoot string, rootExplicit bool, args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runCompactRuntime(ctx, rawRoot, rootExplicit, args, os.Stdin, os.Stdout)
}

func runCompactRuntime(ctx context.Context, rawRoot string, rootExplicit bool, args []string, stdin io.Reader, out io.Writer) error {
	command, err := cliops.ParseCompactRuntime(args)
	if err != nil {
		return err
	}
	if command.Help {
		_, err := fmt.Fprint(out, compactUsage)
		return err
	}
	root := os.Getenv("XBOT_ROOT")
	if strings.TrimSpace(root) == "" {
		return errors.New("compact: --self requires XBOT_ROOT from an active runtime")
	}
	root, err = cliops.CanonicalRoot(root)
	if err != nil {
		return fmt.Errorf("compact: runtime root: %w", err)
	}
	if rootExplicit {
		if strings.TrimSpace(rawRoot) == "" {
			return errors.New("compact: explicit --root must name the runtime data root")
		}
		given, err := cliops.CanonicalRoot(rawRoot)
		if err != nil {
			return fmt.Errorf("compact: explicit root: %w", err)
		}
		if given != root {
			return errors.New("compact: runtime data root mismatch")
		}
	}
	endpoint := os.Getenv("XBOT_ENDPOINT")
	if endpoint == "" {
		return errors.New("compact: --self requires an active local XBot endpoint")
	}
	callID := os.Getenv("XBOT_CALL_ID")
	if callID == "" {
		return errors.New("compact: requires XBOT_CALL_ID from an active XBot shell")
	}
	focus, err := readBotPersonaText(cliops.BotCommand{Persona: command.Focus, PersonaFile: command.FocusFile}, stdin)
	if err != nil {
		return fmt.Errorf("compact: focus input: %w", err)
	}
	input := cliops.Compact{From: command.From, To: command.To, Focus: focus}
	wire, err := json.Marshal(input)
	if err != nil {
		return err
	}
	if len(wire) > cliops.CompactMaxRequest {
		return errors.New("compact: request exceeds 1 MiB after JSON encoding")
	}
	reply, operationErr := cliops.CallCompact(ctx, endpoint, root, callID, input)
	if operationErr != nil && reply == (cliops.CompactReply{}) {
		return operationErr
	}
	raw, err := json.Marshal(reply)
	if err != nil {
		return err
	}
	text := cliops.FormatCompact(reply)
	if len(raw)+1 > tools.DefaultShellOutputBytes || len(text) > tools.DefaultShellOutputBytes {
		return errors.New("compact: runtime reply facts exceed 32 KiB Shell output budget")
	}
	if command.JSON {
		raw = append(raw, '\n')
	} else {
		raw = []byte(text)
	}
	if _, err := out.Write(raw); err != nil {
		return err
	}
	return operationErr
}
