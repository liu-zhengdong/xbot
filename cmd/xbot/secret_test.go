package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/link"
)

func withStdin(t *testing.T, in string, fn func() error) error {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdin
	os.Stdin = r
	go func() {
		_, _ = io.WriteString(w, in)
		_ = w.Close()
	}()
	runErr := fn()
	os.Stdin = old
	_ = r.Close()
	return runErr
}

func TestSecretCLISetListRm(t *testing.T) {
	root := t.TempDir()
	if _, err := bot.Create(root, "ada", "persona", []string{link.NameTerminal}); err != nil {
		t.Fatal(err)
	}
	const value = "super-secret-value-42"

	if err := withStdin(t, value+"\n", func() error {
		return cmdSecret(root, []string{"set", "ada", "GITHUB_TOKEN", "--domains", "a.com,b.com"})
	}); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(bot.Dir(root, "ada"), "vault")
	raw, err := os.ReadFile(filepath.Join(dir, "secrets.enc"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(value)) {
		t.Fatal("plaintext value on disk")
	}

	out := captureStdout(t, func() error { return cmdSecret(root, []string{"list", "ada"}) })
	if !strings.Contains(out, "GITHUB_TOKEN\ta.com,b.com") {
		t.Fatalf("list output %q", out)
	}
	if strings.Contains(out, value) {
		t.Fatalf("value in list output %q", out)
	}

	// The value must come from stdin, never a positional argument.
	if err := withStdin(t, value, func() error {
		return cmdSecret(root, []string{"set", "ada", "OTHER", "a.com"})
	}); err == nil {
		t.Fatal("accepted a value as an argument")
	}

	out = captureStdout(t, func() error { return cmdSecret(root, []string{"rm", "ada", "GITHUB_TOKEN"}) })
	if !strings.Contains(out, "removed GITHUB_TOKEN") {
		t.Fatalf("rm output %q", out)
	}
	out = captureStdout(t, func() error { return cmdSecret(root, []string{"list", "ada"}) })
	if strings.Contains(out, "GITHUB_TOKEN") {
		t.Fatalf("still listed %q", out)
	}
}
