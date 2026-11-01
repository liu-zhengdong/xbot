package mcp

import (
	"context"
	"errors"
	"io"
	"os"
	"time"
)

// spawnEnv is the child environment: a short base list, plus named secrets that are already set.
func spawnEnv(names []string, lookup func(string) (string, bool), providerKey func(string) bool) []string {
	if lookup == nil {
		lookup = os.LookupEnv
	}
	var out []string
	have := map[string]bool{}
	add := func(k string) {
		if have[k] {
			return
		}
		v, ok := lookup(k)
		if !ok {
			return
		}
		have[k] = true
		out = append(out, k+"="+v)
	}
	for _, k := range []string{"PATH", "HOME", "TMPDIR", "LANG", "SYSTEMROOT"} {
		add(k)
	}
	for _, k := range names {
		if providerKey(k) {
			continue
		}
		add(k)
	}
	return out
}

type stdioConn struct {
	io.Reader
	w     io.WriteCloser
	c     io.Closer
	wait  func()
	abort func(error)
}

func (s *stdioConn) Write(p []byte) (int, error) { return s.w.Write(p) }

func (s *stdioConn) WriteContext(ctx context.Context, p []byte) (int, error) {
	return writeWithContext(ctx, s.w, p, s.abort)
}

// zeroWriteCanceled records cancellation before any byte was sent. If a write
// began, its deadline must have interrupted it and been successfully reset.
type zeroWriteCanceled struct{ cause error }

func (e *zeroWriteCanceled) Error() string { return e.cause.Error() }
func (e *zeroWriteCanceled) Unwrap() error { return e.cause }

// writeWithContext is shared by the real stdin pipe and injected transports.
// A pipe without deadline support must be canceled through its Session so the
// child is killed before Close waits for process exit.
func writeWithContext(ctx context.Context, w io.Writer, p []byte, abort func(error)) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, &zeroWriteCanceled{err}
	}
	if writer, ok := w.(interface{ SetWriteDeadline(time.Time) error }); ok {
		deadline, _ := ctx.Deadline()
		if err := writer.SetWriteDeadline(deadline); err == nil {
			done := make(chan struct{})
			stop := context.AfterFunc(ctx, func() {
				_ = writer.SetWriteDeadline(time.Now())
				close(done)
			})
			n, err := w.Write(p)
			if !stop() {
				<-done
			}
			if n == len(p) && err == nil {
				return n, writer.SetWriteDeadline(time.Time{})
			}
			if n == 0 && errors.Is(err, os.ErrDeadlineExceeded) {
				cause := ctx.Err()
				if cause == nil && !deadline.IsZero() && !time.Now().Before(deadline) {
					cause = context.DeadlineExceeded
				}
				if cause != nil {
					if resetErr := writer.SetWriteDeadline(time.Time{}); resetErr == nil {
						return 0, &zeroWriteCanceled{cause}
					}
				}
			}
			return n, errors.Join(err, ctx.Err())
		} else if !errors.Is(err, os.ErrNoDeadline) {
			return 0, err
		}
	}
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		abort(ctx.Err())
		close(done)
	})
	n, err := w.Write(p)
	if !stop() {
		<-done
	}
	return n, errors.Join(err, ctx.Err())
}

func (s *stdioConn) Close() error {
	err1 := s.w.Close()
	err2 := s.c.Close()
	if s.wait != nil {
		s.wait()
	}
	if err1 != nil {
		return err1
	}
	return err2
}
