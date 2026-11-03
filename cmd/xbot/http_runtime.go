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

const httpUsage = `Usage: xbot [--root DIR] http --self <url> [--method M] [--header K=V]... [--body B | --body-file FILE|-] [--json]
  xbot http --help
--self selects the active source through XBOT_ENDPOINT and canonical XBOT_ROOT;
XBOT_CALL_ID supplies the original active Shell context. XBOT_BOT is diagnostic
only. An empty --method means GET. --header repeats as K=V with unique names.
--body-file - reads stdin. Raw inputs and escaped JSON each allow 1 MiB. A call
that injects {{secret:NAME}} or uses a method other than GET/HEAD shows the
original owner approval card before it runs; a plain read runs without one.
Success prints the original "Status\nbody" reply. Received facts print before a
failure exit; no automatic retry is added. Help works offline; runtime calls
require the active source endpoint and the original Shell call ID.
`

func cmdHTTPRuntime(rawRoot string, rootExplicit bool, args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runHTTPRuntime(ctx, rawRoot, rootExplicit, args, os.Stdin, os.Stdout)
}

func runHTTPRuntime(ctx context.Context, rawRoot string, rootExplicit bool, args []string, stdin io.Reader, out io.Writer) error {
	command, err := cliops.ParseHTTPRuntime(args)
	if err != nil {
		return err
	}
	if command.Help {
		_, err := fmt.Fprint(out, httpUsage)
		return err
	}
	root, err := cardsRuntimeRoot(rawRoot, rootExplicit, "http")
	if err != nil {
		return err
	}
	endpoint := os.Getenv("XBOT_ENDPOINT")
	if endpoint == "" {
		return errors.New("http: --self requires an active local XBot endpoint")
	}
	callID := os.Getenv("XBOT_CALL_ID")
	if callID == "" {
		return errors.New("http: requires XBOT_CALL_ID from an active XBot shell")
	}
	input := cliops.HTTPCall{
		Method:  command.Method,
		URL:     command.URL,
		Headers: command.Headers,
		Body:    command.Body,
	}
	if command.BodyFile != "" {
		reader := stdin
		if command.BodyFile != "-" {
			file, err := os.Open(command.BodyFile)
			if err != nil {
				return err
			}
			defer file.Close()
			reader = file
		}
		raw, err := io.ReadAll(io.LimitReader(reader, cliops.CardsMaxRequest+1))
		if err != nil {
			return err
		}
		if len(raw) > cliops.CardsMaxRequest {
			return errors.New("http: body input exceeds 1 MiB")
		}
		input.Body = string(raw)
	}
	wire, err := json.Marshal(input)
	if err != nil {
		return err
	}
	if len(wire) > cliops.CardsMaxRequest {
		return errors.New("http: request exceeds 1 MiB after JSON encoding")
	}
	reply, operationErr := cliops.CallHTTP(ctx, endpoint, root, callID, input)
	if operationErr != nil && reply == (cliops.CardReply{}) {
		return operationErr
	}
	return writeCardReply(out, "http", command.JSON, reply, operationErr)
}
