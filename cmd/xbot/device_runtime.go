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
	"syscall"

	"xbot/internal/cliops"
	"xbot/internal/deviceexec"
	"xbot/internal/tools"
)

const deviceUsage = `Usage: xbot [--root DIR] device ACTION --self [--device ID] [--json]
  device list --self
  device exec --self --args '{"command":"pwd"}' [--tool sh] [--description TEXT] [--ref REF]
  device read --self --args '{"path":"file"}' [--ref REF]
  device write --self --args '{"path":"file","content":"text"}' [--ref REF]
  device desktop|browser --self --tool NAME --args '{}' [--description TEXT] [--ref REF]
  device tasks --self [--offset N] [--limit N]
  device task REF --self [--offset N] [--limit N]
  device stop REF --self
  device artifact REF --self --artifact-id ID [--offset N] [--limit N]
  device --help
Calls also accept --args-file FILE|- instead of --args. Args must be one JSON object.
--self requires the original active prepared Shell call. The runtime supplies Bot,
main/subagent context and readonly policy. --device defaults to the message's frozen
selection. Offline or unavailable devices fail without a local fallback.
Calls receive stable references before sending. Unknown results must be queried
with task REF; do not automatically repeat exec, write or desktop actions.
Results are bounded JSON facts. Use task/artifact offsets for more output.
`

func cmdDeviceRuntime(rawRoot string, explicit bool, args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runDeviceRuntime(ctx, rawRoot, explicit, args, os.Stdin, os.Stdout)
}

func runDeviceRuntime(ctx context.Context, rawRoot string, explicit bool, args []string, stdin io.Reader, out io.Writer) error {
	c, err := cliops.ParseDeviceRuntime(args)
	if err != nil {
		return err
	}
	if c.Help {
		_, err = fmt.Fprint(out, deviceUsage)
		return err
	}
	root, endpoint, err := runtimeSource("device", rawRoot, explicit)
	if err != nil {
		return err
	}
	callID := os.Getenv("XBOT_CALL_ID")
	if callID == "" {
		return errors.New("device: requires XBOT_CALL_ID from an active XBot shell")
	}
	in := cliops.DeviceRequest{Action: c.Action, DeviceID: c.DeviceID, Ref: c.Ref, Offset: c.Offset, Limit: c.Limit, ArtifactID: c.ArtifactID}
	if in.Limit == 0 && (c.Action == "task" || c.Action == "artifact") {
		in.Limit = 4 << 10
	}
	switch c.Action {
	case "exec", "read", "write", "desktop", "browser":
		text, err := readBotPersonaText(cliops.BotCommand{Persona: c.Args, PersonaFile: c.ArgsFile}, stdin)
		if err != nil {
			return fmt.Errorf("device: args: %w", err)
		}
		raw := bytes.TrimSpace([]byte(text))
		if len(raw) == 0 || raw[0] != '{' || !json.Valid(raw) {
			return errors.New("device: args must be one JSON object")
		}
		if in.Ref == "" {
			in.Ref, err = deviceexec.NewRef()
			if err != nil {
				return err
			}
		}
		in.Call = deviceexec.Call{Ref: in.Ref, Kind: c.Action, Tool: c.Tool, Description: c.Description, Args: raw}
	}
	if err := in.Validate(); err != nil {
		return err
	}
	reply, problem := cliops.CallDevice(ctx, endpoint, root, callID, in)
	if problem != nil && in.Call.Ref != "" {
		problem = fmt.Errorf("device: ref %s; query device task %s --self before any retry: %w", in.Ref, in.Ref, problem)
	}
	if len(reply) == 0 {
		return problem
	}
	// Compact JSON keeps the exact callback fields and fits the Shell budget.
	var compact bytes.Buffer
	if err := json.Compact(&compact, reply); err != nil {
		return err
	}
	if compact.Len()+1 > tools.DefaultShellOutputBytes {
		return errors.New("device: response exceeds 32 KiB; use a smaller --limit")
	}
	if _, err := out.Write(append(compact.Bytes(), '\n')); err != nil {
		return err
	}
	return problem
}
