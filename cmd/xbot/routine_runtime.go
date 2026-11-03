package main

import (
	"bytes"
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

const routineUsage = `Usage: xbot [--root DIR] routine ACTION
Runtime commands (require this source's XBOT_ENDPOINT and XBOT_ROOT):
  list --self [--json]
  create --self (--args JSON_OBJECT | --args-file FILE|-) [--json]
  update ID --self (--args JSON_OBJECT | --args-file FILE|-) [--json]
  delete ID --self [--json]
Create fields: id, name, prompt, cron, at, precheck, skills.
Update fields: name, prompt, cron, enabled, precheck, skills.
--args-file - reads stdin. Input and final JSON requests are limited to 1 MiB.
Results are complete bounded JSON or a local file reference for a large list.
An applied write means the definition changed; it does not mean the routine fired.
Precheck changes require the original active source Shell approval context.
Existing offline commands (files/configuration only):
  migrate [--dry-run] [--bot NAME]
  list --bot NAME
  update ID --bot NAME --skills id1,id2
Help works without a running instance. Runtime failures never fall back to offline files.
`

// routineUsesRuntime only distinguishes the existing offline forms from the
// runtime parser. It never consults an endpoint or reads an argument file.
func routineUsesRuntime(args []string) bool {
	if len(args) == 0 {
		return true
	}
	switch args[0] {
	case "migrate", "list", "update":
	default:
		return true
	}
	for i := 1; i < len(args); i++ {
		flag, _, eq := strings.Cut(args[i], "=")
		switch flag {
		case "--self", "--json", "--help", "-h", "--args", "--args-file":
			return true
		case "--bot", "--skills":
			if !eq {
				i++ // an offline value may literally be --self or --help
			}
		}
	}
	return false
}

func cmdRoutineRuntime(rawRoot string, rootExplicit bool, args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runRoutineRuntime(ctx, rawRoot, rootExplicit, args, os.Stdin, os.Stdout)
}

func runRoutineRuntime(ctx context.Context, rawRoot string, rootExplicit bool, args []string, stdin io.Reader, out io.Writer) error {
	command, err := cliops.ParseRoutineRuntime(args)
	if err != nil {
		return err
	}
	if command.Help {
		_, err := fmt.Fprint(out, routineUsage)
		return err
	}
	root, endpoint, err := runtimeSource("routine", rawRoot, rootExplicit)
	if err != nil {
		return err
	}
	var result any
	var text bytes.Buffer
	var operationErr error
	if command.Action == "list" {
		reply, err := cliops.CallRoutineList(ctx, endpoint, root)
		if err != nil && reply.Error == "" && reply.Routines == nil && reply.File == "" {
			return err
		}
		result, operationErr = reply, err
		if !command.JSON {
			text.WriteString(cliops.FormatRoutineList(reply))
		}
	} else {
		var reply cliops.RoutineWriteReply
		callID := os.Getenv("XBOT_CALL_ID")
		switch command.Action {
		case "create":
			input, err := readRoutineCreateInput(command, stdin)
			if err != nil {
				return err
			}
			reply, operationErr = cliops.CallRoutineCreate(ctx, endpoint, root, callID, input)
		case "update":
			input, err := readRoutinePatchInput(command, stdin)
			if err != nil {
				return err
			}
			reply, operationErr = cliops.CallRoutineUpdate(ctx, endpoint, root, callID, command.ID, input)
		case "delete":
			reply, operationErr = cliops.CallRoutineDelete(ctx, endpoint, root, callID, command.ID)
		}
		if operationErr != nil && reply.ID == "" && !reply.Applied && reply.Error == "" {
			return operationErr
		}
		result = reply
		if !command.JSON {
			fmt.Fprintf(&text, "id: %s\napplied: %t\nstage: %s\n", reply.ID, reply.Applied, reply.Stage)
			if reply.Error != "" {
				fmt.Fprintf(&text, "error: %s\n", reply.Error)
			}
		}
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	if len(raw)+1 > cliops.RoutineMaxReply {
		return errors.New("routine: runtime result exceeds 64 KiB JSON output budget")
	}
	if command.JSON {
		raw = append(raw, '\n')
	} else {
		raw = text.Bytes()
		if command.Action == "list" && len(raw) > min(cliops.RoutineMaxReply, tools.DefaultShellOutputBytes) {
			return errors.New("routine: human list exceeds Shell output budget")
		}
		if len(raw) > cliops.RoutineMaxReply {
			return errors.New("routine: runtime result exceeds 64 KiB output budget")
		}
	}
	if _, err := out.Write(raw); err != nil {
		return err
	}
	return operationErr
}
