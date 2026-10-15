package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"xbot/internal/node"
	"xbot/internal/nodesync"
)

func cmdNode(root string, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: xbot node serve|add|list|remove|call")
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
	tok, created, err := node.EnsureToken(root, *token)
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	nodesync.Start(ctx, root, nodesync.DefaultEvery, nodesync.DefaultDebounce)
	srv := &http.Server{Addr: *addr, Handler: node.Handler(tok, info, nodeDispatch(root, info))}
	go func() {
		<-ctx.Done()
		shut, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shut)
	}()
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
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
	if err := node.Add(root, node.Node{Name: fs.Arg(0), URL: *rawURL, Token: *token}); err != nil {
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
		if info, err := node.Ping(ctx, n.URL, n.Token); err == nil {
			state = "online " + info.Name
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
	text, err := node.Call(ctx, n.URL, n.Token, args[1], command, true)
	if err != nil {
		return err
	}
	fmt.Println(text)
	return nil
}

func nodeDispatch(root string, info node.Info) func(node.Request) (string, error) {
	return func(req node.Request) (string, error) {
		switch req.Cap {
		case "info":
			return node.InfoText(info), nil
		case "shell":
			return node.RunShell(root, req.Command, req.Approved)
		case nodesync.Cap:
			return nodesync.Accept(root, req.Command)
		default:
			return "", fmt.Errorf("node: unknown cap %q", req.Cap)
		}
	}
}
