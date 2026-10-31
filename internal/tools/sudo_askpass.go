package tools

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"xbot/internal/vault"
)

// BindLocalSudo uses the same per-bot backend as HTTP and settings. Reopening
// at each command observes settings edits without restarting a running Bot.
func (s *Shell) BindLocalSudo(open func() (vault.Backend, error)) {
	s.sessMu.Lock()
	s.localSudo = open
	s.sessMu.Unlock()
}

func (s *Shell) sudoEnv(env []string) ([]string, func(), *vault.Store, error) {
	s.sessMu.Lock()
	open := s.localSudo
	s.sessMu.Unlock()
	noop := func() {}
	if open == nil || runtime.GOOS == "windows" {
		return env, noop, nil, nil
	}
	backend, err := open()
	if err != nil {
		return nil, noop, nil, errors.New("shell: cannot open sudo vault")
	}
	store, ok := backend.(*vault.Store)
	if !ok {
		return env, noop, nil, nil
	} // vaultd never exposes plaintext.
	if _, ok := store.Lookup(vault.LocalSudo); !ok {
		return env, noop, nil, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, noop, nil, err
	}
	dir, err := os.MkdirTemp("", "xbot-askpass-")
	if err != nil {
		return nil, noop, nil, err
	}
	path := filepath.Join(dir, "askpass")
	quote := func(v string) string { return "'" + strings.ReplaceAll(v, "'", "'\"'\"'") + "'" }
	script := "#!/bin/sh\nexec " + quote(exe) + " --internal-sudo-askpass " + quote(store.Dir()) + " " + quote(store.KeyPath()) + "\n"
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		os.RemoveAll(dir)
		return nil, noop, nil, err
	}
	unregister := vault.Register(store)
	var once sync.Once
	cleanup := func() { once.Do(func() { unregister(); _ = os.RemoveAll(dir) }) }
	if env == nil {
		env = os.Environ()
	}
	env = stripEnv(env, []string{"SUDO_ASKPASS", "SUDO_ASKPASS_REQUIRE"})
	return append(env, "SUDO_ASKPASS="+path, "SUDO_ASKPASS_REQUIRE=1"), cleanup, store, nil
}
