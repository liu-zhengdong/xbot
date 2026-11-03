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

const linksUsage = `Usage: xbot [--root DIR] links search --self [--query TEXT | --query-file FILE|-] [--json]
  xbot [--root DIR] links load --self <id|name> [--json]
  xbot links --help
  xbot links search|load --help
--self selects the active source through XBOT_ENDPOINT and canonical XBOT_ROOT;
XBOT_CALL_ID supplies the original active Shell context. XBOT_BOT is diagnostic
only. search prints the original TSV catalog rows, or "no links"; an empty
query lists the whole catalog. load prints the original "loaded id: tools…"
acknowledgement; mcp: ids and store connectors still point at mcp tools.
--query-file - reads stdin. Raw query and escaped JSON each allow 1 MiB.
Received facts print before a failure exit; no automatic retry is added.
Help works offline; runtime calls require the active source endpoint and the
original Shell call ID. Install and remove stay on the links tool owner card.
`

func cmdLinksRuntime(rawRoot string, rootExplicit bool, args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runLinksRuntime(ctx, rawRoot, rootExplicit, args, os.Stdin, os.Stdout)
}

func runLinksRuntime(ctx context.Context, rawRoot string, rootExplicit bool, args []string, stdin io.Reader, out io.Writer) error {
	command, err := cliops.ParseLinksRuntime(args)
	if err != nil {
		return err
	}
	if command.Help {
		_, err := fmt.Fprint(out, linksUsage)
		return err
	}
	root := os.Getenv("XBOT_ROOT")
	if strings.TrimSpace(root) == "" {
		return errors.New("links: --self requires XBOT_ROOT from an active runtime")
	}
	root, err = cliops.CanonicalRoot(root)
	if err != nil {
		return fmt.Errorf("links: runtime root: %w", err)
	}
	if rootExplicit {
		if strings.TrimSpace(rawRoot) == "" {
			return errors.New("links: explicit --root must name the runtime data root")
		}
		given, err := cliops.CanonicalRoot(rawRoot)
		if err != nil {
			return fmt.Errorf("links: explicit root: %w", err)
		}
		if given != root {
			return errors.New("links: runtime data root mismatch")
		}
	}
	endpoint := os.Getenv("XBOT_ENDPOINT")
	if endpoint == "" {
		return errors.New("links: --self requires an active local XBot endpoint")
	}
	callID := os.Getenv("XBOT_CALL_ID")
	if callID == "" {
		return errors.New("links: requires XBOT_CALL_ID from an active XBot shell")
	}
	var reply cliops.LinksReply
	var operationErr error
	switch command.Action {
	case "search":
		query, err := readBotPersonaText(cliops.BotCommand{Persona: command.Query, PersonaFile: command.QueryFile}, stdin)
		if err != nil {
			return fmt.Errorf("links: query input: %w", err)
		}
		input := cliops.LinksSearch{Query: query}
		wire, err := json.Marshal(input)
		if err != nil {
			return err
		}
		if len(wire) > cliops.LinksMaxRequest {
			return errors.New("links: request exceeds 1 MiB after JSON encoding")
		}
		reply, operationErr = cliops.CallLinksSearch(ctx, endpoint, root, callID, input)
	case "load":
		reply, operationErr = cliops.CallLinksLoad(ctx, endpoint, root, callID, cliops.LinksLoad{ID: command.ID})
	default:
		return errors.New("links: expected search or load")
	}
	if operationErr != nil && reply == (cliops.LinksReply{}) {
		return operationErr
	}
	raw, err := json.Marshal(reply)
	if err != nil {
		return err
	}
	text := cliops.FormatLinks(reply)
	if len(raw)+1 > tools.DefaultShellOutputBytes || len(text) > tools.DefaultShellOutputBytes {
		return errors.New("links: runtime reply facts exceed 32 KiB Shell output budget")
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
