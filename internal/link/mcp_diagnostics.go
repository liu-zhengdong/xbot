package link

import (
	"errors"
	"io"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"unicode"
)

const mcpDiagnosticCap = 8 << 10

// MCPStatus describes this instance, not installation or browser readiness.
// Reading it never starts a process or performs an RPC.
type MCPStatus struct {
	State     string `json:"state"` // configured, ready, error
	ToolCount int    `json:"tool_count"`
	Error     string `json:"error,omitempty"`
}

func (m *MCP) Status() MCPStatus {
	m.mu.Lock()
	err, client, listed, count := m.lastErr, m.client, m.listed, len(m.tools)
	m.mu.Unlock()
	if client != nil && err == nil {
		if dead := client.failure(); dead != nil {
			err = m.diagnostic("connection", dead)
		}
	}
	if err != nil {
		return MCPStatus{State: "error", Error: err.Error()}
	}
	if listed {
		return MCPStatus{State: "ready", ToolCount: count}
	}
	return MCPStatus{State: "configured"}
}

func (m *MCP) diagnostic(stage string, cause error) error {
	text := cause.Error()
	if stderr := m.stderr.String(); stderr != "" {
		text += "; stderr: " + stderr
	}
	if m.redact != nil {
		text = m.redact(text)
	}
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, text)
	if len(text) > mcpDiagnosticCap {
		text = strings.ToValidUTF8(text[:mcpDiagnosticCap], "") + " [truncated]"
	}
	err := errors.New(m.Name() + " " + stage + ": " + text)
	m.mu.Lock()
	m.lastErr = err
	m.mu.Unlock()
	return err
}

// Snapshot launch credentials so rotation cannot expose an older value printed
// by this process. Redact before truncating the returned diagnostic.
func (m *MCP) diagnosticRedactor() func(string) string {
	var values []string
	lookup := m.Spec.secretLookup()
	for _, name := range m.Spec.SecretEnvs {
		if v, ok := lookup(name); ok && v != "" {
			values = append(values, v)
		}
	}
	sort.Slice(values, func(i, j int) bool { return len(values[i]) > len(values[j]) })
	var pairs []string
	for _, v := range values {
		pairs = append(pairs, v, "[secret]")
	}
	replacer := strings.NewReplacer(pairs...)
	return func(s string) string { return replacer.Replace(s) }
}

// Keep a bounded prefix. On overflow, discard the incomplete final line so a
// secret split at the buffer boundary cannot leak as an unredactable fragment.
type mcpDiagnostics struct {
	mu        sync.Mutex
	data      []byte
	truncated bool
}

func (b *mcpDiagnostics) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	if !b.truncated {
		room := mcpDiagnosticCap - len(b.data)
		if len(p) > room {
			b.data = append(b.data, p[:room]...)
			b.truncated = true
		} else {
			b.data = append(b.data, p...)
		}
	}
	return n, nil
}
func (b *mcpDiagnostics) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := string(b.data)
	if b.truncated {
		if i := strings.LastIndexByte(s, '\n'); i >= 0 {
			s = s[:i]
		} else {
			s = ""
		}
		s += " [stderr truncated]"
	}
	return strings.TrimSpace(s)
}

// Wait only after stdout has drained. Cmd.Wait also drains the bounded stderr
// writer, so early process exits retain their actual diagnostic and exit code.
type mcpProcessReader struct {
	io.Reader
	cmd  *exec.Cmd
	once sync.Once
	err  error
}

func (r *mcpProcessReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if err != nil {
		r.wait()
		if r.err != nil {
			err = r.err
		}
	}
	return n, err
}

func (r *mcpProcessReader) wait() { r.once.Do(func() { r.err = r.cmd.Wait() }) }
