package link

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// Real stdio transport, scripted child; no npm, network, browser or model.
func TestMCPDiagnosticProcess(t *testing.T) {
	if len(os.Args) < 2 {
		return
	}
	mode := os.Args[len(os.Args)-1]
	if mode != "mcp-diagnostic-child" && mode != "mcp-oversized-child" {
		return
	}
	sc := bufio.NewScanner(os.Stdin)
	sc.Scan()
	if mode == "mcp-oversized-child" {
		fmt.Print(strings.Repeat("x", (1<<20)+1))
		sc.Scan()
		os.Exit(0)
	}
	fmt.Fprintln(os.Stderr, "npx: missing browser dependency", os.Getenv("MCP_TEST_SECRET"))
	os.Exit(7)
}

func TestMCPStartupStderrAndStatus(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	m := NewMCP(MCPServer{Name: "playwright", Command: exe, Args: []string{"-test.run=^TestMCPDiagnosticProcess$", "mcp-diagnostic-child"}, SecretEnvs: []string{"MCP_TEST_SECRET"}, Secret: func(string) (string, bool) { return "fixture-private-value", true }})
	defer m.Close()
	if state := m.Status(); state.State != "configured" {
		t.Fatal(state)
	}
	if list := m.Tools(); len(list) != 0 {
		t.Fatal("unexpected tools")
	}
	state := m.Status()
	if state.State != "error" || !strings.Contains(state.Error, "initialize") || !strings.Contains(state.Error, "missing browser dependency") || !strings.Contains(state.Error, "exit status 7") || strings.Contains(state.Error, "fixture-private-value") {
		t.Fatalf("bad sanitized status: %+v", state)
	}
	_, err = m.LoadTools(context.Background())
	if err == nil || err.Error() != state.Error {
		t.Fatalf("cached failure lost: %v", err)
	}
}

func TestMCPDiscoveryFailures(t *testing.T) {
	for _, mode := range []string{"initialize", "tools/list", "empty", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			m := NewMCP(MCPServer{Name: "fixture", Command: "fake", Dial: dialFake(t, func(msg rpcMessage) []rpcMessage {
				if msg.Method == mode && mode != "timeout" {
					return []rpcMessage{{JSONRPC: "2.0", ID: msg.ID, Error: &rpcError{Code: -32000, Message: "dependency unavailable"}}}
				}
				switch msg.Method {
				case "initialize":
					return []rpcMessage{rpcOK(msg.ID, `{"protocolVersion":"2024-11-05"}`)}
				case "tools/list":
					if mode == "timeout" {
						return nil
					}
					return []rpcMessage{rpcOK(msg.ID, `{"tools":[]}`)}
				}
				return nil
			})})
			defer m.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			_, err := m.LoadTools(ctx)
			if err == nil {
				t.Fatal("failure returned success")
			}
			want := mode
			if mode == "empty" {
				want = "server returned no tools"
			}
			if mode == "timeout" {
				want = "deadline exceeded"
			}
			if !strings.Contains(err.Error(), want) || m.Status().State != "error" {
				t.Fatalf("%v %+v", err, m.Status())
			}
		})
	}
}

func TestMCPDiagnosticsBoundAndRedact(t *testing.T) {
	m := NewMCP(MCPServer{Name: "fixture", SecretEnvs: []string{"KEY"}, Secret: func(string) (string, bool) { return "private-fixture", true }})
	m.redact = m.diagnosticRedactor()
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 20 {
				_, _ = m.stderr.Write([]byte("dependency private-fixture\n"))
				_ = m.stderr.String()
			}
		}()
	}
	wg.Wait()
	_, _ = m.stderr.Write([]byte(strings.Repeat("x", mcpDiagnosticCap)))
	err := m.diagnostic("initialize", fmt.Errorf("request rejected private-fixture"))
	if len(err.Error()) > mcpDiagnosticCap+100 || strings.Contains(err.Error(), "private-fixture") || !strings.Contains(err.Error(), "truncated") {
		t.Fatal("unbounded or unredacted diagnostic")
	}
}

func TestMCPDiscoveryStatusAfterClose(t *testing.T) {
	m := NewMCP(MCPServer{Name: "fixture", Dial: dialFake(t, func(msg rpcMessage) []rpcMessage {
		if msg.Method == "initialize" {
			return []rpcMessage{rpcOK(msg.ID, `{}`)}
		}
		if msg.Method == "tools/list" {
			return []rpcMessage{rpcOK(msg.ID, `{"tools":[{"name":"test"}]}`)}
		}
		return nil
	})})
	if _, err := m.LoadTools(context.Background()); err != nil {
		t.Fatal(err)
	}
	if st := m.Status(); st.State != "ready" || st.ToolCount != 1 {
		t.Fatal(st)
	}
	_ = m.Close()
	deadline := time.Now().Add(time.Second)
	for m.Status().State != "error" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if m.Status().State != "error" {
		t.Fatal("closed connection still ready")
	}
	if list, err := m.LoadTools(context.Background()); err == nil || len(list) != 0 {
		t.Fatalf("stale tools %d %v", len(list), err)
	}
}

func TestMCPHTTPLoadDeadline(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer srv.Close()
	defer close(release)
	m := NewMCP(MCPServer{Name: "slow", URL: srv.URL})
	defer m.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := m.LoadTools(ctx); done <- err }()
	<-entered
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "deadline exceeded") {
			t.Fatalf("timeout hidden: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("load context did not bound HTTP handshake")
	}
}

func TestMCPFailedDiscoveryReapsProcess(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	m := NewMCP(MCPServer{Name: "oversized", Command: exe, Args: []string{"-test.run=^TestMCPDiagnosticProcess$", "mcp-oversized-child"}})
	defer m.Close()
	if _, err := m.LoadTools(context.Background()); err == nil {
		t.Fatal("oversized protocol line accepted")
	}
	reader := m.client.conn.(*stdioConn).Reader.(*mcpProcessReader)
	if reader.cmd.ProcessState == nil {
		t.Fatal("failed initialization left process unreaped")
	}
}
