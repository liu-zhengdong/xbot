package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"xbot/internal/node"
	"xbot/internal/nodesync"
)

func cmdNode(root string, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: xbot node serve|add|list|remove|call|pending|approve")
	}
	switch args[0] {
	case "serve":
		return cmdNodeServe(root, args[1:])
	case "add":
		return cmdNodeAdd(root, args[1:])
	case "list":
		return cmdNodeList(root, args[1:])
	case "remove":
		return cmdNodeRemove(root, args[1:])
	case "call":
		return cmdNodeCall(root, args[1:])
	case "pending":
		return cmdNodePending(root, args[1:])
	case "approve":
		return cmdNodeApprove(root, args[1:])
	default:
		return fmt.Errorf("unknown node command %q", args[0])
	}
}

func cmdNodeServe(root string, args []string) error {
	ordered, err := reorderFlags(args, map[string]bool{"addr": true, "token": true, "name": true})
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("node serve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	addr := fs.String("addr", "127.0.0.1:18772", "listen address")
	token := fs.String("token", "", "bearer token")
	name := fs.String("name", "", "name announced to callers")
	if err := fs.Parse(ordered); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: xbot node serve [--addr 127.0.0.1:18772] [--token TOKEN] [--name NAME]")
	}
	provided, err := node.ResolveToken(*token, os.Getenv("XBOT_NODE_TOKEN"), os.Stdin)
	if err != nil {
		return err
	}
	tok, created, err := node.EnsureToken(root, provided)
	if err != nil {
		return err
	}
	info, err := node.Identity(root, *name)
	if err != nil {
		return err
	}
	if created {
		fmt.Printf("token: %s\n", tok)
	}
	fmt.Fprintf(os.Stderr, "node %s listening on http://%s\n", info.Name, *addr)
	ctx, stop := signal.NotifyContext(context.Background(), stopSignals()...)
	defer stop()
	nodesync.Start(ctx, root, nodesync.DefaultEvery, nodesync.DefaultDebounce)
	return node.Serve(ctx, *addr, node.Handler(tok, info, nodeDispatch(root, info)))
}

func cmdNodeAdd(root string, args []string) error {
	ordered, err := reorderFlags(args, map[string]bool{"url": true, "token": true})
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("node add", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	rawURL := fs.String("url", "", "base URL of xbot node serve")
	token := fs.String("token", "", "bearer token")
	if err := fs.Parse(ordered); err != nil {
		return err
	}
	if fs.NArg() != 1 || strings.TrimSpace(*rawURL) == "" {
		return errors.New("usage: xbot node add <name> --url URL [--token TOKEN]")
	}
	tok, err := node.ResolveToken(*token, os.Getenv("XBOT_NODE_TOKEN"), os.Stdin)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := node.Ping(ctx, *rawURL, tok); err != nil {
		return err
	}
	if err := node.Add(root, node.Node{Name: fs.Arg(0), URL: *rawURL, Token: tok}); err != nil {
		return err
	}
	fmt.Printf("added %s\n", fs.Arg(0))
	return nil
}

func cmdNodeList(root string, args []string) error {
	if len(args) != 0 {
		return errors.New("usage: xbot node list")
	}
	nodes, err := node.List(root)
	if err != nil {
		return err
	}
	if len(nodes) == 0 {
		fmt.Println("no nodes")
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for _, n := range nodes {
		state := "offline"
		info, err := node.Ping(ctx, n.URL, n.Token)
		if err == nil {
			state = "online " + info.Name
		} else if errors.Is(err, node.ErrUnauthorized) {
			state = "unauthorized"
		}
		fmt.Printf("%s\t%s\t%s\n", n.Name, state, n.URL)
	}
	return nil
}

func cmdNodeRemove(root string, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: xbot node remove <name>")
	}
	if err := node.Remove(root, args[0]); err != nil {
		return err
	}
	fmt.Printf("removed %s\n", args[0])
	return nil
}

func cmdNodeCall(root string, args []string) error {
	if len(args) < 2 {
		return errors.New("usage: xbot node call <name> info | xbot node call <name> shell <command>")
	}
	n, err := node.Lookup(root, args[0])
	if err != nil {
		return err
	}
	command := strings.Join(args[2:], " ")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	text, err := node.Call(ctx, n.URL, n.Token, args[1], command, false)
	if err != nil {
		return err
	}
	fmt.Println(text)
	return nil
}

func cmdNodePending(root string, args []string) error {
	if len(args) != 0 {
		return errors.New("usage: xbot node pending")
	}
	items, err := node.ListPending(root)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		fmt.Println("no pending")
		return nil
	}
	for _, item := range items {
		fmt.Printf("%s\t%s\t%s\t%s\n", item.ID, item.Caller, oneLine(item.Command), item.Time.UTC().Format(time.RFC3339))
	}
	return nil
}

func cmdNodeApprove(root string, args []string) error {
	ordered, err := reorderFlags(args, map[string]bool{})
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("node approve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	yes := fs.Bool("yes", false, "run without asking")
	if err := fs.Parse(ordered); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: xbot node approve [--yes] <id>")
	}
	item, err := node.LookupPending(root, fs.Arg(0))
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "id: %s\ncaller: %s\ntime: %s\ncommand: %s\n", item.ID, item.Caller, item.Time.UTC().Format(time.RFC3339), item.Command)
	if !*yes {
		fmt.Fprint(os.Stderr, "Approve? [y/N] ")
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && strings.TrimSpace(line) == "" {
			fmt.Fprintln(os.Stderr, "not approved")
			return nil
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "y", "yes":
		default:
			fmt.Fprintln(os.Stderr, "not approved")
			return nil
		}
	}
	text, err := node.Approve(root, item.ID)
	if err != nil {
		return err
	}
	fmt.Println(text)
	return nil
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	return s
}

func nodeDispatch(root string, info node.Info) func(node.Request) (string, error) {
	return func(req node.Request) (string, error) {
		switch req.Cap {
		case "info":
			return node.InfoText(info), nil
		case "shell":
			return node.RunShell(root, req.Command, req.Caller, req.Approved)
		case nodesync.Cap:
			return nodesync.Accept(root, req.Command)
		default:
			return "", fmt.Errorf("node: unknown cap %q", req.Cap)
		}
	}
}

func stopSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM}
}
