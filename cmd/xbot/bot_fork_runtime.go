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

const botForkUsage = `Usage: xbot [--root DIR] bot-fork NAME --self (--role TEXT | --role-file FILE|-) [--group NAME] [--json]
  xbot bot-fork --help
--role is the new Bot's duties/persona, not its runtime preset. --role-file -
reads stdin; file/stdin supports the full 8 KiB duties without Shell command limits.
Raw input and the final escaped JSON request each allow 1 MiB. The original source
validates the name, duties and group. --self selects the active source Bot through
XBOT_ENDPOINT and canonical XBOT_ROOT; XBOT_BOT is diagnostic only.
Created acknowledges the original Bot config, not complete setup. Confirmations
and handoff_parts count successful original calls only. An uncertain memory attempt
may already have written Markdown. Receipts print before a failure exit; preserve
them and do not blindly retry a partial or uncertain fork. No automatic retry is added.
Help works offline. Runtime failures require the active source endpoint.
`

func cmdBotForkRuntime(rawRoot string, rootExplicit bool, args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runBotForkRuntime(ctx, rawRoot, rootExplicit, args, os.Stdin, os.Stdout)
}

func runBotForkRuntime(ctx context.Context, rawRoot string, rootExplicit bool, args []string, stdin io.Reader, out io.Writer) error {
	command, err := cliops.ParseBotForkRuntime(args)
	if err != nil {
		return err
	}
	if command.Help {
		_, err := fmt.Fprint(out, botForkUsage)
		return err
	}
	root := os.Getenv("XBOT_ROOT")
	if strings.TrimSpace(root) == "" {
		return errors.New("bot-fork: --self requires XBOT_ROOT from an active runtime")
	}
	root, err = cliops.CanonicalRoot(root)
	if err != nil {
		return fmt.Errorf("bot-fork: runtime root: %w", err)
	}
	if rootExplicit {
		if strings.TrimSpace(rawRoot) == "" {
			return errors.New("bot-fork: explicit --root must name the runtime data root")
		}
		given, err := cliops.CanonicalRoot(rawRoot)
		if err != nil {
			return fmt.Errorf("bot-fork: explicit root: %w", err)
		}
		if given != root {
			return errors.New("bot-fork: runtime data root mismatch")
		}
	}
	endpoint := os.Getenv("XBOT_ENDPOINT")
	if endpoint == "" {
		return errors.New("bot-fork: --self requires an active local XBot endpoint")
	}
	input, err := readBotForkInput(command, stdin)
	if err != nil {
		return err
	}
	reply, operationErr := cliops.CallBotFork(ctx, endpoint, root, os.Getenv("XBOT_CALL_ID"), input)
	if operationErr != nil && reply.Name == "" && !reply.Created && reply.CreateStage == "" && len(reply.Confirmed) == 0 && reply.HandoffParts == 0 && reply.HandoffTotal == 0 && reply.UncertainMemory == nil && reply.GroupID == "" && reply.Stage == "" && reply.Error == "" {
		return operationErr
	}
	raw, err := json.Marshal(reply)
	if err != nil {
		return err
	}
	text := cliops.FormatBotFork(reply)
	if len(raw)+1 > tools.DefaultShellOutputBytes || len(text) > tools.DefaultShellOutputBytes {
		return errors.New("bot-fork: runtime receipt facts exceed 32 KiB Shell output budget")
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

func readBotForkInput(command cliops.BotForkCommand, stdin io.Reader) (cliops.BotFork, error) {
	role, err := readBotPersonaText(cliops.BotCommand{Persona: command.Role, PersonaFile: command.RoleFile}, stdin)
	input := cliops.BotFork{Name: command.Name, Role: role, Group: command.Group}
	if err != nil {
		return input, fmt.Errorf("bot-fork: role input: %w", err)
	}
	wire, err := json.Marshal(input)
	if err != nil {
		return input, err
	}
	if len(wire) > cliops.BotForkMaxRequest {
		return input, errors.New("bot-fork: request exceeds 1 MiB after JSON encoding")
	}
	return input, nil
}
