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
)

const skillUsage = `Usage: xbot [--root DIR] skill ACTION
Runtime commands (require this source's XBOT_ENDPOINT and XBOT_ROOT):
  read ID --self [--json]
  write SLUG --description TEXT [--title TEXT] (--body TEXT | --body-file FILE|-) [--scope NAME] --self [--json]
--self locates the source Bot; it does not set the write scope.
Omitted or empty scope enables all local Bots; self selects the current Bot.
Any other scope, including global, is a real Bot name (matched without case).
An explicit empty body is valid. --body-file - reads stdin. Writes are limited to 1 MiB of final JSON.
Read results contain complete body text or a bounded preview and a local file reference.
Existing offline commands (files/configuration only):
  list
  show SLUG
  import PATH
  remove SLUG
  enable BOT SLUG
  disable BOT SLUG
Help works without a running instance. Runtime failures never fall back to offline files.
`

func cmdSkillRuntime(rawRoot string, rootExplicit bool, args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runSkillRuntime(ctx, rawRoot, rootExplicit, args, os.Stdin, os.Stdout)
}

func runSkillRuntime(ctx context.Context, rawRoot string, rootExplicit bool, args []string, in io.Reader, out io.Writer) error {
	command, err := cliops.ParseSkillRuntime(args)
	if err != nil {
		return err
	}
	if command.Help {
		_, err := fmt.Fprint(out, skillUsage)
		return err
	}
	root := os.Getenv("XBOT_ROOT")
	if strings.TrimSpace(root) == "" {
		return errors.New("skill: --self requires XBOT_ROOT from an active runtime")
	}
	root, err = cliops.CanonicalRoot(root)
	if err != nil {
		return fmt.Errorf("skill: runtime root: %w", err)
	}
	if rootExplicit {
		if strings.TrimSpace(rawRoot) == "" {
			return errors.New("skill: explicit --root must name the runtime data root")
		}
		given, err := cliops.CanonicalRoot(rawRoot)
		if err != nil {
			return fmt.Errorf("skill: explicit root: %w", err)
		}
		if given != root {
			return errors.New("skill: runtime data root mismatch")
		}
	}
	endpoint := os.Getenv("XBOT_ENDPOINT")
	if endpoint == "" {
		return errors.New("skill: --self requires an active local XBot endpoint")
	}
	var value any
	var text bytes.Buffer
	var operationErr error
	switch command.Action {
	case "read":
		reply, err := cliops.CallSkillRead(ctx, endpoint, root, command.ID)
		if err != nil && reply.Slug == "" && reply.Body == nil && reply.Error == "" {
			return err
		}
		value, operationErr = reply, err
		if !command.JSON {
			if reply.Body != nil {
				text.WriteString(*reply.Body)
			} else {
				if reply.Preview != "" {
					fmt.Fprintln(&text, reply.Preview)
				}
				if reply.File != "" {
					fmt.Fprintf(&text, "%s (access: %s, bytes: %d)\n", reply.File, reply.Access, reply.Bytes)
				}
			}
			if reply.Error != "" {
				fmt.Fprintf(&text, "error: %s\n", reply.Error)
			}
		}
	case "write":
		body := command.Body
		if command.BodyFile != "" {
			reader := in
			if command.BodyFile != "-" {
				file, err := os.Open(command.BodyFile)
				if err != nil {
					return err
				}
				defer file.Close()
				reader = file
			}
			raw, err := io.ReadAll(io.LimitReader(reader, cliops.SkillMaxRequest+1))
			if err != nil {
				return err
			}
			if len(raw) > cliops.SkillMaxRequest {
				return errors.New("skill: body exceeds 1 MiB")
			}
			content := string(raw)
			body = &content
		}
		update := cliops.SkillWrite{Slug: command.Slug, Title: command.Title, Description: command.Description, Scope: command.Scope, Body: body}
		raw, err := json.Marshal(update)
		if err != nil {
			return err
		}
		if len(raw) > cliops.SkillMaxRequest {
			return errors.New("skill: request exceeds 1 MiB after JSON encoding")
		}
		reply, err := cliops.CallSkillWrite(ctx, endpoint, root, update)
		if err != nil && !reply.Written && len(reply.UpdatedBots) == 0 && reply.Slug == "" && reply.Error == "" {
			return err
		}
		value, operationErr = reply, err
		if !command.JSON {
			fmt.Fprintf(&text, "slug: %s\nwritten: %t\nscope: %s\ntarget: %s\nupdated_bots: %s\n", reply.Slug, reply.Written, reply.Scope, reply.Target, strings.Join(reply.UpdatedBots, ", "))
			if reply.Error != "" {
				fmt.Fprintf(&text, "error: %s\n", reply.Error)
			}
		}
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(raw)+1 > cliops.SkillMaxReply {
		return errors.New("skill: runtime result exceeds 64 KiB JSON output budget")
	}
	if command.JSON {
		raw = append(raw, '\n')
	} else {
		raw = text.Bytes()
		if len(raw) > cliops.SkillMaxReply {
			return errors.New("skill: runtime result exceeds 64 KiB output budget")
		}
	}
	if _, err := out.Write(raw); err != nil {
		return err
	}
	return operationErr
}
