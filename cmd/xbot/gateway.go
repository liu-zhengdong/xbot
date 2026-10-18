package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"xbot/internal/gateway"
)

func cmdGateway(root string, args []string) error {
	if len(args) == 0 || args[0] != "serve" {
		return errors.New("usage: xbot gateway serve [--addr 127.0.0.1:8790] [--dir DIR] [--open] [--behind-proxy]")
	}
	ordered, err := reorderFlags(args[1:], map[string]bool{"addr": true, "dir": true})
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("gateway serve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	addr := fs.String("addr", "127.0.0.1:8790", "listen address")
	dir := fs.String("dir", "", "gateway data directory")
	open := fs.Bool("open", false, "allow registration without a token")
	behindProxy := fs.Bool("behind-proxy", false, "trust the last X-Forwarded-For address")
	if err := fs.Parse(ordered); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: xbot gateway serve [--addr 127.0.0.1:8790] [--dir DIR] [--open] [--behind-proxy]")
	}
	if strings.TrimSpace(*dir) == "" {
		*dir = filepath.Join(root, "gateway")
	}
	token := strings.TrimSpace(os.Getenv("XBOT_GATEWAY_TOKEN"))
	if token == "" && !*open {
		return errors.New("gateway: set XBOT_GATEWAY_TOKEN in the environment, or pass --open to allow unauthenticated registration")
	}
	srv := &gateway.Server{Dir: *dir, Token: token, Open: *open, TrustForwarded: *behindProxy}
	ctx, stop := signal.NotifyContext(context.Background(), stopSignals()...)
	defer stop()
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		return err
	}
	defer ln.Close()
	fmt.Fprintf(os.Stderr, "gateway listening on http://%s\n", ln.Addr())
	httpServer := &http.Server{
		Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 20 * time.Second, WriteTimeout: gateway.MaxWait + 15*time.Second,
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
