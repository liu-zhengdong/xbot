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
	"xbot/internal/tools"
)

const botUsage = `Usage: xbot [--root DIR] COMMAND
Runtime commands (require this source's XBOT_ENDPOINT and XBOT_ROOT):
  list --self [--json]
  info [NAME] --self [--steps N] [--json]
  new NAME --self (--persona TEXT | --persona-file FILE|-) [--json]
  delete NAME --self [--json]
  pin NAME --self [--json]
  unpin NAME --self [--json]
  primary set NAME --self [--json]
--self selects the active source, not the target Bot. Only the current primary source can create/delete/pin/unpin/set primary.
--persona-file - reads stdin. Input and final JSON requests are limited to 1 MiB.
Info steps above 10 are normalized on the server; zero or negative values omit steps.
Results are complete bounded output or a local file reference for a large list/status.
Applied/removed receipts remain visible when a later operation stage fails.
Existing offline commands (files/configuration only):
  list
  new NAME [--persona TEXT] [--provider NAME] [--model NAME]
  delete NAME
  pin NAME
  unpin NAME
  primary
  primary set NAME
Help works without a running instance. Runtime failures never fall back to offline files.
`

const personaUsage = `Usage: xbot [--root DIR] persona set --self (--text TEXT | --file FILE|-) [--json]
  xbot persona --help
  xbot persona set --help
Replaces this source Bot's complete duties; effective on its next wake.
Requires this source's XBOT_ENDPOINT and XBOT_ROOT. All source Bots can set their own duties.
--file - reads stdin. Raw input and the final JSON request are limited to 1 MiB.
Help works without a running instance. Runtime failures never fall back to offline files.
`

func cmdBotRuntime(rawRoot string, rootExplicit bool, args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runBotRuntime(ctx, rawRoot, rootExplicit, args, os.Stdin, os.Stdout)
}

func runBotRuntime(ctx context.Context, rawRoot string, rootExplicit bool, args []string, stdin io.Reader, out io.Writer) error {
	command, err := cliops.ParseBotRuntime(args)
	if err != nil {
		return err
	}
	if command.Help {
		usage := botUsage
		if command.Action == "persona" {
			usage = personaUsage
		}
		_, err := fmt.Fprint(out, usage)
		return err
	}
	if command.Offline {
		return errors.New("bot: expected a runtime command with --self")
	}
	root, endpoint, err := runtimeSource("bot", rawRoot, rootExplicit)
	if err != nil {
		return err
	}
	var result any
	var text string
	var operationErr error
	switch command.Action {
	case "list":
		reply, err := cliops.CallBotList(ctx, endpoint, root)
		if err != nil && reply.Error == "" && reply.Local == nil && reply.Remote == nil && reply.File == "" {
			return err
		}
		result, text, operationErr = reply, cliops.FormatBotList(reply), err
	case "info":
		reply, err := cliops.CallBotInfo(ctx, endpoint, root, cliops.BotInfoQuery{Bot: command.Name, Steps: command.Steps})
		if err != nil && reply.Error == "" && reply.Bots == nil && reply.Steps == nil && reply.File == "" {
			return err
		}
		result, text, operationErr = reply, cliops.FormatBotInfo(reply), err
	case "new":
		input, err := readBotCreateInput(command, stdin)
		if err != nil {
			return err
		}
		reply, err := cliops.CallBotCreate(ctx, endpoint, root, input)
		if err != nil && reply.Error == "" && reply.Name == "" && !reply.Applied {
			return err
		}
		result, text, operationErr = reply, cliops.FormatBotCreate(reply), err
	case "persona":
		input, err := readBotPersonaInput(command, stdin)
		if err != nil {
			return err
		}
		reply, err := cliops.CallBotPersona(ctx, endpoint, root, os.Getenv("XBOT_CALL_ID"), input)
		if err != nil && reply.Error == "" && reply.Name == "" && !reply.Written {
			return err
		}
		result, text, operationErr = reply, cliops.FormatBotPersona(reply), err
	case "delete":
		reply, err := cliops.CallBotDelete(ctx, endpoint, root, os.Getenv("XBOT_CALL_ID"), command.Name)
		if err != nil && reply.Error == "" && reply.Name == "" && !reply.Removed {
			return err
		}
		result, text, operationErr = reply, cliops.FormatBotDelete(reply), err
	case "pin", "unpin":
		reply, err := cliops.CallBotPin(ctx, endpoint, root, cliops.BotPin{Name: command.Name, Pinned: command.Action == "pin"})
		if err != nil && reply.Error == "" && reply.Name == "" && !reply.Applied {
			return err
		}
		result, text, operationErr = reply, cliops.FormatBotPin(reply), err
	case "primary":
		reply, err := cliops.CallBotPrimary(ctx, endpoint, root, command.Name)
		if err != nil && reply.Error == "" && reply.Name == "" && !reply.Applied {
			return err
		}
		result, text, operationErr = reply, cliops.FormatBotPrimary(reply), err
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	budget := min(cliops.BotMaxReply, tools.DefaultShellOutputBytes)
	if len(raw)+1 > budget || len(text) > budget {
		if reply, ok := result.(cliops.BotPersonaReply); ok {
			// Keep the committed-write facts visible even if a receipt cannot
			// fit. The request body never belongs in the short acknowledgement.
			operationErr = errors.New("persona: runtime result exceeds 32 KiB Shell output budget")
			reply.Error = operationErr.Error()
			raw, _ = json.Marshal(reply)
			text = cliops.FormatBotPersona(reply)
			if len(raw)+1 > budget || len(text) > budget {
				reply.Name = ""
				raw, _ = json.Marshal(reply)
				text = cliops.FormatBotPersona(reply)
			}
		}
		if len(raw)+1 > budget || len(text) > budget {
			return errors.New("bot: runtime result exceeds 32 KiB Shell output budget")
		}
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
