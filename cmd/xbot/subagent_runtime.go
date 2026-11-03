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

const subagentUsage = `Usage: xbot [--root DIR] subagent list --self [--limit N] [--json]
  xbot [--root DIR] subagent read --self <id> [--json]
  xbot [--root DIR] subagent expand --self <id> [--from N] [--to N] [--json]
  xbot subagent --help
  xbot subagent list|read|expand --help
--self selects the active source through XBOT_ENDPOINT and canonical XBOT_ROOT;
XBOT_BOT is diagnostic only. The original subagent read methods have no call
ID. list returns the original items JSON array with live task status overlaid;
read returns the original five-line status and summary; expand returns the
original untrusted transcript range, 1-based inclusive, capped at 8 KiB.
Omitted --from/--to clamp to the full recorded range. Received facts print
before a failure exit; no automatic retry is added. Help works offline;
runtime calls require the active source endpoint.
`

func cmdSubagentRuntime(rawRoot string, rootExplicit bool, args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runSubagentRuntime(ctx, rawRoot, rootExplicit, args, os.Stdout)
}

func runSubagentRuntime(ctx context.Context, rawRoot string, rootExplicit bool, args []string, out io.Writer) error {
	command, err := cliops.ParseSubagentRuntime(args)
	if err != nil {
		return err
	}
	if command.Help {
		_, err := fmt.Fprint(out, subagentUsage)
		return err
	}
	root := os.Getenv("XBOT_ROOT")
	if strings.TrimSpace(root) == "" {
		return errors.New("subagent: --self requires XBOT_ROOT from an active runtime")
	}
	root, err = cliops.CanonicalRoot(root)
	if err != nil {
		return fmt.Errorf("subagent: runtime root: %w", err)
	}
	if rootExplicit {
		if strings.TrimSpace(rawRoot) == "" {
			return errors.New("subagent: explicit --root must name the runtime data root")
		}
		given, err := cliops.CanonicalRoot(rawRoot)
		if err != nil {
			return fmt.Errorf("subagent: explicit root: %w", err)
		}
		if given != root {
			return errors.New("subagent: runtime data root mismatch")
		}
	}
	endpoint := os.Getenv("XBOT_ENDPOINT")
	if endpoint == "" {
		return errors.New("subagent: --self requires an active local XBot endpoint")
	}
	var reply cliops.SubagentReply
	var operationErr error
	switch command.Action {
	case "list":
		reply, operationErr = cliops.CallSubagentList(ctx, endpoint, root, cliops.SubagentList{Limit: command.Limit})
	case "read":
		reply, operationErr = cliops.CallSubagentRead(ctx, endpoint, root, cliops.SubagentRead{ID: command.ID})
	case "expand":
		reply, operationErr = cliops.CallSubagentExpand(ctx, endpoint, root, cliops.SubagentExpand{ID: command.ID, From: command.From, To: command.To})
	default:
		return errors.New("subagent: expected list, read, or expand")
	}
	if operationErr != nil && reply == (cliops.SubagentReply{}) {
		return operationErr
	}
	raw, err := json.Marshal(reply)
	if err != nil {
		return err
	}
	text := cliops.FormatSubagent(reply)
	if len(raw)+1 > tools.DefaultShellOutputBytes || len(text) > tools.DefaultShellOutputBytes {
		return errors.New("subagent: runtime reply facts exceed 32 KiB Shell output budget")
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
