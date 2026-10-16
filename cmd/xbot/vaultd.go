package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"

	"xbot/internal/vault"
)

// cmdVaultd runs the vault daemon. It owns the per-bot vaults under --dir and
// only serves peers whose uid is in --allow-uid.
func cmdVaultd(root string, args []string) error {
	ordered, err := reorderFlags(args, map[string]bool{"dir": true, "socket": true, "allow-uid": true})
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("vaultd", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := fs.String("dir", "", "daemon data directory")
	socket := fs.String("socket", "", "unix socket path")
	allow := fs.String("allow-uid", "", "comma-separated uids allowed to connect")
	if err := fs.Parse(ordered); err != nil {
		return err
	}
	if fs.NArg() != 0 || strings.TrimSpace(*dir) == "" || strings.TrimSpace(*socket) == "" || strings.TrimSpace(*allow) == "" {
		return errors.New("usage: xbot vaultd --dir DIR --socket PATH --allow-uid N[,N]")
	}
	allowUIDs, err := parseUIDs(*allow)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(*dir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(*dir, 0700); err != nil {
		return err
	}
	// Remove a stale socket. Only unlink the exact path we were given.
	if _, err := os.Stat(*socket); err == nil {
		if err := os.Remove(*socket); err != nil {
			return err
		}
	}
	ln, err := net.Listen("unix", *socket)
	if err != nil {
		return err
	}
	// Access is enforced by SO_PEERCRED, not the socket mode.
	if err := os.Chmod(*socket, 0666); err != nil {
		_ = ln.Close()
		return err
	}
	defer func() {
		_ = ln.Close()
		_ = os.Remove(*socket)
	}()

	ctx, stop := signal.NotifyContext(context.Background(), stopSignals()...)
	defer stop()
	// Close the listener on SIGTERM/SIGINT so Serve returns.
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	server := &vault.Server{DataDir: *dir, AllowUIDs: allowUIDs}
	fmt.Fprintf(os.Stderr, "vaultd: serving %s on %s\n", *dir, *socket)
	if err := server.Serve(ln); err != nil && !errors.Is(err, net.ErrClosed) {
		return err
	}
	return nil
}

func parseUIDs(raw string) ([]int, error) {
	var out []int
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		uid, err := strconv.Atoi(part)
		if err != nil || uid < 0 {
			return nil, fmt.Errorf("vaultd: bad uid %q", part)
		}
		out = append(out, uid)
	}
	if len(out) == 0 {
		return nil, errors.New("vaultd: no allowed uids")
	}
	return out, nil
}
