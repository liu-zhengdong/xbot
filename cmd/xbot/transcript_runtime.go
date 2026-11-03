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

const transcriptUsage = `Usage: xbot [--root DIR] transcript search --self [--query TEXT | --query-file FILE|-] [--bot NAME] [--json]
  xbot [--root DIR] transcript expand --self --from N --to N [--bot NAME] [--json]
  xbot [--root DIR] transcript detail --self --seq N [--experience ID] [--json]
  xbot transcript --help
  xbot transcript search|expand|detail --help
--self selects the active source through XBOT_ENDPOINT and canonical XBOT_ROOT;
XBOT_BOT is diagnostic only. The original transcript methods have no call ID.
search returns the original ranked seq/role snippet list; use a returned tool
seq with expand to read that paired result as recorded, without re-running it.
--query-file - reads stdin. Raw input and escaped JSON each allow 1 MiB.
detail reads one source message whole: seq is 1-based, or a 0-based offset into
--experience's original range. expand and search may name another Bot that
shares an owner; detail stays on this source.
Results preserve the original 4/8/64 KiB semantic text and foreign fence, or
reference a complete local JSON artifact. Results print before a failure exit.
Help works offline. Runtime calls require the active source endpoint.
`

func cmdTranscriptRuntime(rawRoot string, rootExplicit bool, args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runTranscriptRuntime(ctx, rawRoot, rootExplicit, args, os.Stdin, os.Stdout)
}

func runTranscriptRuntime(ctx context.Context, rawRoot string, rootExplicit bool, args []string, stdin io.Reader, out io.Writer) error {
	command, err := cliops.ParseTranscriptRuntime(args)
	if err != nil {
		return err
	}
	if command.Help {
		_, err := fmt.Fprint(out, transcriptUsage)
		return err
	}
	root, endpoint, err := runtimeSource("transcript", rawRoot, rootExplicit)
	if err != nil {
		return err
	}
	var reply cliops.TranscriptReadReply
	var operationErr error
	switch command.Action {
	case "search":
		input, err := readTranscriptSearchInput(command, stdin)
		if err != nil {
			return err
		}
		reply, operationErr = cliops.CallTranscriptSearch(ctx, endpoint, root, input)
	case "expand":
		reply, operationErr = cliops.CallTranscriptExpand(ctx, endpoint, root, cliops.TranscriptExpand{Bot: command.Bot, From: command.From, To: command.To})
	case "detail":
		reply, operationErr = cliops.CallTranscriptDetail(ctx, endpoint, root, cliops.TranscriptDetail{Seq: command.Seq, Experience: command.Experience})
	default:
		return errors.New("transcript: expected search, expand or detail")
	}
	if operationErr != nil && reply == (cliops.TranscriptReadReply{}) {
		return operationErr
	}
	raw, err := json.Marshal(reply)
	if err != nil {
		return err
	}
	text := cliops.FormatTextResult(reply)
	if !cliops.FitsOutput(reply, text) {
		return errors.New("transcript: runtime reply facts exceed 32 KiB Shell output budget")
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

func readTranscriptSearchInput(command cliops.TranscriptCommand, stdin io.Reader) (cliops.TranscriptSearch, error) {
	query, err := readBotPersonaText(cliops.BotCommand{Persona: command.Query, PersonaFile: command.QueryFile}, stdin)
	input := cliops.TranscriptSearch{Query: query, Bot: command.Bot}
	if err != nil {
		return input, fmt.Errorf("transcript: query input: %w", err)
	}
	wire, err := json.Marshal(input)
	if err != nil {
		return input, err
	}
	if len(wire) > cliops.TranscriptMaxRequest {
		return input, errors.New("transcript: request exceeds 1 MiB after JSON encoding")
	}
	return input, nil
}
