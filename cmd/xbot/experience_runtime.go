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

const experienceUsage = `Usage: xbot [--root DIR] experience record --self (--summary TEXT | --summary-file FILE|-) [--focus TEXT | --focus-file FILE|-] [--from N] [--to N] [--json]
  xbot [--root DIR] experience search --self [--query TEXT | --query-file FILE|-] [--limit N] [--json]
  xbot experience --help
  xbot experience record --help
  xbot experience search --help
--self selects the active source through XBOT_ENDPOINT and canonical XBOT_ROOT;
XBOT_BOT is diagnostic only. A file path of - reads stdin, for at most one field.
Each raw input and the final escaped JSON request allow 1 MiB. The source trims
and validates summary (nonempty, at most 2 KiB), focus and transcript bounds.
Zero bounds use the original operation-time defaults. Search uses the original
rendered result and limit policy (default 8); a complete result may be a local file.
Recorded true confirms the original call. Null is unconfirmed: database effects
may already have occurred. Receipts print before a failure exit. Preserve them
before considering another record; no automatic retry is added.
Help works offline. Runtime calls require the active source endpoint.
`

func cmdExperienceRuntime(rawRoot string, rootExplicit bool, args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runExperienceRuntime(ctx, rawRoot, rootExplicit, args, os.Stdin, os.Stdout)
}

func runExperienceRuntime(ctx context.Context, rawRoot string, rootExplicit bool, args []string, stdin io.Reader, out io.Writer) error {
	command, err := cliops.ParseExperienceRuntime(args)
	if err != nil {
		return err
	}
	if command.Help {
		_, err := fmt.Fprint(out, experienceUsage)
		return err
	}
	root, endpoint, err := runtimeSource("experience", rawRoot, rootExplicit)
	if err != nil {
		return err
	}
	var reply any
	var text string
	var operationErr error
	if command.Action == "record" {
		input, err := readExperienceRecordInput(command, stdin)
		if err != nil {
			return err
		}
		value, err := cliops.CallExperienceRecord(ctx, endpoint, root, input)
		if err != nil && value.Error == "" && value.ID == 0 && value.From == 0 && value.To == 0 && value.Recorded == nil {
			return err
		}
		reply, text, operationErr = value, cliops.FormatExperienceRecord(value), err
	} else {
		input, err := readExperienceSearchInput(command, stdin)
		if err != nil {
			return err
		}
		value, err := cliops.CallExperienceSearch(ctx, endpoint, root, input)
		if err != nil && value.Error == "" && value.Text == "" && value.File == "" && value.Access == "" && value.Bytes == 0 {
			return err
		}
		reply, text, operationErr = value, cliops.FormatTextResult(value), err
	}
	raw, err := json.Marshal(reply)
	if err != nil {
		return err
	}
	if !cliops.FitsOutput(reply, text) {
		return errors.New("experience: runtime reply facts exceed 32 KiB Shell output budget")
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

func readExperienceRecordInput(command cliops.ExperienceCommand, stdin io.Reader) (cliops.ExperienceRecord, error) {
	input := cliops.ExperienceRecord{From: command.From, To: command.To}
	var err error
	input.Summary, err = readBotPersonaText(cliops.BotCommand{Persona: command.Summary, PersonaFile: command.SummaryFile}, stdin)
	if err != nil {
		return input, fmt.Errorf("experience: summary input: %w", err)
	}
	input.Focus, err = readBotPersonaText(cliops.BotCommand{Persona: command.Focus, PersonaFile: command.FocusFile}, stdin)
	if err != nil {
		return input, fmt.Errorf("experience: focus input: %w", err)
	}
	wire, err := json.Marshal(input)
	if err != nil {
		return input, err
	}
	if len(wire) > cliops.ExperienceMaxRequest {
		return input, errors.New("experience: request exceeds 1 MiB after JSON encoding")
	}
	return input, nil
}

func readExperienceSearchInput(command cliops.ExperienceCommand, stdin io.Reader) (cliops.ExperienceSearch, error) {
	query, err := readBotPersonaText(cliops.BotCommand{Persona: command.Query, PersonaFile: command.QueryFile}, stdin)
	input := cliops.ExperienceSearch{Query: query, Limit: command.Limit}
	if err != nil {
		return input, fmt.Errorf("experience: query input: %w", err)
	}
	wire, err := json.Marshal(input)
	if err != nil {
		return input, err
	}
	if len(wire) > cliops.ExperienceMaxRequest {
		return input, errors.New("experience: request exceeds 1 MiB after JSON encoding")
	}
	return input, nil
}
