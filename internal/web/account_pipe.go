package web

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"xbot/internal/account"
	"xbot/internal/fsutil"
)

type accountPipeChange struct {
	session, only string
	ack           chan error
}
type accountPipe struct {
	changes chan accountPipeChange
	done    chan struct{}
}

func (s *Server) accountSessionPath() string { return filepath.Join(s.Root, "account-session") }

// keepPipeSession is called only after the binding has proved account ownership.
func (s *Server) keepPipeSession(session string) error { return s.changePipeSession(session, "") }
func (s *Server) forgetPipeSession(session string)     { _ = s.changePipeSession("", session) }

func (s *Server) changePipeSession(session, only string) error {
	s.acct.pipeMu.Lock()
	p := s.acct.pipe
	if p == nil {
		defer s.acct.pipeMu.Unlock()
		if session != "" {
			return fsutil.WriteAtomic(s.accountSessionPath(), []byte(session), 0o600)
		}
		raw, _ := os.ReadFile(s.accountSessionPath())
		if only != "" && string(raw) != only {
			return nil
		}
		err := os.Remove(s.accountSessionPath())
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	s.acct.pipeMu.Unlock()
	c := accountPipeChange{session: session, only: only, ack: make(chan error, 1)}
	select {
	case p.changes <- c:
	case <-p.done:
		return context.Canceled
	}
	select {
	case err := <-c.ack:
		return err
	case <-p.done:
		return context.Canceled
	}
}

// StartAccountPipe restores the bound owner's session and serializes standby
// rotation, logout and expiry. run must exit when its context is cancelled.
// Account checks and device reports continue even with no browser open.
func (s *Server) StartAccountPipe(ctx context.Context, run func(context.Context, string, string) error) func() {
	ctx, stop := context.WithCancel(ctx)
	p := &accountPipe{changes: make(chan accountPipeChange), done: make(chan struct{})}
	s.acct.pipeMu.Lock()
	s.acct.pipe = p
	s.acct.pipeMu.Unlock()
	go func() {
		defer close(p.done)
		var session string
		var cancel context.CancelFunc
		var ended chan error
		halt := func() {
			if cancel != nil {
				cancel()
				<-ended
				cancel = nil
				ended = nil
			}
		}
		defer halt()
		start := func(next string) error {
			if next == session && cancel != nil {
				return nil
			}
			halt()
			if next == "" {
				session = ""
				err := os.Remove(s.accountSessionPath())
				if errors.Is(err, os.ErrNotExist) {
					return nil
				}
				return err
			}
			if len(next) > 128 || strings.TrimSpace(next) != next {
				return errors.New("invalid account session")
			}
			if err := fsutil.WriteAtomic(s.accountSessionPath(), []byte(next), 0o600); err != nil {
				return err
			}
			session = next
			var child context.Context
			child, cancel = context.WithCancel(ctx)
			ended = make(chan error, 1)
			go func(result chan error) { result <- s.runAccountPipe(child, next, run) }(ended)
			return nil
		}
		if raw, err := os.ReadFile(s.accountSessionPath()); err == nil && len(raw) > 0 {
			if err := start(string(raw)); err != nil {
				log.Print("xbot: pipe: cannot restore account session")
			}
		}
		for {
			select {
			case <-ctx.Done():
				return
			case c := <-p.changes:
				if c.only != "" && c.only != session {
					c.ack <- nil
					continue
				}
				c.ack <- start(c.session)
			case err := <-ended:
				cancel()
				cancel = nil
				ended = nil
				if errors.Is(err, account.ErrSession) {
					_ = os.Remove(s.accountSessionPath())
					session = ""
					log.Print("xbot: pipe: account session expired; standby stopped")
				} else if ctx.Err() == nil {
					log.Print("xbot: pipe: standby stopped")
				}
			}
		}
	}()
	return func() { stop(); <-p.done }
}

func (s *Server) runAccountPipe(ctx context.Context, session string, run func(context.Context, string, string) error) error {
	var device string
	runCtx, stopRun := context.WithCancel(ctx)
	var done chan error
	defer func() {
		stopRun()
		if done != nil {
			<-done
		}
	}()
	check := func() error {
		checkCtx, stop := context.WithTimeout(ctx, 10*time.Second)
		defer stop()
		me, err := s.acct.client.Me(checkCtx, session)
		if err != nil {
			return err
		}
		b, err := s.readBinding()
		if err != nil {
			return err
		}
		if !me.Verified || b.Key == "" || b.Key != me.Key || b.DeviceID == "" {
			return account.ErrSession
		}
		device = b.DeviceID
		return s.reportDevice(checkCtx, session, b, true)
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		err := check()
		if errors.Is(err, account.ErrSession) {
			return err
		}
		if err == nil && done == nil {
			done = make(chan error, 1)
			go func(result chan error, id string) { result <- run(runCtx, session, id) }(done, device)
		}
		if err != nil && ctx.Err() == nil {
			log.Print("xbot: pipe: account check/report unavailable; retrying")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		case err := <-done:
			done = nil
			return fmt.Errorf("standby ended: %w", err)
		}
	}
}
