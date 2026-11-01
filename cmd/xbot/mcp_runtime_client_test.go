package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"xbot/internal/cliops"
)

func TestMCPHelpPrecedesLocalRootAndCredentials(t *testing.T) {
	absent := filepath.Join(t.TempDir(), "must-stay-absent")
	output, err := os.CreateTemp(t.TempDir(), "help")
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	original := os.Stdout
	os.Stdout = output
	defer func() { os.Stdout = original }()
	if err := run([]string{"--root", absent, "mcp", "call", "--help"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(absent); !os.IsNotExist(err) {
		t.Fatalf("help touched root: %v", err)
	}
	raw, err := os.ReadFile(output.Name())
	if err != nil || !bytes.Contains(raw, []byte("--self")) {
		t.Fatalf("help: %s %v", raw, err)
	}
}

func TestMCPHumanUsesOwnerSessionAndPreservesArgsAndErrors(t *testing.T) {
	t.Setenv("XBOT_ROOT", filepath.Join(t.TempDir(), "unrelated-client-default"))
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/session" {
			http.SetCookie(w, &http.Cookie{Name: "owner", Value: "fixture", Path: "/"})
			_ = json.NewEncoder(w).Encode(map[string]bool{"authed": true})
			return
		}
		cookie, err := r.Cookie("owner")
		if err != nil || cookie.Value != "fixture" {
			t.Error("owner cookie was not reused")
		}
		if r.Header.Get("X-XBot-Root") != "" {
			t.Error("implicit client root leaked to human auth")
		}
		if r.URL.Path != "/api/bots/ada/mcp/call" {
			t.Errorf("path %s", r.URL.Path)
		}
		var call cliops.MCPCall
		if err := json.NewDecoder(r.Body).Decode(&call); err != nil {
			t.Error(err)
		}
		if !bytes.Equal(call.Args, []byte(`{"n":9007199254740993}`)) || call.Tool != "remote/name" {
			t.Errorf("arguments changed: %+v", call)
		}
		calls++
		_ = json.NewEncoder(w).Encode(cliops.MCPReply{IsError: true, Content: []cliops.MCPContent{{Type: "text", Text: "remote error content"}}})
	}))
	defer server.Close()
	var out bytes.Buffer
	err := runMCP("", []string{"call", "docs", "remote/name", "--http", server.URL, "--bot", "ada", "--args-file", "-", "--json"}, strings.NewReader(`{"n":9007199254740993}`), &out)
	if err == nil || !strings.Contains(err.Error(), "isError") || !strings.Contains(out.String(), "remote error content") || calls != 1 {
		t.Fatalf("content/exit/count: %s %v %d", out.String(), err, calls)
	}
	out.Reset()
	if err := runMCP("", []string{"call", "docs", "remote/name", "--http", server.URL, "--bot", "ada", "--args-file", "-"}, strings.NewReader(`{} {}`), &out); err == nil {
		t.Fatal("multiple argument objects accepted")
	}
	if calls != 1 {
		t.Fatal("invalid args reached server")
	}
}
