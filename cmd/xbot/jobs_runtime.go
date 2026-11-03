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

const jobsUsage = `Usage: xbot [--root DIR] jobs output --self <id> [--offset N] [--tail N] [--json]
  xbot [--root DIR] jobs status --self [<id>] [--json]
  xbot [--root DIR] jobs cancel --self <id> [--json]
  xbot jobs --help
  xbot jobs output|status|cancel --help
--self selects the active source through XBOT_ENDPOINT and canonical XBOT_ROOT;
XBOT_BOT is diagnostic only. The original shell job methods have no call ID.
output returns the original status/offset/next_offset text; a tail ignores
offset; status lists jobs or reports one job; cancel requests cancellation.
Received facts print before a failure exit; no automatic retry is added. Help
works offline; runtime calls require the active source endpoint.
`

func cmdJobsRuntime(rawRoot string, rootExplicit bool, args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runJobsRuntime(ctx, rawRoot, rootExplicit, args, os.Stdout)
}

func runJobsRuntime(ctx context.Context, rawRoot string, rootExplicit bool, args []string, out io.Writer) error {
	command, err := cliops.ParseJobsRuntime(args)
	if err != nil {
		return err
	}
	if command.Help {
		_, err := fmt.Fprint(out, jobsUsage)
		return err
	}
	root, endpoint, err := runtimeSource("jobs", rawRoot, rootExplicit)
	if err != nil {
		return err
	}
	var reply cliops.JobsReply
	var operationErr error
	switch command.Action {
	case "output":
		reply, operationErr = cliops.CallJobsOutput(ctx, endpoint, root, cliops.JobsOutput{ID: command.ID, Offset: command.Offset, Tail: command.Tail})
	case "status":
		reply, operationErr = cliops.CallJobsStatus(ctx, endpoint, root, cliops.JobsStatus{ID: command.ID})
	case "cancel":
		reply, operationErr = cliops.CallJobsCancel(ctx, endpoint, root, cliops.JobsCancel{ID: command.ID})
	default:
		return errors.New("jobs: expected output, status, or cancel")
	}
	if operationErr != nil && reply == (cliops.JobsReply{}) {
		return operationErr
	}
	raw, err := json.Marshal(reply)
	if err != nil {
		return err
	}
	text := cliops.FormatJobs(reply)
	if len(raw)+1 > tools.DefaultShellOutputBytes || len(text) > tools.DefaultShellOutputBytes {
		return errors.New("jobs: runtime reply facts exceed 32 KiB Shell output budget")
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
