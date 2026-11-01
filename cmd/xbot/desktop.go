package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"xbot/internal/desktop"
)

func cmdDesktop(root string, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: xbot desktop status|snapshot|screenshot|click|type|set-value|scroll|key")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var action desktop.Action
	switch args[0] {
	case "status":
		out, err := desktop.Status(ctx)
		if err != nil {
			return err
		}
		fmt.Println(out)
		return nil
	case "snapshot":
		if len(args) != 2 {
			return errors.New("usage: xbot desktop snapshot <app>")
		}
		action = desktop.Action{Name: "snapshot", App: args[1]}
	case "set-value":
		if len(args) != 3 {
			return errors.New("usage: xbot desktop set-value <ref> <text>")
		}
		action = desktop.Action{Name: "set_value", Ref: args[1], Text: args[2]}
	case "screenshot":
		if len(args) != 1 {
			return errors.New("usage: xbot desktop screenshot")
		}
		action.Name = "screenshot"
	case "click":
		if len(args) == 2 {
			action = desktop.Action{Name: "click", Ref: args[1]}
			break
		}
		if len(args) != 3 {
			return errors.New("usage: xbot desktop click <ref> (Mac/Windows) | <x> <y> (Linux)")
		}
		x, err1 := strconv.Atoi(args[1])
		y, err2 := strconv.Atoi(args[2])
		if err1 != nil || err2 != nil {
			return errors.New("usage: xbot desktop click <ref> (Mac/Windows) | <x> <y> (Linux)")
		}
		action = desktop.Action{Name: "click", X: x, Y: y}
	case "type":
		if len(args) < 2 {
			return errors.New("usage: xbot desktop type <ref> <text> (Mac/Windows) | <text> (Linux)")
		}
		action = desktop.Action{Name: "type", Text: strings.Join(args[1:], " ")}
		if strings.HasPrefix(args[1], "@") && len(args) >= 3 {
			action.Ref = args[1]
			action.Text = strings.Join(args[2:], " ")
		}
	case "scroll":
		ref := ""
		if len(args) == 3 {
			ref = args[1]
			args = []string{args[0], args[2]}
		}
		if len(args) != 2 {
			return errors.New("usage: xbot desktop scroll [ref] <dy>")
		}
		dy, err := strconv.Atoi(args[1])
		if err != nil {
			return errors.New("usage: xbot desktop scroll [ref] <dy>")
		}
		action = desktop.Action{Name: "scroll", Ref: ref, Dy: dy}
	case "key":
		app := ""
		if len(args) == 3 {
			app = args[1]
			args = []string{args[0], args[2]}
		}
		if len(args) != 2 {
			return errors.New("usage: xbot desktop key [app] <key>")
		}
		action = desktop.Action{Name: "key", App: app, Key: args[1]}
	default:
		return fmt.Errorf("unknown desktop command %q", args[0])
	}
	text, err := desktop.Perform(ctx, root, nil, action, true)
	if err != nil {
		return err
	}
	fmt.Println(text)
	return nil
}
