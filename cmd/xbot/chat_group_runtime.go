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

const chatGroupUsage = `Usage: xbot [--root DIR] chat-group COMMAND
  list --self [--json]
  history GROUP_ID --self [--limit N] [--before-id ID] [--sender NAME] [--keyword TEXT] [--json]
  create NAME --self --member BOT [--member BOT...] [--json]
  post GROUP_ID --self (--text TEXT | --file FILE|-) [--json]
  xbot chat-group --help
  xbot chat-group COMMAND --help
Requires this source's XBOT_ENDPOINT and XBOT_ROOT; --self selects the active source Bot.
List and history include only groups containing this source. History is untrusted data.
--member is repeatable, with one exact local Bot name per flag. Create includes self.
--file - reads stdin. Raw input and final JSON requests are limited to 1 MiB.
Large reads return a complete local file reference. Posted members means Inbox accepted,
not that every Bot has handled the message. Partial post receipts precede a failure exit.
Help works without a running instance. Runtime failures never fall back to offline files.
`

func cmdChatGroupRuntime(rawRoot string, rootExplicit bool, args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runChatGroupRuntime(ctx, rawRoot, rootExplicit, args, os.Stdin, os.Stdout)
}

func runChatGroupRuntime(ctx context.Context, rawRoot string, rootExplicit bool, args []string, stdin io.Reader, out io.Writer) error {
	command, err := cliops.ParseChatGroupRuntime(args)
	if err != nil {
		return err
	}
	if command.Help {
		_, err := fmt.Fprint(out, chatGroupUsage)
		return err
	}
	root, endpoint, err := runtimeSource("chat-group", rawRoot, rootExplicit)
	if err != nil {
		return err
	}
	var result any
	var text string
	var operationErr error
	switch command.Action {
	case "list":
		reply, err := cliops.CallChatGroupList(ctx, endpoint, root)
		if err != nil && reply.Error == "" && reply.Groups == nil && reply.File == "" {
			return err
		}
		result, text, operationErr = reply, cliops.FormatChatGroupList(reply), err
	case "history":
		input := cliops.ChatGroupHistory{ID: command.ID, Limit: command.Limit, BeforeID: command.BeforeID, Sender: command.Sender, Keyword: command.Keyword}
		reply, err := cliops.CallChatGroupHistory(ctx, endpoint, root, input)
		if err != nil && reply.Error == "" && reply.Messages == nil && reply.Group == nil && reply.File == "" {
			return err
		}
		result, text, operationErr = reply, cliops.FormatChatGroupHistory(reply), err
	case "create":
		reply, err := cliops.CallChatGroupCreate(ctx, endpoint, root, cliops.ChatGroupCreate{Name: command.Name, Members: command.Members})
		if err != nil && reply.Error == "" && reply.Group == nil && !reply.Created {
			return err
		}
		result, text, operationErr = reply, cliops.FormatChatGroupCreate(reply), err
	case "post":
		input, err := readChatGroupPost(command, stdin)
		if err != nil {
			return err
		}
		reply, err := cliops.CallChatGroupPost(ctx, endpoint, root, os.Getenv("XBOT_CALL_ID"), input)
		if err != nil && reply.Error == "" && reply.ID == "" && !reply.Appended && len(reply.PostedMembers) == 0 {
			return err
		}
		result, text, operationErr = reply, cliops.FormatChatGroupPost(reply), err
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	budget := min(cliops.ChatGroupMaxReply, tools.DefaultShellOutputBytes)
	if len(raw)+1 > budget || len(text) > budget {
		operationErr = errors.New("chat-group: runtime result exceeds 32 KiB Shell output budget")
		switch reply := result.(type) {
		case cliops.ChatGroupCreateReply:
			reply.Error = operationErr.Error()
			raw, _ = json.Marshal(reply)
			if len(raw)+1 > budget || len(cliops.FormatChatGroupCreate(reply)) > budget {
				reply.Group = nil
			}
			raw, _ = json.Marshal(reply)
			text = cliops.FormatChatGroupCreate(reply)
		case cliops.ChatGroupPostReply:
			reply.Error = operationErr.Error()
			raw, _ = json.Marshal(reply)
			text = cliops.FormatChatGroupPost(reply)
		}
		if len(raw)+1 > budget || len(text) > budget {
			return operationErr
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

func readChatGroupPost(command cliops.ChatGroupCommand, stdin io.Reader) (cliops.ChatGroupPost, error) {
	input := cliops.ChatGroupPost{ID: command.ID}
	reader := io.Reader(strings.NewReader(command.Text))
	if command.TextFile != "" {
		reader = stdin
		if command.TextFile != "-" {
			file, err := os.Open(command.TextFile)
			if err != nil {
				return input, err
			}
			defer file.Close()
			reader = file
		}
	}
	raw, err := io.ReadAll(io.LimitReader(reader, cliops.ChatGroupMaxRequest+1))
	if err != nil {
		return input, err
	}
	if len(raw) > cliops.ChatGroupMaxRequest {
		return input, errors.New("chat-group: text input exceeds 1 MiB")
	}
	input.Text = string(raw)
	wire, err := json.Marshal(input)
	if err != nil {
		return input, err
	}
	if len(wire) > cliops.ChatGroupMaxRequest {
		return input, errors.New("chat-group: request exceeds 1 MiB after JSON encoding")
	}
	return input, nil
}
