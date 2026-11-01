package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMCPNotesDoNotBlockTools(t *testing.T) {
	m := NewSession(Spec{
		Name:    "docs",
		Command: "fake",
		Dial: dialFake(t, func(msg rpcMessage) []rpcMessage {
			switch msg.Method {
			case "notifications/initialized":
				return nil
			case "initialize":
				return []rpcMessage{rpcOK(msg.ID, `{"protocolVersion":"2024-11-05"}`)}
			case "tools/list":
				out := make([]rpcMessage, 0, 41)
				for i := 0; i < 40; i++ {
					out = append(out, rpcNote("notifications/xbot/event", `{"text":"n"}`))
				}
				out = append(out, rpcOK(msg.ID, `{"tools":[{"name":"echo","inputSchema":{"type":"object"}}]}`))
				return out
			default:
				return nil
			}
		}),
	}, Options{})
	ch := make(chan []ToolSpec, 1)
	go func() { list, _ := m.LoadTools(context.Background()); ch <- list }()
	select {
	case list := <-ch:
		if len(list) != 1 || list[0].Name != "echo" {
			t.Fatalf("%d", len(list))
		}
	case <-time.After(3 * time.Second):
		t.Fatal("tools/list blocked on notifications")
	}
}

func TestMCPToolListStopsAtPageCap(t *testing.T) {
	var n atomic.Int32
	m := NewSession(Spec{
		Name:    "docs",
		Command: "fake",
		Dial: dialFake(t, func(msg rpcMessage) []rpcMessage {
			switch msg.Method {
			case "notifications/initialized":
				return nil
			case "initialize":
				return []rpcMessage{rpcOK(msg.ID, `{"protocolVersion":"2024-11-05"}`)}
			case "tools/list":
				i := n.Add(1)
				body := fmt.Sprintf(`{"tools":[{"name":"t%d","inputSchema":{"type":"object"}}],"nextCursor":"c%d"}`, i, i)
				return []rpcMessage{rpcOK(msg.ID, body)}
			default:
				return nil
			}
		}),
	}, Options{})
	list, err := m.LoadTools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n.Load() != mcpPageCap || len(list) != mcpPageCap {
		t.Fatalf("pages %d tools %d", n.Load(), len(list))
	}
}

