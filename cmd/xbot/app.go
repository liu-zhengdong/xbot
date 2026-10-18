package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os/signal"
	"path/filepath"
	"strconv"
	"time"

	"xbot/internal/appshell"
	"xbot/internal/fsutil"
	"xbot/internal/web"
)

func lockApp(root string) (*fsutil.FileLock, error) {
	lock, ok, err := fsutil.TryLock(filepath.Join(root, "app.lock"))
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("xbot app is already running")
	}
	return lock, nil
}

func cmdApp(root string, args []string) error {
	fs := flag.NewFlagSet("app", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	port := fs.Int("port", 8787, "local web UI port")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || *port < 1 || *port > 65535 {
		return errors.New("usage: xbot app [--port 8787]")
	}
	lock, err := lockApp(root)
	if err != nil {
		return err
	}
	defer lock.Release()
	ctx, cancel := signal.NotifyContext(context.Background(), stopSignals()...)
	defer cancel()
	type launch struct {
		url string
		err error
	}
	listening := make(chan launch, 1)
	done := make(chan error, 1)
	go func() {
		err := runServe(ctx, root, serveConfig{
			HTTPAddr: net.JoinHostPort("127.0.0.1", strconv.Itoa(*port)), Interval: 15 * time.Second, App: true,
			OnListening: func(ui *web.Server, addr net.Addr) {
				// Set before the page loads, so the first POST already reaches the window.
				ui.AppTheme = appshell.SetTheme
				code, err := ui.NewLaunchCode()
				listening <- launch{url: "http://" + addr.String() + "/app/launch?code=" + code, err: err}
			},
		})
		var op *net.OpError
		if errors.As(err, &op) && op.Op == "listen" {
			err = fmt.Errorf("xbot app: cannot listen on port %d: %w", *port, err)
		}
		done <- err
		cancel()
	}()
	wait := func() error {
		cancel()
		select {
		case err := <-done:
			return err
		case <-time.After(10 * time.Second):
			return errors.New("xbot app: serve shutdown timed out")
		}
	}
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return wait()
	case launch := <-listening:
		if launch.err != nil {
			_ = wait()
			return launch.err
		}
		shellDone := make(chan struct{})
		go func() {
			select {
			case <-ctx.Done():
				appshell.Quit()
			case <-shellDone:
			}
		}()
		// MyGo locks the main goroutine's OS thread in init. Keep Run here.
		err := appshell.Run(appshell.Options{Title: "XBot", URL: launch.url, StatePath: filepath.Join(root, "app-window.json")}, nil)
		close(shellDone)
		stopErr := wait()
		if err != nil {
			return err
		}
		return stopErr
	}
}
