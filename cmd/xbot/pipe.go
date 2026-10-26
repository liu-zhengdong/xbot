package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"xbot/internal/account"
	"xbot/internal/pipe"
)

const pipeUsage = "usage: xbot pipe serve [--addr 127.0.0.1:8792] [--public URL]  (env ACCOUNT_API, optional PIPE_ORIGINS)"

func cmdPipe(args []string) error {
	if len(args) == 0 || args[0] != "serve" {
		return errors.New(pipeUsage)
	}
	ordered, err := reorderFlags(args[1:], map[string]bool{"addr": true, "public": true})
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("pipe serve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	addr := fs.String("addr", "127.0.0.1:8792", "listen address")
	public := fs.String("public", "", "public base URL returned with tickets")
	if err := fs.Parse(ordered); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New(pipeUsage)
	}
	accountAPI := strings.TrimSpace(os.Getenv("ACCOUNT_API"))
	if accountAPI == "" {
		return errors.New("pipe: set ACCOUNT_API to the Account API base URL, such as https://account.babelgo.cn:8443")
	}
	var origins []string
	for _, o := range strings.Split(os.Getenv("PIPE_ORIGINS"), ",") {
		if o = strings.TrimRight(strings.TrimSpace(o), "/"); o != "" {
			origins = append(origins, o)
		}
	}
	hub := &pipe.Hub{Accounts: account.NewRemote(accountAPI), Public: strings.TrimRight(*public, "/"), Origins: origins, Log: log.New(os.Stderr, "", log.LstdFlags)}
	ctx, stop := signal.NotifyContext(context.Background(), stopSignals()...)
	defer stop()
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		return err
	}
	defer ln.Close()
	fmt.Fprintf(os.Stderr, "pipe listening on http://%s\n", ln.Addr())
	// Deadlines cover the ticket POST and the upgrade; a WebSocket clears
	// them once it takes the connection.
	httpServer := &http.Server{
		Handler: hub.Handler(), ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 20 * time.Second, WriteTimeout: 30 * time.Second,
		IdleTimeout: 120 * time.Second,
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	served := make(chan error, 1)
	go func() { served <- httpServer.Serve(ln) }()
	select {
	case err = <-served:
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdown); err != nil {
			_ = httpServer.Close()
			return err
		}
		err = <-served
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