func TestMCPSecretsStayNames(t *testing.T) {
	t.Setenv("MCP_OK", "present-value")
	t.Setenv("MCP_SHOULD_NOT_LEAK", "leak-value")
	env := spawnEnv([]string{"MCP_OK"}, os.LookupEnv, (Options{}).providerKey)
	joined := strings.Join(env, "\n")
	if !strings.Contains(joined, "MCP_OK=present-value") || strings.Contains(joined, "leak-value") || strings.Contains(joined, "MCP_SHOULD_NOT_LEAK") {
		t.Fatal(joined)
	}
	if _, ok := os.LookupEnv("PATH"); ok && !strings.Contains(joined, "PATH=") {
		t.Fatal(joined)
	}
	path := filepath.Join(t.TempDir(), "mcp.json")
	if err := AddMCP(path, Spec{Name: "docs", Command: "/bin/echo", SecretEnvs: []string{"MCP_OK"}}, Options{}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("present-value")) || bytes.Contains(raw, []byte("leak-value")) || !bytes.Contains(raw, []byte("MCP_OK")) {
		t.Fatalf("file %s", raw)
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0600 {
		t.Fatalf("mode %v %v", fi, err)
	}
	if err := AddMCP(path, Spec{Name: "docs", Command: "/bin/echo"}, Options{}); !errors.Is(err, ErrMCPExists) {
		t.Fatal(err)
	}
	if err := AddMCP(path, Spec{Name: "bad", Command: "echo\nrm"}, Options{}); err == nil {
		t.Fatal("newline command accepted")
	}
	if err := AddMCP(path, Spec{Name: "bad", Command: "/bin/echo", SecretEnvs: []string{"token"}}, Options{}); err == nil {
		t.Fatal("lowercase secret accepted")
	}
	t.Setenv("XBOT_API_KEY", "sk-provider")
	options := Options{IsProviderKey: func(name string) bool { return name == "XBOT_API_KEY" }}
	leaked := spawnEnv([]string{"XBOT_API_KEY", "MCP_OK"}, func(k string) (string, bool) {
		if k == "XBOT_API_KEY" {
			return "sk-provider", true
		}
		return os.LookupEnv(k)
	}, options.providerKey)
	if strings.Contains(strings.Join(leaked, "\n"), "sk-provider") || strings.Contains(strings.Join(leaked, "\n"), "XBOT_API_KEY=") {
		t.Fatalf("provider key passed: %s", leaked)
	}
	if err := AddMCP(path, Spec{Name: "bad", Command: "/bin/echo", SecretEnvs: []string{"XBOT_API_KEY"}}, options); err == nil {
		t.Fatal("provider key accepted")
	}
	raw, _ = os.ReadFile(path)
	if bytes.Contains(raw, []byte("token")) || bytes.Contains(raw, []byte("echo\n")) {
		t.Fatalf("rejected record stored: %s", raw)
	}
	if err := RemoveMCP(path, "missing", Options{}); !errors.Is(err, ErrMCPMissing) {
		t.Fatal(err)
	}
	if err := RemoveMCP(path, "docs", Options{}); err != nil {
		t.Fatal(err)
	}
	list, err := LoadMCP(path, Options{})
	if err != nil || len(list) != 0 {
		t.Fatalf("%v %v", list, err)
	}
	tokenPath := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(tokenPath, []byte(`{"servers":[{"name":"docs","command":"/bin/echo","token":"sek"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadMCP(tokenPath, Options{}); err == nil {
		t.Fatal("token field accepted")
	}
}

func dialFake(t *testing.T, on func(rpcMessage) []rpcMessage) func(context.Context) (io.ReadWriteCloser, error) {
	t.Helper()
	client, server := net.Pipe()
	go func() {
		defer server.Close()
		sc := bufio.NewScanner(server)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for sc.Scan() {
			var msg rpcMessage
			if err := json.Unmarshal(bytes.TrimSpace(sc.Bytes()), &msg); err != nil {
				return
			}
			for _, out := range on(msg) {
				raw, err := json.Marshal(out)
				if err != nil {
					return
				}
				if _, err := server.Write(append(raw, '\n')); err != nil {
					return
				}
			}
		}
	}()
	t.Cleanup(func() { _ = client.Close() })
	return func(context.Context) (io.ReadWriteCloser, error) { return client, nil }
}

func rpcOK(id json.RawMessage, result string) rpcMessage {
	return rpcMessage{JSONRPC: "2.0", ID: append(json.RawMessage(nil), id...), Result: json.RawMessage(result)}
}

func rpcNote(method, params string) rpcMessage {
	return rpcMessage{JSONRPC: "2.0", Method: method, Params: json.RawMessage(params)}
}

// Transport assertions moved from the connector catalog integration test: only
// declared names use injected credentials, with process environment fallback.
func TestMCPSecretLookupPrecedence(t *testing.T) {
	t.Setenv("CONTEXT7_API_KEY", "from-env")
	s := Spec{SecretEnvs: []string{"CONTEXT7_API_KEY"}, Secret: func(name string) (string, bool) {
		switch name {
		case "CONTEXT7_API_KEY":
			return "from-bot", true
		case "PATH":
			return "/evil", true
		}
		return "", false
	}}
	env := strings.Join(spawnEnv(s.SecretEnvs, s.secretLookup(), (Options{}).providerKey), "\n")
	if !strings.Contains(env, "CONTEXT7_API_KEY=from-bot") || strings.Contains(env, "from-env") || strings.Contains(env, "/evil") {
		t.Fatalf("env %q", env)
	}
	if h := secretHeaders(s.SecretEnvs, s.secretLookup(), (Options{}).providerKey); h.Get("CONTEXT7_API_KEY") != "from-bot" {
		t.Fatalf("headers %v", h)
	}
	s.Secret = func(string) (string, bool) { return "", false }
	if env := strings.Join(spawnEnv(s.SecretEnvs, s.secretLookup(), (Options{}).providerKey), "\n"); !strings.Contains(env, "CONTEXT7_API_KEY=from-env") {
		t.Fatalf("fallback %q", env)
	}
}
