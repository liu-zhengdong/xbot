package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"xbot/internal/browser"
)

func cmdBrowser(root string, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: xbot browser open|snapshot|click|stop")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	switch args[0] {
	case "open":
		return cmdBrowserOpen(ctx, root, args[1:])
	case "snapshot":
		if len(args) != 1 {
			return errors.New("usage: xbot browser snapshot")
		}
		sess, err := browser.AttachSaved(ctx, root)
		if err != nil {
			return err
		}
		defer sess.Release()
		text, err := sess.Snapshot(ctx)
		if err != nil {
			return err
		}
		fmt.Println(text)
		return nil
	case "click":
		if len(args) != 2 {
			return errors.New("usage: xbot browser click <selector>")
		}
		sess, err := browser.AttachSaved(ctx, root)
		if err != nil {
			return err
		}
		defer sess.Release()
		text, err := sess.Click(ctx, args[1])
		if err != nil {
			return err
		}
		fmt.Println(text)
		return nil
	case "stop":
		if len(args) != 1 {
			return errors.New("usage: xbot browser stop")
		}
		note, err := browser.Stop(root)
		if err != nil {
			return err
		}
		if note != "" {
			fmt.Fprintln(os.Stderr, note)
		}
		return nil
	default:
		return fmt.Errorf("unknown browser command %q", args[0])
	}
}

func cmdBrowserOpen(ctx context.Context, root string, args []string) error {
	headless := false
	var rest []string
	for _, a := range args {
		if a == "--headless" {
			headless = true
			continue
		}
		rest = append(rest, a)
	}
	if len(rest) != 1 {
		return errors.New("usage: xbot browser open [--headless] <url>")
	}
	sess, err := browser.Launch(ctx, root, rest[0], headless)
	if err != nil {
		return err
	}
	text, err := sess.Snapshot(ctx)
	sess.Release()
	if err != nil {
		return err
	}
	fmt.Println(strings.TrimSpace(text))
	return nil
}
