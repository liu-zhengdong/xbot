package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"xbot/internal/cliops"
)

const memoryRecallUsage = `Usage: xbot [--root DIR] memory-recall --self [--query TEXT | --query-file FILE|-] [--scope bot|person] [--since YYYY-MM-DD] [--json]
  xbot memory-recall --help
--self selects the active source through XBOT_ENDPOINT and canonical XBOT_ROOT;
XBOT_CALL_ID supplies the original active Shell context. XBOT_BOT is diagnostic only.
--query-file - reads stdin. Raw input and escaped JSON each allow 1 MiB.
Query is optional; an empty query keeps the original no-token/no-match behavior.
Scope and since pass unchanged to the original scanner; dates use process local time.
Results preserve the original ranked path:line text or reference its complete local
JSON artifact. Results print before a failure exit. No automatic retry is added.
Help works offline. Runtime calls require the active source endpoint and call ID.
`

func cmdMemoryRecallRuntime(rawRoot string, rootExplicit bool, args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runMemoryRecallRuntime(ctx, rawRoot, rootExplicit, args, os.Stdin, os.Stdout)
}

func runMemoryRecallRuntime(ctx context.Context, rawRoot string, rootExplicit bool, args []string, stdin io.Reader, out io.Writer) error {
	command, err := cliops.ParseMemoryRecallRuntime(args)
	if err != nil {
		return err
	}
	if command.Help {
		_, err := fmt.Fprint(out, memoryRecallUsage)
		return err
	}
	root, endpoint, err := runtimeSource("memory-recall", rawRoot, rootExplicit)
	if err != nil {
		return err
	}
	callID := os.Getenv("XBOT_CALL_ID")
	if callID == "" {
		return errors.New("memory-recall: requires XBOT_CALL_ID from an active XBot shell")
	}
	input, err := readMemoryRecallInput(command, stdin)
	if err != nil {
		return err
	}
	reply, operationErr := cliops.CallMemoryRecall(ctx, endpoint, root, callID, input)
	if operationErr != nil && reply == (cliops.MemoryRecallReply{}) {
		return operationErr
	}
	raw, err := json.Marshal(reply)
	if err != nil {
		return err
	}
	text := cliops.FormatTextResult(reply)
	if !cliops.FitsOutput(reply, text) {
		return errors.New("memory-recall: runtime reply facts exceed 32 KiB Shell output budget")
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

func readMemoryRecallInput(command cliops.MemoryRecallCommand, stdin io.Reader) (cliops.MemoryRecall, error) {
	query, err := readBotPersonaText(cliops.BotCommand{Persona: command.Query, PersonaFile: command.QueryFile}, stdin)
	input := cliops.MemoryRecall{Query: query, Scope: command.Scope, Since: command.Since}
	if err != nil {
		return input, fmt.Errorf("memory-recall: query input: %w", err)
	}
	wire, err := json.Marshal(input)
	if err != nil {
		return input, err
	}
	if len(wire) > cliops.MemoryRecallMaxRequest {
		return input, errors.New("memory-recall: request exceeds 1 MiB after JSON encoding")
	}
	return input, nil
}
