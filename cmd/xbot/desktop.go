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
		return errors.New("usage: xbot desktop screenshot|click|type|scroll|key")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var action desktop.Action
	switch args[0] {
	case "screenshot":
		if len(args) != 1 {
			return errors.New("usage: xbot desktop screenshot")
		}
		action.Name = "screenshot"
	case "click":
		if len(args) != 3 {
			return errors.New("usage: xbot desktop click <x> <y>")
		}
		x, err1 := strconv.Atoi(args[1])
		y, err2 := strconv.Atoi(args[2])
		if err1 != nil || err2 != nil {
			return errors.New("usage: xbot desktop click <x> <y>")
		}
		action = desktop.Action{Name: "click", X: x, Y: y}
	case "type":
		if len(args) < 2 {
			return errors.New("usage: xbot desktop type <text>")
		}
		action = desktop.Action{Name: "type", Text: strings.Join(args[1:], " ")}
	case "scroll":
		if len(args) != 2 {
			return errors.New("usage: xbot desktop scroll <dy>")
		}
		dy, err := strconv.Atoi(args[1])
		if err != nil {
			return errors.New("usage: xbot desktop scroll <dy>")
		}
		action = desktop.Action{Name: "scroll", Dy: dy}
	case "key":
		if len(args) != 2 {
			return errors.New("usage: xbot desktop key <key>")
		}
		action = desktop.Action{Name: "key", Key: args[1]}
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
