package kernel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"xbot/internal/provider"
)

func TestCwdReadFollowsShell(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "pkg", "notes.txt"), "IN-PKG\n")
	k, p, now := workBot(t, dir)
	runTurn(t, k, now, "go",
		toolCall("c1", "shell", `{"command":"cd pkg && cat notes.txt"}`),
		toolCall("c2", "read_file", `{"path":"notes.txt"}`),
		provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("done")})
	var shellOut, readOut string
	for _, r := range p.reqs {
		for _, m := range r.Messages {
			if m.Role == provider.RoleTool && m.CallID() == "c1" {
				shellOut = m.Text()
			}
			if m.Role == provider.RoleTool && m.CallID() == "c2" {
				readOut = m.Text()
			}
		}
	}
	t.Logf("shell: %q\nread_file: %q", shellOut, readOut)
	if strings.Contains(shellOut, "cwd: ") && !strings.Contains(readOut, "IN-PKG") {
		t.Errorf("after the shell reported cwd .../pkg, read_file(\"notes.txt\") did not read pkg/notes.txt")
	}
}

// write_file after cd lands at the workspace root, silently.
func TestCwdWriteFollowsShell(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "pkg", "go.mod"), "module pkg\n")
	k, _, now := workBot(t, dir)
	runTurn(t, k, now, "go",
		toolCall("c1", "shell", `{"command":"cd pkg"}`),
		toolCall("c2", "write_file", `{"path":"main.go","content":"package main\n"}`),
		toolCall("c3", "shell", `{"command":"ls"}`),
		provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("done")})
	if _, err := os.Stat(filepath.Join(dir, "pkg", "main.go")); err != nil {
		_, rootErr := os.Stat(filepath.Join(dir, "main.go"))
		t.Errorf("write_file(\"main.go\") after `cd pkg` did not create pkg/main.go (created at workspace root: %v)", rootErr == nil)
	}
}
