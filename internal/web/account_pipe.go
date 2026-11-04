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
)

type accountPipeChange struct {
	session, only string // installation credentials; never UI sessions
	ack           chan error
}
type accountPipe struct {
	changes chan accountPipeChange
	done    chan struct{}
}

// accountSessionPath is the legacy UI session file, read only for migration.
func (s *Server) accountSessionPath() string { return filepath.Join(s.Root, "account-session") }

// keepPipeSession is called after UI account ownership has been proved. Only
// the independent installation credential reaches the standby runner.
func (s *Server) keepPipeSession(session string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	credential, err := s.boundCredential(ctx, session, false)
	if errors.Is(err, account.ErrBinding) {
		// Logging into the UI cannot restore a revoked installation member.
		return s.changePipeSession("", "")
	}
	if err != nil {
		return err
	}
	s.clearLegacySession(session)
	return s.changePipeSession(credential, "")
}

// UI logout and session expiry leave installation membership and standby alone.
// An empty session is the explicit stop used when this installation is unbound.
func (s *Server) forgetPipeSession(session string) {
	if session != "" {
		s.clearLegacySession(session)
		return
	}
	_ = s.changePipeSession("", "")
}

func (s *Server) clearLegacySession(only string) {
	raw, err := os.ReadFile(s.accountSessionPath())
	if err == nil && (only == "" || string(raw) == only) {
		_ = os.Remove(s.accountSessionPath())
	}
}

func (s *Server) changePipeSession(credential, only string) error {
	s.acct.pipeMu.Lock()
	p := s.acct.pipe
	s.acct.pipeMu.Unlock()
	if p == nil {
		// The credential is already durable in the binding file.
		return nil
	}
	c := accountPipeChange{session: credential, only: only, ack: make(chan error, 1)}
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

// StartAccountPipe restores installation identity without an open browser.
// A legacy session can migrate once. Invalid binding tokens never auto-rebind.
// run must exit when its context is cancelled.
func (s *Server) StartAccountPipe(ctx context.Context, run func(context.Context, string, string) error) func() {
	ctx, stop := context.WithCancel(ctx)
	p := &accountPipe{changes: make(chan accountPipeChange), done: make(chan struct{})}
	s.acct.pipeMu.Lock()
	s.acct.pipe = p
	s.acct.pipeMu.Unlock()
	go func() {
		defer close(p.done)
		var credential string
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
			if next == credential && cancel != nil {
				return nil
			}
			halt()
			credential = ""
			if next == "" {
				return nil
			}
			if len(next) > 128 || strings.TrimSpace(next) != next {
				return account.ErrBinding
			}
			credential = next
			var child context.Context
			child, cancel = context.WithCancel(ctx)
			ended = make(chan error, 1)
			go func(result chan error) { result <- s.runAccountPipe(child, next, run) }(ended)
			return nil
		}
		if b, err := s.readBinding(); err == nil && b.Key != "" {
			var next string
			if b.Token != "" {
				next = b.Token
				s.clearLegacySession("")
			} else if raw, err := os.ReadFile(s.accountSessionPath()); err == nil && len(raw) > 0 {
				checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				next, err = s.boundCredential(checkCtx, string(raw), false)
				cancel()
				if err != nil {
					log.Print("xbot: pipe: cannot migrate legacy account session")
				}
				if err == nil || errors.Is(err, account.ErrSession) || errors.Is(err, account.ErrBinding) {
					s.clearLegacySession(string(raw))
				}
			}
			if next != "" {
				if err := start(next); err != nil {
					log.Print("xbot: pipe: cannot restore account binding")
				}
			}
		}
		for {
			select {
			case <-ctx.Done():
				return
			case c := <-p.changes:
				if c.only != "" && c.only != credential {
					c.ack <- nil
					continue
				}
				c.ack <- start(c.session)
			case err := <-ended:
				cancel()
				cancel = nil
				ended = nil
				credential = ""
				if errors.Is(err, account.ErrBinding) {
					log.Print("xbot: pipe: account binding revoked; standby stopped")
				} else if ctx.Err() == nil {
					log.Print("xbot: pipe: standby stopped")
				}
			}
		}
	}()
	return func() { stop(); <-p.done }
}

func (s *Server) runAccountPipe(ctx context.Context, credential string, run func(context.Context, string, string) error) error {
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
		identity, err := s.acct.client.AuthenticateBinding(checkCtx, credential)
		if err != nil {
			return err
		}
		b, err := s.readBinding()
		if err != nil {
			return err
		}
		if b.Key == "" || identity.AccountKey != b.Key || identity.DeviceID != b.DeviceID || identity.ID != b.BindingID || b.Token != credential {
			return account.ErrBinding
		}
		device = b.DeviceID
		return s.acct.client.ReportMetadata(checkCtx, credential, s.deviceReport(device))
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		err := check()
		if errors.Is(err, account.ErrBinding) || errors.Is(err, account.ErrRevoked) {
			return account.ErrBinding
		}
		if err == nil && done == nil {
			done = make(chan error, 1)
			go func(result chan error, id string) { result <- run(runCtx, credential, id) }(done, device)
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
