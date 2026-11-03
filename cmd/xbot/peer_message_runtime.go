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

const peerMessageUsage = `Usage: xbot [--root DIR] peer-message TARGET --self (--text TEXT | --file FILE|-) [--json]
  xbot peer-message --help
TARGET is a local Bot or bot@paired-device. --self selects the active source Bot
through XBOT_ENDPOINT and canonical XBOT_ROOT; XBOT_BOT is diagnostic only.
--file - reads stdin. Raw input and the final escaped JSON request each allow 1 MiB.
The original source validates text and target policy. Local posted means Inbox
accepted. Gateway accepted means HTTP 2xx and a successfully read response, not
recipient handling. Queued means durably pending at source, with 24h expiry.
An unknown gateway outcome may already have been accepted. Receipts print before
a failure exit; do not blindly retry a failed call. No automatic retry is added.
Help works offline. Runtime failures require the active source endpoint.
`

func cmdPeerMessageRuntime(rawRoot string, rootExplicit bool, args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runPeerMessageRuntime(ctx, rawRoot, rootExplicit, args, os.Stdin, os.Stdout)
}

func runPeerMessageRuntime(ctx context.Context, rawRoot string, rootExplicit bool, args []string, stdin io.Reader, out io.Writer) error {
	command, err := cliops.ParsePeerMessageRuntime(args)
	if err != nil {
		return err
	}
	if command.Help {
		_, err := fmt.Fprint(out, peerMessageUsage)
		return err
	}
	root, endpoint, err := runtimeSource("peer-message", rawRoot, rootExplicit)
	if err != nil {
		return err
	}
	input, err := readPeerMessageInput(command, stdin)
	if err != nil {
		return err
	}
	reply, operationErr := cliops.CallPeerMessage(ctx, endpoint, root, os.Getenv("XBOT_CALL_ID"), input)
	if operationErr != nil && reply.Error == "" && reply.Stage == "" && !reply.Posted && reply.EventID == "" && reply.EnvelopeID == "" && reply.Gateway == "" && !reply.Queued && reply.Recorded == nil && reply.RecordSeq == 0 {
		return operationErr
	}
	raw, err := json.Marshal(reply)
	if err != nil {
		return err
	}
	text := cliops.FormatPeerMessage(reply)
	if len(raw)+1 > tools.DefaultShellOutputBytes || len(text) > tools.DefaultShellOutputBytes {
		return errors.New("peer-message: runtime receipt facts exceed 32 KiB Shell output budget")
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

func readPeerMessageInput(command cliops.PeerMessageCommand, stdin io.Reader) (cliops.PeerMessage, error) {
	// Reuse the existing complete, bounded file/stdin reader; only the final
	// request shape changes. No text trimming or domain limits belong here.
	text, err := readBotPersonaText(cliops.BotCommand{Persona: command.Text, PersonaFile: command.TextFile}, stdin)
	input := cliops.PeerMessage{Bot: command.Bot, Text: text}
	if err != nil {
		return input, fmt.Errorf("peer-message: text input: %w", err)
	}
	wire, err := json.Marshal(input)
	if err != nil {
		return input, err
	}
	if len(wire) > cliops.PeerMessageMaxRequest {
		return input, errors.New("peer-message: request exceeds 1 MiB after JSON encoding")
	}
	return input, nil
}
