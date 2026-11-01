package mcp

import (
	"io"
	"os"
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
	w    io.WriteCloser
	c    io.Closer
	wait func()
}

func (s *stdioConn) Write(p []byte) (int, error) { return s.w.Write(p) }

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
